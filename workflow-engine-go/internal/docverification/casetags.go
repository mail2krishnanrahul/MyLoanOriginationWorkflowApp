package docverification

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AddCaseTag adds a label to a case. Idempotent per (case_id, tag_code) pair.
// If a tag with the same code already exists and is active, it returns the
// existing tag without inserting a duplicate.
func AddCaseTag(ctx context.Context, pool *pgxpool.Pool,
	caseID, tenantID, tagCode string, tagValue *string, taggedBy string) (*model.CaseTag, error) {

	if caseID == "" || tagCode == "" || taggedBy == "" {
		return nil, fmt.Errorf("AddCaseTag: caseID, tagCode, taggedBy are required")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("AddCaseTag: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var tagID string
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		INSERT INTO case_tags (tenant_id, case_id, tag_code, tag_value, tagged_by, tagged_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6)
		ON CONFLICT (tenant_id, case_id, tag_code) WHERE removed_at IS NULL
		DO UPDATE SET tag_value = EXCLUDED.tag_value
		RETURNING tag_id::text
	`, tenantID, caseID, tagCode, tagValue, taggedBy, now).Scan(&tagID)
	if err != nil {
		return nil, fmt.Errorf("AddCaseTag: insert: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":   caseID,
		"tag_code":  tagCode,
		"tag_value": tagValue,
		"tagged_by": taggedBy,
		"timestamp": now,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseTagAdded, payload); err != nil {
		return nil, fmt.Errorf("AddCaseTag: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("AddCaseTag: commit: %w", err)
	}

	slog.Debug("case tag added", "case_id", caseID, "tag_code", tagCode)
	return &model.CaseTag{
		TagID:    tagID,
		TenantID: tenantID,
		CaseID:   caseID,
		TagCode:  tagCode,
		TagValue: tagValue,
		TaggedBy: taggedBy,
		TaggedAt: now,
	}, nil
}

// RemoveCaseTag soft-deletes (tombstones) an active tag by setting removed_at.
func RemoveCaseTag(ctx context.Context, pool *pgxpool.Pool, tagID, tenantID, removedBy string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("RemoveCaseTag: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var caseID, tagCode string
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `
		UPDATE case_tags
		SET removed_at = $3, removed_by = $2::uuid
		WHERE tag_id = $1::uuid AND tenant_id = $4::uuid AND removed_at IS NULL
		RETURNING case_id::text, tag_code
	`, tagID, removedBy, now, tenantID).Scan(&caseID, &tagCode)
	if err != nil {
		return fmt.Errorf("RemoveCaseTag: update: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":    caseID,
		"tag_id":     tagID,
		"tag_code":   tagCode,
		"removed_by": removedBy,
		"timestamp":  now,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseTagRemoved, payload); err != nil {
		return fmt.Errorf("RemoveCaseTag: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("RemoveCaseTag: commit: %w", err)
	}

	slog.Debug("case tag removed", "case_id", caseID, "tag_code", tagCode)
	return nil
}

// GetActiveCaseTags returns all currently active (non-removed) tags for a case.
func GetActiveCaseTags(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) ([]model.CaseTag, error) {
	rows, err := pool.Query(ctx, `
		SELECT tag_id::text, tenant_id::text, case_id::text,
		       tag_code, tag_value, tagged_by::text, tagged_at
		FROM case_tags
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid
		  AND removed_at IS NULL
		ORDER BY tagged_at ASC
	`, caseID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("GetActiveCaseTags: query: %w", err)
	}
	defer rows.Close()

	var tags []model.CaseTag
	for rows.Next() {
		var t model.CaseTag
		if err := rows.Scan(
			&t.TagID, &t.TenantID, &t.CaseID,
			&t.TagCode, &t.TagValue, &t.TaggedBy, &t.TaggedAt,
		); err != nil {
			return nil, fmt.Errorf("GetActiveCaseTags: scan: %w", err)
		}
		tags = append(tags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetActiveCaseTags: rows: %w", err)
	}
	return tags, nil
}
