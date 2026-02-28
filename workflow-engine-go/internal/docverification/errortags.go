package docverification

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AddDocumentErrorTag adds an error tag to a case document.
// If the tag is BLOCKING and no other BLOCKING tags existed before,
// the document status is updated to ERROR_TAGGED.
func AddDocumentErrorTag(ctx context.Context, pool *pgxpool.Pool, input model.AddErrorTagInput) (*model.DocumentErrorTag, error) {
	if input.TenantID == "" || input.DocumentID == "" || input.CaseID == "" {
		return nil, fmt.Errorf("AddDocumentErrorTag: tenantID, documentID, CaseID are required")
	}
	if input.Severity == "" {
		input.Severity = model.ErrorSeverityMajor
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("AddDocumentErrorTag: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Check QA lock — reject direct changes if QA-locked.
	var qaLocked bool
	_ = tx.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'qa_locked')::bool, false)
		FROM cases WHERE id = $1::uuid
	`, input.CaseID).Scan(&qaLocked)
	if qaLocked {
		return nil, model.ErrCaseQALocked
	}

	var tagID string
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		INSERT INTO document_error_tags (
			tenant_id, document_id, case_id,
			error_code, error_description, severity,
			tagged_by, tagged_at
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid,
			$4, $5, $6,
			$7::uuid, $8
		)
		RETURNING tag_id::text
	`,
		input.TenantID, input.DocumentID, input.CaseID,
		string(input.ErrorCode), input.ErrorDescription, string(input.Severity),
		input.TaggedBy, now,
	).Scan(&tagID)
	if err != nil {
		return nil, fmt.Errorf("AddDocumentErrorTag: insert: %w", err)
	}

	// Update document status to ERROR_TAGGED.
	_, err = tx.Exec(ctx, `
		UPDATE case_documents
		SET status = 'ERROR_TAGGED', updated_at = now()
		WHERE document_id = $1::uuid
		  AND status NOT IN ('ACCEPTED')
	`, input.DocumentID)
	if err != nil {
		return nil, fmt.Errorf("AddDocumentErrorTag: update doc status: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":     input.CaseID,
		"document_id": input.DocumentID,
		"tag_id":      tagID,
		"error_code":  input.ErrorCode,
		"severity":    input.Severity,
		"tagged_by":   input.TaggedBy,
		"timestamp":   now,
	}
	if err := publishEventInTx(ctx, tx, input.CaseID, model.EventDocumentErrorTagged, payload); err != nil {
		return nil, fmt.Errorf("AddDocumentErrorTag: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("AddDocumentErrorTag: commit: %w", err)
	}

	slog.Info("document error tagged",
		"tag_id", tagID,
		"case_id", input.CaseID,
		"error_code", input.ErrorCode,
		"severity", input.Severity)

	return &model.DocumentErrorTag{
		TagID:      tagID,
		TenantID:   input.TenantID,
		DocumentID: input.DocumentID,
		CaseID:     input.CaseID,
		ErrorCode:  string(input.ErrorCode),
		Severity:   input.Severity,
		TaggedBy:   input.TaggedBy,
		TaggedAt:   now,
	}, nil
}

// ResolveDocumentErrorTag marks an existing error tag as resolved.
func ResolveDocumentErrorTag(ctx context.Context, pool *pgxpool.Pool, tagID, resolvedByUserID, tenantID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ResolveDocumentErrorTag: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var caseID, documentID string
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		UPDATE document_error_tags
		SET resolved_at = $3,
		    resolved_by = $2::uuid
		WHERE tag_id = $1::uuid
		  AND tenant_id = $4::uuid
		  AND resolved_at IS NULL
		RETURNING case_id::text, document_id::text
	`, tagID, resolvedByUserID, now, tenantID).Scan(&caseID, &documentID)
	if err != nil {
		return fmt.Errorf("ResolveDocumentErrorTag: update: %w", err)
	}

	// If all error tags on this document are resolved, update status back to REVIEWED.
	var unresolvedCount int
	_ = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM document_error_tags
		WHERE document_id = $1::uuid AND resolved_at IS NULL
	`, documentID).Scan(&unresolvedCount)

	if unresolvedCount == 0 {
		_, _ = tx.Exec(ctx, `
			UPDATE case_documents
			SET status = 'REVIEWED', updated_at = now()
			WHERE document_id = $1::uuid
		`, documentID)
	}

	payload := map[string]interface{}{
		"case_id":     caseID,
		"document_id": documentID,
		"tag_id":      tagID,
		"resolved_by": resolvedByUserID,
		"timestamp":   now,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventDocumentErrorResolved, payload); err != nil {
		return fmt.Errorf("ResolveDocumentErrorTag: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ResolveDocumentErrorTag: commit: %w", err)
	}

	slog.Info("document error tag resolved", "tag_id", tagID, "case_id", caseID)
	return nil
}

// GetDocumentErrorTags returns all error tags for a case, optionally only unresolved.
func GetDocumentErrorTags(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string, unresolvedOnly bool) ([]model.DocumentErrorTag, error) {
	query := `
		SELECT tag_id::text, tenant_id::text, document_id::text, case_id::text,
		       error_code, error_description, severity,
		       tagged_by::text, tagged_at, resolved_at, resolved_by::text, created_at
		FROM document_error_tags
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid
	`
	if unresolvedOnly {
		query += " AND resolved_at IS NULL"
	}
	query += " ORDER BY tagged_at ASC"

	rows, err := pool.Query(ctx, query, caseID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("GetDocumentErrorTags: query: %w", err)
	}
	defer rows.Close()

	var tags []model.DocumentErrorTag
	for rows.Next() {
		var t model.DocumentErrorTag
		if err := rows.Scan(
			&t.TagID, &t.TenantID, &t.DocumentID, &t.CaseID,
			&t.ErrorCode, &t.ErrorDescription, &t.Severity,
			&t.TaggedBy, &t.TaggedAt, &t.ResolvedAt, &t.ResolvedBy, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetDocumentErrorTags: scan: %w", err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetDocumentErrorTags: rows: %w", err)
	}
	return tags, nil
}

// HasUnresolvedBlockingTags returns true if the case has any unresolved BLOCKING error tags.
func HasUnresolvedBlockingTags(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) (bool, error) {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM document_error_tags
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid
		  AND severity = 'BLOCKING' AND resolved_at IS NULL
	`, caseID, tenantID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("HasUnresolvedBlockingTags: %w", err)
	}
	return count > 0, nil
}
