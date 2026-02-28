package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TriggerQAReview transitions a case into QA_REVIEW stage:
// 1. Guards: no unresolved BLOCKING error tags, no incomplete blocking tasks.
// 2. Sets metadata qa_locked=true on the case (staging guard for all update paths).
// 3. Publishes CASE_SUBMITTED_FOR_QA.
func TriggerQAReview(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID, submittedByUserID string) error {
	// Pre-checks (read-only, before starting the write tx).
	hasBlocking, err := HasUnresolvedBlockingTags(ctx, pool, caseID, tenantID)
	if err != nil {
		return fmt.Errorf("TriggerQAReview: check blocking tags: %w", err)
	}
	if hasBlocking {
		return model.ErrUnresolvedBlockingTags
	}

	// Check for incomplete is_blocking tasks.
	var blockingIncomplete int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tasks
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid
		  AND is_blocking = true
		  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'DONE')
	`, caseID, tenantID).Scan(&blockingIncomplete)
	if err != nil {
		return fmt.Errorf("TriggerQAReview: check blocking tasks: %w", err)
	}
	if blockingIncomplete > 0 {
		return model.ErrBlockingTasksRemain
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("TriggerQAReview: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Set qa_locked on the case and advance to QA_REVIEW stage.
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET current_stage_code    = $2,
		    current_stage_ordinal = 6,
		    metadata              = metadata || jsonb_build_object(
		        'qa_locked',         true,
		        'qa_locked_at',      now()::text,
		        'qa_submitted_by',   $3::text
		    ),
		    updated_at            = now(),
		    row_version           = row_version + 1
		WHERE id = $1::uuid
	`, caseID, StageQAReview, submittedByUserID)
	if err != nil {
		return fmt.Errorf("TriggerQAReview: update case: %w", err)
	}

	// Publish stage change and QA submission events.
	stagePayload := map[string]interface{}{
		"case_id": caseID, "to_stage": StageQAReview, "to_stage_order": 6,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseStageChanged, stagePayload); err != nil {
		return fmt.Errorf("TriggerQAReview: publish stage change: %w", err)
	}

	qaPayload := map[string]interface{}{
		"case_id":      caseID,
		"submitted_by": submittedByUserID,
		"submitted_at": time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseSubmittedForQA, qaPayload); err != nil {
		return fmt.Errorf("TriggerQAReview: publish QA event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("TriggerQAReview: commit: %w", err)
	}

	slog.Info("case submitted for QA review", "case_id", caseID, "submitted_by", submittedByUserID)
	return nil
}

// AssertCaseNotQALocked returns ErrCaseQALocked if the case is currently QA-locked.
// Call this at the beginning of any mutation path that should be blocked during QA.
func AssertCaseNotQALocked(ctx context.Context, pool *pgxpool.Pool, caseID string) error {
	var qaLocked bool
	err := pool.QueryRow(ctx, `
		SELECT COALESCE((metadata->>'qa_locked')::bool, false)
		FROM cases WHERE id = $1::uuid
	`, caseID).Scan(&qaLocked)
	if err != nil {
		return fmt.Errorf("AssertCaseNotQALocked: %w", err)
	}
	if qaLocked {
		return model.ErrCaseQALocked
	}
	return nil
}

// StageChangeForQA records a change that was blocked by the QA lock.
// The change is staged in qa_staged_changes for later application/discard by QA.
func StageChangeForQA(
	ctx context.Context,
	pool *pgxpool.Pool,
	caseID, tenantID, submittedBy string,
	changeType model.QAStagedChangeType,
	payload interface{},
) (string, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("StageChangeForQA: marshal payload: %w", err)
	}

	var changeID string
	err = pool.QueryRow(ctx, `
		INSERT INTO qa_staged_changes (
			tenant_id, case_id, change_type, payload, submitted_by
		) VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, $5::uuid)
		RETURNING change_id::text
	`, tenantID, caseID, string(changeType), payloadBytes, submittedBy).Scan(&changeID)
	if err != nil {
		return "", fmt.Errorf("StageChangeForQA: insert: %w", err)
	}

	slog.Info("change staged during QA lock",
		"change_id", changeID, "case_id", caseID, "change_type", changeType)
	return changeID, nil
}

// QAApprove approves the case from QA review, unlocks it, and publishes QA_APPROVED.
// Staged changes are applied automatically.
func QAApprove(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID, approvedByUserID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("QAApprove: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Unlock the QA lock and mark case as COMPLETED.
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET status       = 'COMPLETED',
		    completed_at = $3,
		    metadata     = metadata ||
		        jsonb_build_object(
		            'qa_locked',     false,
		            'qa_approved_at', $3::text,
		            'qa_approved_by', $2::text
		        ),
		    updated_at   = $3,
		    row_version  = row_version + 1
		WHERE id = $1::uuid
	`, caseID, approvedByUserID, now)
	if err != nil {
		return fmt.Errorf("QAApprove: update case: %w", err)
	}

	// Mark all pending staged changes as APPLIED.
	_, err = tx.Exec(ctx, `
		UPDATE qa_staged_changes
		SET status = 'APPLIED', applied_at = $3, applied_by = $2::uuid
		WHERE case_id = $1::uuid AND tenant_id = $4::uuid AND status = 'PENDING'
	`, caseID, approvedByUserID, now, tenantID)
	if err != nil {
		return fmt.Errorf("QAApprove: apply staged changes: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":     caseID,
		"approved_by": approvedByUserID,
		"approved_at": now,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventQAApproved, payload); err != nil {
		return fmt.Errorf("QAApprove: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("QAApprove: commit: %w", err)
	}

	slog.Info("case QA approved", "case_id", caseID, "approved_by", approvedByUserID)
	return nil
}

// QAReject rejects the case from QA and sends it back for remediation.
// Staged changes are marked DISCARDED. Case is unlocked for remediation.
func QAReject(
	ctx context.Context,
	pool *pgxpool.Pool,
	caseID, tenantID, rejectedByUserID string,
	rejectionItems []model.QARejectionItem,
) error {
	if len(rejectionItems) == 0 {
		return fmt.Errorf("QAReject: at least one rejection item is required")
	}

	rejectionJSON, err := json.Marshal(rejectionItems)
	if err != nil {
		return fmt.Errorf("QAReject: marshal rejection items: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("QAReject: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET current_stage_code    = $2,
		    current_stage_ordinal = 7,
		    metadata              = metadata || jsonb_build_object(
		        'qa_locked',          false,
		        'qa_rejected_at',     $4::text,
		        'qa_rejected_by',     $3::text,
		        'qa_rejection_items', $5::jsonb
		    ),
		    updated_at            = $4,
		    row_version           = row_version + 1
		WHERE id = $1::uuid
	`, caseID, StageQARemediation, rejectedByUserID, now, rejectionJSON)
	if err != nil {
		return fmt.Errorf("QAReject: update case: %w", err)
	}

	// Discard all pending staged changes.
	_, _ = tx.Exec(ctx, `
		UPDATE qa_staged_changes
		SET status = 'DISCARDED'
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid AND status = 'PENDING'
	`, caseID, tenantID)

	// Publish stage change to QA_REMEDIATION.
	stagePayload := map[string]interface{}{
		"case_id": caseID, "from_stage": StageQAReview,
		"to_stage": StageQARemediation, "to_stage_order": 7, "is_regression": true,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseStageChanged, stagePayload); err != nil {
		return fmt.Errorf("QAReject: publish stage change: %w", err)
	}

	qaPayload := map[string]interface{}{
		"case_id":         caseID,
		"rejected_by":     rejectedByUserID,
		"rejected_at":     now,
		"rejection_items": rejectionItems,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventQARejected, qaPayload); err != nil {
		return fmt.Errorf("QAReject: publish QA_REJECTED: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("QAReject: commit: %w", err)
	}

	slog.Info("case QA rejected — sent to remediation",
		"case_id", caseID,
		"rejected_by", rejectedByUserID,
		"rejection_items_count", len(rejectionItems))
	return nil
}

// SubmitQARemediation is called by the loan officer after addressing QA rejection items.
// It re-locks the case and publishes CASE_RESUBMITTED_FOR_QA.
func SubmitQARemediation(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID, resubmittedByUserID string) error {
	hasBlocking, err := HasUnresolvedBlockingTags(ctx, pool, caseID, tenantID)
	if err != nil {
		return fmt.Errorf("SubmitQARemediation: check blocking tags: %w", err)
	}
	if hasBlocking {
		return model.ErrUnresolvedBlockingTags
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("SubmitQARemediation: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET current_stage_code    = $2,
		    current_stage_ordinal = 6,
		    metadata              = metadata || jsonb_build_object(
		        'qa_locked',              true,
		        'qa_resubmitted_at',      $3::text,
		        'qa_resubmitted_by',      $4::text
		    ),
		    updated_at            = $3,
		    row_version           = row_version + 1
		WHERE id = $1::uuid
		  AND current_stage_code = $5
	`, caseID, StageQAReview, now, resubmittedByUserID, StageQARemediation)
	if err != nil {
		return fmt.Errorf("SubmitQARemediation: update case: %w", err)
	}

	stagePayload := map[string]interface{}{
		"case_id": caseID, "from_stage": StageQARemediation,
		"to_stage": StageQAReview, "to_stage_order": 6,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseStageChanged, stagePayload); err != nil {
		return fmt.Errorf("SubmitQARemediation: publish stage change: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":        caseID,
		"resubmitted_by": resubmittedByUserID,
		"resubmitted_at": now,
	}
	if err := publishEventInTx(ctx, tx, caseID, model.EventCaseResubmittedForQA, payload); err != nil {
		return fmt.Errorf("SubmitQARemediation: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("SubmitQARemediation: commit: %w", err)
	}

	slog.Info("case resubmitted for QA", "case_id", caseID, "by", resubmittedByUserID)
	return nil
}

// GetQAStagedChanges returns all pending staged changes for a QA-locked case.
func GetQAStagedChanges(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) ([]model.QAStagedChange, error) {
	rows, err := pool.Query(ctx, `
		SELECT change_id::text, tenant_id::text, case_id::text,
		       change_type, payload, submitted_by::text,
		       submitted_at, status, applied_at, applied_by::text
		FROM qa_staged_changes
		WHERE case_id = $1::uuid AND tenant_id = $2::uuid
		ORDER BY submitted_at ASC
	`, caseID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("GetQAStagedChanges: query: %w", err)
	}
	defer rows.Close()

	var changes []model.QAStagedChange
	for rows.Next() {
		var c model.QAStagedChange
		var payloadRaw []byte
		if err := rows.Scan(
			&c.ChangeID, &c.TenantID, &c.CaseID,
			&c.ChangeType, &payloadRaw, &c.SubmittedBy,
			&c.SubmittedAt, &c.Status, &c.AppliedAt, &c.AppliedBy,
		); err != nil {
			return nil, fmt.Errorf("GetQAStagedChanges: scan: %w", err)
		}
		c.Payload = json.RawMessage(payloadRaw)
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetQAStagedChanges: rows: %w", err)
	}
	return changes, nil
}
