package document

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// DocumentRetentionJob enforces retention rules for completed/cancelled cases.
type DocumentRetentionJob struct {
	db            *sqlx.DB
	storage       DocumentStorage
	archiveBucket string
	sweepInterval time.Duration
	batchSize     int
	logger        *slog.Logger
}

// NewDocumentRetentionJob creates a daily retention sweep worker.
func NewDocumentRetentionJob(
	db *sqlx.DB,
	storage DocumentStorage,
	archiveBucket string,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *DocumentRetentionJob {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 10000
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DocumentRetentionJob{
		db:            db,
		storage:       storage,
		archiveBucket: strings.TrimSpace(archiveBucket),
		sweepInterval: interval,
		batchSize:     batchSize,
		logger:        logger,
	}
}

type retentionCandidate struct {
	DocumentID       string    `db:"document_id"`
	CaseID           string    `db:"case_id"`
	DocumentTypeCode string    `db:"document_type_code"`
	StoragePath      string    `db:"storage_path"`
	RetentionPolicy  string    `db:"retention_policy"`
	UploadedAt       time.Time `db:"uploaded_at"`
}

// Run executes a single retention sweep batch.
func (j *DocumentRetentionJob) Run(ctx context.Context) error {
	if j == nil {
		return fmt.Errorf("DocumentRetentionJob.Run: job is nil")
	}
	if j.db == nil {
		return fmt.Errorf("DocumentRetentionJob.Run: db is nil")
	}
	if j.storage == nil {
		return fmt.Errorf("DocumentRetentionJob.Run: storage is nil")
	}

	tx, err := j.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("DocumentRetentionJob.Run: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var candidates []retentionCandidate
	err = tx.SelectContext(ctx, &candidates, `
		SELECT
			cd.id::text AS document_id,
			cd.case_id::text AS case_id,
			cd.document_type_code,
			cd.storage_path,
			dt.retention_policy,
			cd.uploaded_at
		FROM case_documents cd
		JOIN cases c ON c.id = cd.case_id
		JOIN document_types dt
		  ON dt.case_type_code = cd.case_type_code
		 AND dt.case_type_version = cd.case_type_version
		 AND dt.document_type_code = cd.document_type_code
		WHERE cd.status IN ('UPLOADED', 'VERIFIED')
		  AND cd.legal_hold = FALSE
		  AND c.status IN ('COMPLETED', 'CANCELLED')
		  AND cd.uploaded_at < (now() - (dt.retention_days || ' days')::interval)
		ORDER BY cd.uploaded_at ASC
		LIMIT $1
		FOR UPDATE OF cd SKIP LOCKED
	`, j.batchSize)
	if err != nil {
		return fmt.Errorf("DocumentRetentionJob.Run: query candidates: %w", err)
	}

	archived := 0
	deleted := 0
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch model.DocumentRetentionPolicy(strings.ToUpper(strings.TrimSpace(candidate.RetentionPolicy))) {
		case model.DocumentRetentionPolicyArchive:
			if err := j.archiveOne(ctx, tx, candidate); err != nil {
				j.logger.Error("document archive failed", "document_id", candidate.DocumentID, "error", err)
				continue
			}
			archived++
		case model.DocumentRetentionPolicyDelete:
			if err := j.deleteOne(ctx, tx, candidate); err != nil {
				j.logger.Error("document delete failed", "document_id", candidate.DocumentID, "error", err)
				continue
			}
			deleted++
		default:
			j.logger.Warn("unknown retention policy", "document_id", candidate.DocumentID, "retention_policy", candidate.RetentionPolicy)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DocumentRetentionJob.Run: commit: %w", err)
	}

	j.logger.Info("document retention sweep complete",
		"checked", len(candidates),
		"archived", archived,
		"deleted", deleted,
		"batch_size", j.batchSize)
	return nil
}

// Start runs retention sweep in a ticker loop until context cancellation.
func (j *DocumentRetentionJob) Start(ctx context.Context) {
	ticker := time.NewTicker(j.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("document retention sweep stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("document retention sweep failed", "error", err)
			}
		}
	}
}

func (j *DocumentRetentionJob) archiveOne(ctx context.Context, tx *sqlx.Tx, candidate retentionCandidate) error {
	sourceReader, err := j.storage.Download(ctx, candidate.StoragePath)
	if err != nil {
		return fmt.Errorf("archiveOne: download source object: %w", err)
	}
	defer sourceReader.Close()

	archiveBucket := j.archiveBucket
	if archiveBucket == "" {
		originalBucket, _, parseErr := parseStoragePath(candidate.StoragePath)
		if parseErr != nil {
			return fmt.Errorf("archiveOne: parse source path: %w", parseErr)
		}
		archiveBucket = originalBucket + "-archive"
	}

	_, sourceKey, err := parseStoragePath(candidate.StoragePath)
	if err != nil {
		return fmt.Errorf("archiveOne: parse source key: %w", err)
	}
	archiveKey := fmt.Sprintf("archive/%s", sourceKey)
	archivePath, archiveURL, err := j.storage.Upload(ctx, archiveBucket, archiveKey, sourceReader, map[string]string{
		"archived_from": candidate.StoragePath,
		"document_id":   candidate.DocumentID,
	})
	if err != nil {
		return fmt.Errorf("archiveOne: upload archive object: %w", err)
	}
	if err := j.storage.Delete(ctx, candidate.StoragePath); err != nil {
		return fmt.Errorf("archiveOne: delete source object: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE case_documents
		SET status = 'ARCHIVED',
		    storage_path = $1,
		    storage_url = $2,
		    updated_at = now()
		WHERE id = $3::uuid
	`, archivePath, nilIfBlank(archiveURL), candidate.DocumentID)
	if err != nil {
		return fmt.Errorf("archiveOne: update metadata status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":            candidate.CaseID,
		"document_id":        candidate.DocumentID,
		"document_type_code": candidate.DocumentTypeCode,
		"action":             "ARCHIVE",
		"storage_path":       archivePath,
	})
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &candidate.CaseID,
		EventType:     model.EventDocumentArchived,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("archiveOne: publish DOCUMENT_ARCHIVED: %w", err)
	}
	return nil
}

func (j *DocumentRetentionJob) deleteOne(ctx context.Context, tx *sqlx.Tx, candidate retentionCandidate) error {
	if err := j.storage.Delete(ctx, candidate.StoragePath); err != nil {
		return fmt.Errorf("deleteOne: delete storage object: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE case_documents
		SET status = 'DELETED',
		    metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{retention_action}', to_jsonb('DELETE'::text), true),
		    updated_at = now()
		WHERE id = $1::uuid
	`, candidate.DocumentID)
	if err != nil {
		return fmt.Errorf("deleteOne: update metadata status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":            candidate.CaseID,
		"document_id":        candidate.DocumentID,
		"document_type_code": candidate.DocumentTypeCode,
		"action":             "DELETE",
	})
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &candidate.CaseID,
		EventType:     model.EventDocumentDeleted,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("deleteOne: publish DOCUMENT_DELETED: %w", err)
	}
	return nil
}
