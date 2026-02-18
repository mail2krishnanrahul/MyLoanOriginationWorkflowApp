package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
)

// GetCaseInstance fetches a case by ID.
func (r *Repository) GetCaseInstance(ctx context.Context, tx DBExecutor, caseID string) (*model.CaseInstance, error) {
	if tx == nil {
		tx = r.Pool
	}
	var c model.CaseInstance
	err := tx.QueryRow(ctx, `
		SELECT id, reference_number, case_type_id, case_type_version,
		       parent_case_id, source_case_id, current_stage_code, current_stage_ordinal,
		       status, metadata, assigned_to, rework_count, max_rework_attempts, row_version,
		       created_at, updated_at, completed_at,
		       suspend_reason, resume_at, withdrawal_reason,
		       emergency_closed_at, emergency_reason, supervisor_id
		FROM cases
		WHERE id = $1::uuid`, caseID,
	).Scan(
		&c.ID, &c.ReferenceNumber, &c.CaseTypeID, &c.CaseTypeVersion,
		&c.ParentCaseID, &c.SourceCaseID, &c.CurrentStageCode, &c.CurrentStageOrdinal,
		&c.Status, &c.Metadata, &c.AssignedTo, &c.ReworkCount, &c.MaxReworkAttempts, &c.RowVersion,
		&c.CreatedAt, &c.UpdatedAt, &c.CompletedAt,
		&c.SuspendReason, &c.ResumeAt, &c.WithdrawalReason,
		&c.EmergencyClosedAt, &c.EmergencyReason, &c.SupervisorID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch case %s: %w", caseID, err)
	}
	return &c, nil
}

// GetCaseInstanceWithLock fetches a case by ID with a FOR UPDATE row lock
// for use within transactions that need to prevent concurrent modifications.
func (r *Repository) GetCaseInstanceWithLock(ctx context.Context, tx DBExecutor, caseID string) (*model.CaseInstance, error) {
	if tx == nil {
		tx = r.Pool
	}
	var c model.CaseInstance
	err := tx.QueryRow(ctx, `
		SELECT id, reference_number, case_type_id, case_type_version,
		       parent_case_id, source_case_id, current_stage_code, current_stage_ordinal,
		       status, metadata, assigned_to, rework_count, max_rework_attempts, row_version,
		       created_at, updated_at, completed_at,
		       suspend_reason, resume_at, withdrawal_reason,
		       emergency_closed_at, emergency_reason, supervisor_id
		FROM cases
		WHERE id = $1::uuid
		FOR UPDATE`, caseID,
	).Scan(
		&c.ID, &c.ReferenceNumber, &c.CaseTypeID, &c.CaseTypeVersion,
		&c.ParentCaseID, &c.SourceCaseID, &c.CurrentStageCode, &c.CurrentStageOrdinal,
		&c.Status, &c.Metadata, &c.AssignedTo, &c.ReworkCount, &c.MaxReworkAttempts, &c.RowVersion,
		&c.CreatedAt, &c.UpdatedAt, &c.CompletedAt,
		&c.SuspendReason, &c.ResumeAt, &c.WithdrawalReason,
		&c.EmergencyClosedAt, &c.EmergencyReason, &c.SupervisorID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch case %s with lock: %w", caseID, err)
	}
	return &c, nil
}

// GetCaseType fetches a case type by ID and unmarshals its JSONB config.
func (r *Repository) GetCaseType(ctx context.Context, tx DBExecutor, caseTypeID string) (*model.CaseType, error) {
	if tx == nil {
		tx = r.Pool
	}
	var ct model.CaseType
	var configRaw []byte
	err := tx.QueryRow(ctx, `
		SELECT id, code, version, name, description, config,
		       status, created_at, updated_at, deprecated_at
		FROM case_types
		WHERE id = $1::uuid`, caseTypeID,
	).Scan(
		&ct.ID, &ct.Code, &ct.Version, &ct.Name, &ct.Description, &configRaw,
		&ct.Status, &ct.CreatedAt, &ct.UpdatedAt, &ct.DeprecatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch case_type %s: %w", caseTypeID, err)
	}
	if err := json.Unmarshal(configRaw, &ct.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal case_type config: %w", err)
	}
	return &ct, nil
}

// CountTasksByActivityAndStatus returns task counts grouped by status
// for tasks in a given case, stage, and activity.
func (r *Repository) CountTasksByActivityAndStatus(
	ctx context.Context, tx DBExecutor,
	caseID, stageCode, activityCode string,
) (total int, completed int, err error) {
	if tx == nil {
		tx = r.Pool
	}
	rows, err := tx.Query(ctx, `
		SELECT status, COUNT(*)
		FROM tasks
		WHERE case_id = $1::uuid
		  AND stage_code = $2
		  AND activity_code = $3
		GROUP BY status`, caseID, stageCode, activityCode,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count tasks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, fmt.Errorf("failed to scan task count: %w", err)
		}
		total += count
		if status == string(model.TaskStatusDone) || status == string(model.TaskStatusSkipped) || status == string(model.TaskStatusCancelled) {
			completed += count
		}
	}
	return total, completed, rows.Err()
}

// CompleteCase marks a case as COMPLETED.
func (r *Repository) CompleteCase(ctx context.Context, tx DBExecutor, caseID string) error {
	if tx == nil {
		tx = r.Pool
	}
	tag, err := tx.Exec(ctx, `
		UPDATE cases
		SET status       = 'COMPLETED',
		    completed_at = now(),
		    row_version  = row_version + 1,
		    updated_at   = now()
		WHERE id = $1::uuid
		  AND status != 'COMPLETED'`, caseID,
	)
	if err != nil {
		return fmt.Errorf("failed to complete case %s: %w", caseID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("case %s not found or already completed", caseID)
	}
	return nil
}

// CloneCase duplicates a case and its tasks into a new case.
// Returns the new Case ID.
func (r *Repository) CloneCase(ctx context.Context, tx DBExecutor, sourceCaseID string) (string, error) {
	if tx == nil {
		tx = r.Pool
	}

	// 1. Copy Case
	var newCaseID string
	err := tx.QueryRow(ctx, `
		INSERT INTO cases (
			case_type_id, case_type_version, parent_case_id, source_case_id,
			current_stage_code, current_stage_ordinal, status, metadata,
			applicant_data
		)
		SELECT
			case_type_id, case_type_version, parent_case_id, $1,
			current_stage_code, current_stage_ordinal, 'CLONED', metadata,
			COALESCE(applicant_data, '{}'::jsonb)
		FROM cases
		WHERE id = $1::uuid
		RETURNING id`, sourceCaseID,
	).Scan(&newCaseID)
	if err != nil {
		return "", fmt.Errorf("failed to clone case %s: %w", sourceCaseID, err)
	}

	// 2. Copy Tasks
	// We generate new idempotency keys to avoid constraints.
	// We reset status to PENDING and clear execution details.
	_, err = tx.Exec(ctx, `
		INSERT INTO tasks (
			case_id, task_definition_code, activity_code, stage_code,
			status, priority, assigned_service,
			due_at, max_retries,
			input_payload, output_payload, metadata,
			idempotency_key
		)
		SELECT
			$1::uuid, task_definition_code, activity_code, stage_code,
			'PENDING', priority, NULL,
			due_at, max_retries,
			input_payload, '{}'::jsonb, metadata,
			-- Generate a new idempotency key: "cloned-" + newCaseID + "-" + old_key (truncated if needed or just random)
			'cloned-' || $1 || '-' || idempotency_key
		FROM tasks
		WHERE case_id = $2::uuid`,
		newCaseID, sourceCaseID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to clone tasks for case %s: %w", sourceCaseID, err)
	}

	return newCaseID, nil
}

// UpdateCaseStatus updates the case status and sets lifecycle-specific columns.
func (r *Repository) UpdateCaseStatus(
	ctx context.Context,
	tx DBExecutor,
	caseID string,
	status string,
	reason *string,
	metadata json.RawMessage,
) error {
	// Simplify: Delegate to specialized method or just do explicit updates if simple.
	// But to avoid unused vars, we use them.

	updates := make(map[string]interface{})
	if metadata != nil {
		updates["metadata"] = metadata
	}

	if status == model.CaseStatusSuspended {
		updates["suspend_reason"] = reason
		// Clear resume_at? Or set it?
		// User requirement 2: "optional resume_at timestamp".
		// This signature doesn't pass resumeAt.
		// So we can't set it here unless we change signature or rely on caller to use specific method.
		// I will use UpdateCaseLifecycle for everything advanced. This function handles basic status changes + reason.
	} else if status == model.CaseStatusCancelled {
		updates["withdrawal_reason"] = reason
	}

	return r.UpdateCaseLifecycle(ctx, tx, caseID, status, updates)
}

// allowedCaseColumns is the allowlist of column names that can be set via
// UpdateCaseLifecycle. This prevents SQL injection through map keys.
var allowedCaseColumns = map[string]bool{
	"suspend_reason":     true,
	"resume_at":          true,
	"withdrawal_reason":  true,
	"emergency_reason":   true,
	"emergency_closed_at": true,
	"supervisor_id":      true,
	"completed_at":       true,
	"metadata":           true,
	"assigned_to":        true,
	"current_stage_code": true,
	"current_stage_ordinal": true,
}

// UpdateCaseLifecycle updates status and specific lifecycle columns.
// Column names in the updates map are validated against an allowlist.
func (r *Repository) UpdateCaseLifecycle(
	ctx context.Context,
	tx DBExecutor,
	caseID string,
	status string,
	updates map[string]interface{},
) error {
	if tx == nil {
		tx = r.Pool
	}

	query := "UPDATE cases SET status = $1, row_version = row_version + 1, updated_at = now()"
	args := []interface{}{status}
	argIdx := 2

	for col, val := range updates {
		if !allowedCaseColumns[col] {
			return fmt.Errorf("UpdateCaseLifecycle: disallowed column %q", col)
		}
		query += fmt.Sprintf(", %s = $%d", col, argIdx)
		args = append(args, val)
		argIdx++
	}

	query += fmt.Sprintf(" WHERE id = $%d::uuid", argIdx)
	args = append(args, caseID)

	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update case lifecycle %s: %w", caseID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("case %s not found", caseID)
	}
	return nil
}

// ArchiveCase moves a case and its tasks to the archive tables and deletes originals.
func (r *Repository) ArchiveCase(ctx context.Context, tx DBExecutor, caseID string) error {
	// This MUST be a transaction.
	// If tx is provided, use it. If not, error? Or start one?
	// The repo methods usually accept tx or use pool. But for multi-step, we really want an external tx.
	// I'll assume the caller provides TX or I force one if nil?
	// Protocol says "All transactional functions: func Name(ctx context.Context, tx *sqlx.Tx, ...) error".
	// Here DBExecutor is the interface.

	if tx == nil {
		// Must start a transaction if not provided
		return r.WithTransaction(ctx, func(tx pgx.Tx) error {
			return r.ArchiveCase(ctx, tx, caseID)
		})
	}

	// 1. Copy Case to Archive (explicit columns to avoid breakage on schema changes)
	_, err := tx.Exec(ctx, `
		INSERT INTO cases_archive (
			id, reference_number, case_type_id, case_type_version,
			parent_case_id, current_stage_code, current_stage_ordinal,
			status, metadata, assigned_to, row_version,
			created_at, updated_at, completed_at,
			source_case_id, suspend_reason, resume_at,
			withdrawal_reason, emergency_closed_at, emergency_reason, supervisor_id,
			archived_at
		)
		SELECT
			id, reference_number, case_type_id, case_type_version,
			parent_case_id, current_stage_code, current_stage_ordinal,
			status, metadata, assigned_to, row_version,
			created_at, updated_at, completed_at,
			source_case_id, suspend_reason, resume_at,
			withdrawal_reason, emergency_closed_at, emergency_reason, supervisor_id,
			now()
		FROM cases WHERE id = $1::uuid`, caseID)
	if err != nil {
		return fmt.Errorf("failed to archive case %s: %w", caseID, err)
	}

	// 2. Copy Tasks to Archive (explicit columns to avoid breakage on schema changes)
	_, err = tx.Exec(ctx, `
		INSERT INTO tasks_archive (
			id, case_id, task_definition_code, activity_code, stage_code,
			status, priority, assigned_service,
			assigned_at, started_at, completed_at, due_at,
			retry_count, max_retries,
			input_payload, output_payload, metadata, error_detail,
			idempotency_key, version, created_at, updated_at,
			archived_at
		)
		SELECT
			id, case_id, task_definition_code, activity_code, stage_code,
			status, priority, assigned_service,
			assigned_at, started_at, completed_at, due_at,
			retry_count, max_retries,
			input_payload, output_payload, metadata, error_detail,
			idempotency_key, version, created_at, updated_at,
			now()
		FROM tasks WHERE case_id = $1::uuid`, caseID)
	if err != nil {
		return fmt.Errorf("failed to archive tasks for case %s: %w", caseID, err)
	}

	// 3. Delete Tasks
	_, err = tx.Exec(ctx, `DELETE FROM tasks WHERE case_id = $1::uuid`, caseID)
	if err != nil {
		return fmt.Errorf("failed to delete tasks for case %s: %w", caseID, err)
	}

	// 4. Delete Case
	tag, err := tx.Exec(ctx, `DELETE FROM cases WHERE id = $1::uuid`, caseID)
	if err != nil {
		return fmt.Errorf("failed to delete case %s: %w", caseID, err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("case %s not found during archive delete", caseID)
	}

	return nil
}
