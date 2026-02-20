package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

// CheckAndRecordIdempotencyKey inserts a key into the unified key table.
// Duplicates return (true, nil).
func CheckAndRecordIdempotencyKey(
	ctx context.Context,
	tx *sqlx.Tx,
	keyspace IdempotencyKeyspace,
	key string,
	tenantID string,
	expiresAt time.Time,
) (bool, error) {
	if tx == nil {
		return false, fmt.Errorf("CheckAndRecordIdempotencyKey: tx is nil")
	}
	key = strings.TrimSpace(key)
	tenantID = strings.TrimSpace(tenantID)
	if key == "" {
		return false, fmt.Errorf("CheckAndRecordIdempotencyKey: key is required")
	}
	if tenantID == "" {
		return false, fmt.Errorf("CheckAndRecordIdempotencyKey: tenantID is required")
	}
	if strings.TrimSpace(string(keyspace)) == "" {
		return false, fmt.Errorf("CheckAndRecordIdempotencyKey: keyspace is required")
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(24 * time.Hour)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_keys (keyspace, key, tenant_id, expires_at)
		VALUES ($1, $2, $3::uuid, $4)
	`, string(keyspace), key, tenantID, expiresAt.UTC())
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return true, nil
		}
		return false, fmt.Errorf("CheckAndRecordIdempotencyKey: insert key: %w", err)
	}
	return false, nil
}

// IdempotencyKeyCleanupJob prunes expired idempotency keys.
type IdempotencyKeyCleanupJob struct {
	db        *sqlx.DB
	interval  time.Duration
	batchSize int
	logger    *slog.Logger
}

// NewIdempotencyKeyCleanupJob creates a key cleanup job.
func NewIdempotencyKeyCleanupJob(db *sqlx.DB, interval time.Duration, batchSize int, logger *slog.Logger) *IdempotencyKeyCleanupJob {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 50000
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &IdempotencyKeyCleanupJob{db: db, interval: interval, batchSize: batchSize, logger: logger}
}

// Run executes one cleanup cycle.
func (j *IdempotencyKeyCleanupJob) Run(ctx context.Context) error {
	if j == nil {
		return fmt.Errorf("IdempotencyKeyCleanupJob.Run: job is nil")
	}
	if j.db == nil {
		return fmt.Errorf("IdempotencyKeyCleanupJob.Run: db is nil")
	}

	result, err := j.db.ExecContext(ctx, `
		WITH doomed AS (
			SELECT keyspace, key
			FROM idempotency_keys
			WHERE expires_at < now()
			ORDER BY expires_at ASC
			LIMIT $1
		)
		DELETE FROM idempotency_keys k
		USING doomed d
		WHERE k.keyspace = d.keyspace
		  AND k.key = d.key
	`, j.batchSize)
	if err != nil {
		return fmt.Errorf("IdempotencyKeyCleanupJob.Run: delete expired keys: %w", err)
	}
	rows, _ := result.RowsAffected()
	j.logger.Info("idempotency cleanup completed", "rows_deleted", rows)
	return nil
}

// Start runs the cleanup loop until context cancellation.
func (j *IdempotencyKeyCleanupJob) Start(ctx context.Context) {
	if j == nil {
		return
	}
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("idempotency cleanup stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("idempotency cleanup failed", "error", err)
			}
		}
	}
}

// IngestedEventKeyCleanupJob aggressively prunes external ingestion keys older than 7 days.
type IngestedEventKeyCleanupJob struct {
	db        *sqlx.DB
	interval  time.Duration
	batchSize int
	logger    *slog.Logger
}

// NewIngestedEventKeyCleanupJob creates cleanup job for external ingestion keyspace.
func NewIngestedEventKeyCleanupJob(db *sqlx.DB, interval time.Duration, batchSize int, logger *slog.Logger) *IngestedEventKeyCleanupJob {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if batchSize <= 0 {
		batchSize = 50000
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &IngestedEventKeyCleanupJob{db: db, interval: interval, batchSize: batchSize, logger: logger}
}

// Run executes one cleanup cycle for ingestion keys.
func (j *IngestedEventKeyCleanupJob) Run(ctx context.Context) error {
	if j == nil {
		return fmt.Errorf("IngestedEventKeyCleanupJob.Run: job is nil")
	}
	if j.db == nil {
		return fmt.Errorf("IngestedEventKeyCleanupJob.Run: db is nil")
	}

	result, err := j.db.ExecContext(ctx, `
		WITH doomed AS (
			SELECT key
			FROM idempotency_keys
			WHERE keyspace = $1
			  AND created_at < now() - interval '7 days'
			ORDER BY created_at ASC
			LIMIT $2
		)
		DELETE FROM idempotency_keys k
		USING doomed d
		WHERE k.keyspace = $1
		  AND k.key = d.key
	`, string(IdempotencyKeyspaceExternalEventIngestion), j.batchSize)
	if err != nil {
		return fmt.Errorf("IngestedEventKeyCleanupJob.Run: delete keys: %w", err)
	}
	rows, _ := result.RowsAffected()
	j.logger.Info("ingested-event key cleanup completed", "rows_deleted", rows)
	return nil
}

// Start runs the cleanup loop until context cancellation.
func (j *IngestedEventKeyCleanupJob) Start(ctx context.Context) {
	if j == nil {
		return
	}
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("ingested-event cleanup stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("ingested-event key cleanup failed", "error", err)
			}
		}
	}
}
