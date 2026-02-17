package repository

import (
	"context"
	"fmt"

	"workflow-engine/pkg/model"
)

// RecordStageTransition atomically updates the case's current stage and
// inserts an audit row into case_stage_transitions.
//
// It automatically computes is_regression by comparing ordinals.
// The caller supplies a transaction (tx) so this can participate in a
// broader atomic operation.
func (r *Repository) RecordStageTransition(ctx context.Context, tx DBExecutor, input model.TransitionInput) error {
	if tx == nil {
		return fmt.Errorf("RecordStageTransition requires a non-nil transaction")
	}

	// ---------------------------------------------------------------
	// 1. Fetch the current case state (within the transaction)
	// ---------------------------------------------------------------
	var (
		currentStageCode    *string
		currentStageOrdinal int
		caseStatus          string
		rowVersion          int
	)
	err := tx.QueryRow(ctx, `
		SELECT current_stage_code, current_stage_ordinal, status, row_version
		FROM cases
		WHERE id = $1::uuid
		FOR UPDATE`, input.CaseID,
	).Scan(&currentStageCode, &currentStageOrdinal, &caseStatus, &rowVersion)
	if err != nil {
		return fmt.Errorf("failed to lock case %s: %w", input.CaseID, err)
	}

	// ---------------------------------------------------------------
	// 2. Guard: terminal cases cannot transition
	// ---------------------------------------------------------------
	if caseStatus == model.CaseStatusCompleted || caseStatus == model.CaseStatusCancelled {
		return fmt.Errorf("case %s is in terminal status %s", input.CaseID, caseStatus)
	}

	// ---------------------------------------------------------------
	// 3. Same-stage no-op
	// ---------------------------------------------------------------
	if currentStageCode != nil && *currentStageCode == input.ToStageCode && currentStageOrdinal == input.ToStageOrdinal {
		return nil // nothing to do
	}

	// ---------------------------------------------------------------
	// 4. Compute regression flag
	// ---------------------------------------------------------------
	isRegression := input.ToStageOrdinal < currentStageOrdinal

	if isRegression && (input.RegressionReason == nil || *input.RegressionReason == "") {
		return fmt.Errorf("regression_reason is required when transitioning backward (ordinal %d → %d)", currentStageOrdinal, input.ToStageOrdinal)
	}

	// ---------------------------------------------------------------
	// 5. Update the case row (optimistic lock via row_version)
	// ---------------------------------------------------------------
	tag, err := tx.Exec(ctx, `
		UPDATE cases
		SET current_stage_code    = $1,
		    current_stage_ordinal = $2,
		    status                = CASE WHEN status = 'OPEN' THEN 'IN_PROGRESS' ELSE status END,
		    row_version           = row_version + 1,
		    updated_at            = now()
		WHERE id = $3::uuid
		  AND row_version = $4`,
		input.ToStageCode,
		input.ToStageOrdinal,
		input.CaseID,
		rowVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to update case: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("optimistic lock failure on case %s (row_version %d)", input.CaseID, rowVersion)
	}

	// ---------------------------------------------------------------
	// 6. Insert audit row
	// ---------------------------------------------------------------
	_, err = tx.Exec(ctx, `
		INSERT INTO case_stage_transitions
		    (case_id, from_stage_code, from_stage_ordinal,
		     to_stage_code, to_stage_ordinal,
		     is_regression, regression_reason, triggered_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)`,
		input.CaseID,
		currentStageCode,
		nilIfZero(currentStageOrdinal),
		input.ToStageCode,
		input.ToStageOrdinal,
		isRegression,
		input.RegressionReason,
		input.TriggeredBy,
	)
	if err != nil {
		return fmt.Errorf("failed to insert stage transition: %w", err)
	}

	return nil
}

// nilIfZero returns nil for a zero ordinal (initial state), otherwise
// returns a pointer to the value.
func nilIfZero(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}
