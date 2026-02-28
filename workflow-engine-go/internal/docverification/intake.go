package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HandleCaseCreated processes a newly created HOME_LOAN_DOC_VERIFICATION case.
// It transitions the case to IN_PROGRESS, sets the initial stage, and creates
// all INTAKE stage tasks by publishing a CASE_STAGE_CHANGED event.
// This is called from the orchestrator's CASE_CREATED event handler.
func HandleCaseCreated(ctx context.Context, pool *pgxpool.Pool, event model.Event) error {
	if event.CaseID == nil || *event.CaseID == "" {
		slog.Warn("HandleCaseCreated: missing case_id in event", "event_id", event.ID)
		return nil
	}
	caseID := *event.CaseID

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("HandleCaseCreated: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Verify the case exists and has the correct case type.
	var caseTypeCode string
	err = tx.QueryRow(ctx, `
		SELECT ct.code
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.id = $1::uuid
		FOR UPDATE
	`, caseID).Scan(&caseTypeCode)
	if err != nil {
		return fmt.Errorf("HandleCaseCreated: load case type: %w", err)
	}
	if caseTypeCode != CaseTypeCode {
		// Not our case type — skip silently.
		return nil
	}

	// 2. Transition to IN_PROGRESS and set initial stage.
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET status               = 'IN_PROGRESS',
		    current_stage_code   = $2,
		    current_stage_ordinal = 1,
		    row_version          = row_version + 1,
		    updated_at           = now()
		WHERE id = $1::uuid
		  AND status = 'OPEN'
	`, caseID, StageIntake)
	if err != nil {
		return fmt.Errorf("HandleCaseCreated: update case to IN_PROGRESS: %w", err)
	}

	// 3. Publish CASE_STAGE_CHANGED so the orchestrator creates INTAKE tasks.
	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":        caseID,
		"from_stage":     "",
		"to_stage":       StageIntake,
		"to_stage_order": 1,
	})

	_, err = tx.Exec(ctx, `
		INSERT INTO events_outbox (case_id, event_type, payload, target_service, status)
		VALUES ($1::uuid, $2, $3, 'case-orchestrator', 'PENDING')
	`, caseID, model.EventCaseStageChanged, payload)
	if err != nil {
		return fmt.Errorf("HandleCaseCreated: publish CASE_STAGE_CHANGED: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("HandleCaseCreated: commit: %w", err)
	}

	slog.Info("case intake started",
		"case_id", caseID,
		"initial_stage", StageIntake,
		"timestamp", time.Now().UTC())
	return nil
}

// HandleCaseIntakeFailed handles errors encountered during the INTAKE stage
// (e.g., ECM unavailable, invalid deal reference). It marks the case with
// metadata flags and raises an exception for supervisor review.
func HandleCaseIntakeFailed(ctx context.Context, pool *pgxpool.Pool, caseID, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("HandleCaseIntakeFailed: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	metaUpdate := map[string]interface{}{
		"intake_failed_at":      time.Now().UTC(),
		"intake_failure_reason": reason,
	}
	metaJSON, _ := json.Marshal(metaUpdate)

	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET metadata   = metadata || $2::jsonb,
		    updated_at = now()
		WHERE id = $1::uuid
	`, caseID, metaJSON)
	if err != nil {
		return fmt.Errorf("HandleCaseIntakeFailed: update case metadata: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id": caseID,
		"reason":  reason,
		"stage":   StageIntake,
	})
	_, err = tx.Exec(ctx, `
		INSERT INTO events_outbox (case_id, event_type, payload, target_service, status)
		VALUES ($1::uuid, $2, $3, 'case-orchestrator', 'PENDING')
	`, caseID, model.EventCaseIntakeFailed, payload)
	if err != nil {
		return fmt.Errorf("HandleCaseIntakeFailed: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("HandleCaseIntakeFailed: commit: %w", err)
	}

	slog.Warn("case intake failed", "case_id", caseID, "reason", reason)
	return nil
}

// publishEventInTx is a helper used across all handlers to insert into the
// transactional outbox within an existing transaction.
func publishEventInTx(ctx context.Context, tx pgx.Tx, caseID string, eventType model.EventType, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publishEventInTx: marshal payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO events_outbox (case_id, event_type, payload, target_service, status)
		VALUES ($1::uuid, $2, $3, 'case-orchestrator', 'PENDING')
	`, caseID, string(eventType), payloadBytes)
	return err
}
