package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// InsertAuditEntry — records a single audit event within a transaction
// ---------------------------------------------------------------------------

// InsertAuditEntry writes an audit trail row. It accepts a DBExecutor so it
// can participate in the caller's transaction (ensuring atomicity with the
// mutation it records).
func (r *Repository) InsertAuditEntry(ctx context.Context, tx DBExecutor, entry model.AuditEntry) error {
	if tx == nil {
		tx = r.Pool
	}

	changeDelta := entry.ChangeDelta
	if changeDelta == nil {
		changeDelta = json.RawMessage("{}")
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO audit_trail (
			case_id, action, entity_type, entity_id,
			actor_id, actor_type, change_delta, metadata
		) VALUES (
			$1::uuid, $2, $3, $4,
			$5, $6, $7::jsonb, $8::jsonb
		)`,
		entry.CaseID, entry.Action, entry.EntityType, entry.EntityID,
		entry.ActorID, entry.ActorType, changeDelta, entry.Metadata,
	)
	if err != nil {
		return fmt.Errorf("failed to insert audit entry: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GetAuditTrail — reads audit entries for a case (timeline view)
// ---------------------------------------------------------------------------

// GetAuditTrail fetches all audit entries for a case, ordered newest-first.
// Supports optional filtering by action and pagination via limit/offset.
func (r *Repository) GetAuditTrail(
	ctx context.Context,
	caseID string,
	filterAction string, // empty = all actions
	limit, offset int,
) ([]model.AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows interface{ Next() bool }
	var err error

	if filterAction != "" {
		rows, err = r.Pool.Query(ctx, `
			SELECT id, case_id, action, entity_type, entity_id,
			       actor_id, actor_type, change_delta, metadata, created_at
			FROM audit_trail
			WHERE case_id = $1::uuid AND action = $2
			ORDER BY created_at DESC
			LIMIT $3 OFFSET $4`,
			caseID, filterAction, limit, offset,
		)
	} else {
		rows, err = r.Pool.Query(ctx, `
			SELECT id, case_id, action, entity_type, entity_id,
			       actor_id, actor_type, change_delta, metadata, created_at
			FROM audit_trail
			WHERE case_id = $1::uuid
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3`,
			caseID, limit, offset,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query audit trail: %w", err)
	}

	// Type-assert to pgx.Rows to call Close and Scan
	type scannable interface {
		Next() bool
		Scan(dest ...any) error
		Close()
		Err() error
	}
	pgxRows := rows.(scannable)
	defer pgxRows.Close()

	var entries []model.AuditEntry
	for pgxRows.Next() {
		var e model.AuditEntry
		if err := pgxRows.Scan(
			&e.ID, &e.CaseID, &e.Action, &e.EntityType, &e.EntityID,
			&e.ActorID, &e.ActorType, &e.ChangeDelta, &e.Metadata, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, pgxRows.Err()
}
