package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-engine/internal/document"
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
		caseTypeCode        string
		caseTypeVersion     int
	)
	err := tx.QueryRow(ctx, `
		SELECT c.current_stage_code,
		       c.current_stage_ordinal,
		       c.status,
		       c.row_version,
		       ct.code AS case_type_code,
		       ct.version AS case_type_version
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.id = $1::uuid
		FOR UPDATE`, input.CaseID,
	).Scan(&currentStageCode, &currentStageOrdinal, &caseStatus, &rowVersion, &caseTypeCode, &caseTypeVersion)
	if err != nil {
		return fmt.Errorf("failed to lock case %s: %w", input.CaseID, err)
	}

	// ---------------------------------------------------------------
	// 2. Guard: terminal cases cannot transition
	// ---------------------------------------------------------------
	if caseStatus == model.CaseStatusCompleted || caseStatus == model.CaseStatusCancelled || caseStatus == model.CaseStatusRejected {
		return fmt.Errorf("case %s is in terminal status %s", input.CaseID, caseStatus)
	}

	// ---------------------------------------------------------------
	// 3. Same-stage no-op
	// ---------------------------------------------------------------
	if currentStageCode != nil && *currentStageCode == input.ToStageCode && currentStageOrdinal == input.ToStageOrdinal {
		return nil // nothing to do
	}

	// ---------------------------------------------------------------
	// 3b. Guard: required documents for current stage must be fulfilled
	// ---------------------------------------------------------------
	if r.SQLX != nil && currentStageCode != nil && strings.TrimSpace(*currentStageCode) != "" {
		fulfilled, missingTypes, reqErr := document.CheckDocumentRequirements(ctx, r.SQLX, input.CaseID, *currentStageCode)
		if reqErr != nil {
			return fmt.Errorf("RecordStageTransition: check document requirements: %w", reqErr)
		}
		if !fulfilled {
			missingCodes := make([]string, 0, len(missingTypes))
			for _, item := range missingTypes {
				missingCodes = append(missingCodes, item.DocumentTypeCode)
			}
			reason := fmt.Sprintf("Missing required documents: %v", missingCodes)
			payload, _ := json.Marshal(map[string]interface{}{
				"case_id":             input.CaseID,
				"from_stage_code":     *currentStageCode,
				"to_stage_code":       input.ToStageCode,
				"missing_document_types": missingCodes,
				"reason":              reason,
			})
			_ = r.PublishEvent(ctx, tx, model.Event{
				CaseID:        &input.CaseID,
				EventType:     model.EventStageTransitionBlocked,
				Payload:       payload,
				Status:        model.EventStatusPending,
				TargetService: "case-orchestrator",
			})
			return fmt.Errorf("RecordStageTransition: %s", reason)
		}
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

	// Create placeholder requests for document requirements that become active
	// at the newly entered stage.
	_, err = tx.Exec(ctx, `
		INSERT INTO document_requests (
			case_id,
			case_type_code,
			case_type_version,
			document_type_code,
			required_at_stage,
			required_count_min,
			required_count_max,
			current_count,
			status,
			requested_at
		)
		SELECT
			$1::uuid,
			dt.case_type_code,
			dt.case_type_version,
			dt.document_type_code,
			dt.required_at_stage,
			dt.required_count_min,
			dt.required_count_max,
			0,
			'PENDING',
			now()
		FROM document_types dt
		WHERE dt.case_type_code = $2
		  AND dt.case_type_version = $3
		  AND dt.required_at_stage = $4
		  AND dt.required_count_min > 0
		ON CONFLICT (case_id, document_type_code, required_at_stage)
		DO NOTHING
	`, input.CaseID, caseTypeCode, caseTypeVersion, input.ToStageCode)
	if err != nil {
		return fmt.Errorf("failed to initialize stage document requirements: %w", err)
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
