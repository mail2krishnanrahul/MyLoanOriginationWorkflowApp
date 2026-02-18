package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"workflow-engine/internal/approval"
	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
)

// CreateTask inserts a new task with idempotency-key conflict handling.
// If a task with the same idempotency_key already exists, the existing
// task is returned without modification (ON CONFLICT DO NOTHING).
func (r *Repository) CreateTask(ctx context.Context, tx DBExecutor, task model.Task) (model.Task, error) {
	if tx == nil {
		tx = r.Pool
	}

	// Set defaults
	if task.Status == "" {
		task.Status = model.TaskStatusPending
	}
	if task.Priority == 0 {
		task.Priority = model.TaskPriorityNormal
	}
	if task.InputPayload == nil {
		task.InputPayload = json.RawMessage("{}")
	}
	if task.OutputPayload == nil {
		task.OutputPayload = json.RawMessage("{}")
	}
	if task.Metadata == nil {
		task.Metadata = json.RawMessage("{}")
	}

	// INSERT ... ON CONFLICT (idempotency_key) DO NOTHING
	// If conflict, the INSERT succeeds with 0 rows affected.
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO tasks (
			case_id, task_definition_code, activity_code, stage_code,
			status, priority, assigned_service,
			due_at, max_retries,
			input_payload, output_payload, metadata,
			idempotency_key, requires_approval, approval_amount
		) VALUES (
			$1::uuid, $2, $3, $4,
			$5, $6, $7,
			$8, $9,
			$10, $11, $12,
			$13, $14, $15
		)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		task.CaseID, task.TaskDefinitionCode, task.ActivityCode, task.StageCode,
		string(task.Status), int(task.Priority), task.AssignedService,
		task.DueAt, task.MaxRetries,
		task.InputPayload, task.OutputPayload, task.Metadata,
		task.IdempotencyKey, task.RequiresApproval, task.ApprovalAmount,
	).Scan(&id)

	if err != nil {
		// ON CONFLICT DO NOTHING returns no rows → pgx returns ErrNoRows
		if errors.Is(err, pgx.ErrNoRows) {
			// Idempotent: fetch the existing task
			return r.getTaskByIdempotencyKey(ctx, tx, task.IdempotencyKey)
		}
		return model.Task{}, fmt.Errorf("failed to create task: %w", err)
	}

	task.ID = id
	return task, nil
}

// getTaskByIdempotencyKey fetches an existing task by its idempotency key.
func (r *Repository) getTaskByIdempotencyKey(ctx context.Context, tx DBExecutor, key string) (model.Task, error) {
	var t model.Task
	var status string
	var priority int
	err := tx.QueryRow(ctx, `
		SELECT id, case_id, task_definition_code, activity_code, stage_code,
		       requires_approval, approval_gate_id::text, approval_amount,
		       status, priority, assigned_service,
		       assigned_at, started_at, completed_at, due_at,
		       retry_count, max_retries,
		       input_payload, output_payload, metadata, error_detail,
		       idempotency_key, version, created_at, updated_at
		FROM tasks
		WHERE idempotency_key = $1`, key,
	).Scan(
		&t.ID, &t.CaseID, &t.TaskDefinitionCode, &t.ActivityCode, &t.StageCode,
		&t.RequiresApproval, &t.ApprovalGateID, &t.ApprovalAmount,
		&status, &priority, &t.AssignedService,
		&t.AssignedAt, &t.StartedAt, &t.CompletedAt, &t.DueAt,
		&t.RetryCount, &t.MaxRetries,
		&t.InputPayload, &t.OutputPayload, &t.Metadata, &t.ErrorDetail,
		&t.IdempotencyKey, &t.Version, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return model.Task{}, fmt.Errorf("failed to fetch existing task by idempotency_key %s: %w", key, err)
	}
	t.Status = model.TaskStatus(status)
	t.Priority = model.TaskPriority(priority)
	return t, nil
}

// UpdateTaskStatus atomically advances a task's status with optimistic locking.
// It also updates output_payload and error_detail when provided, and sets
// timestamp fields (started_at, completed_at) based on the new status.
func (r *Repository) UpdateTaskStatus(
	ctx context.Context,
	tx DBExecutor,
	taskID string,
	newStatus model.TaskStatus,
	outputPayload json.RawMessage,
	errorDetail json.RawMessage,
) error {
	if tx == nil {
		tx = r.Pool
	}

	// 1. Lock and read current state
	var currentStatus string
	var currentVersion int
	var caseID string
	var requiresApproval bool
	var approvalGateID *string
	var effectiveStart time.Time
	var calendarID *string
	var durationMS *int64
	err := tx.QueryRow(ctx, `
		SELECT status,
		       version,
		       case_id::text,
		       requires_approval,
		       approval_gate_id::text,
		       COALESCE(effective_start_time, created_at) AS effective_start_time,
		       sla_calendar_id::text AS sla_calendar_id,
		       sla_duration_ms
		FROM tasks
		WHERE id = $1::uuid
		FOR UPDATE`, taskID,
	).Scan(&currentStatus, &currentVersion, &caseID, &requiresApproval, &approvalGateID, &effectiveStart, &calendarID, &durationMS)
	if err != nil {
		return fmt.Errorf("failed to lock task %s: %w", taskID, err)
	}

	// 2. Guard: terminal tasks cannot transition
	current := model.TaskStatus(currentStatus)
	if current.IsTerminal() {
		return fmt.Errorf("task %s is in terminal status %s", taskID, currentStatus)
	}
	if newStatus == model.TaskStatusDone && requiresApproval {
		if approvalGateID == nil || strings.TrimSpace(*approvalGateID) == "" {
			return fmt.Errorf("task %s requires approval but has no approval_gate_id", taskID)
		}
		if err := r.ensureApprovalGateSatisfied(ctx, tx, *approvalGateID); err != nil {
			return fmt.Errorf("task %s completion blocked: %w", taskID, err)
		}
	}

	// 3. Compute timestamp updates based on new status
	var startedAt, completedAt *time.Time
	now := time.Now().UTC()
	switch newStatus {
	case model.TaskStatusInProgress:
		startedAt = &now
	case model.TaskStatusDone, model.TaskStatusFailed, model.TaskStatusCancelled, model.TaskStatusSkipped:
		completedAt = &now
	}

	// 4. Update with optimistic lock
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status         = $1,
		    output_payload = COALESCE($2, output_payload),
		    error_detail   = COALESCE($3, error_detail),
		    started_at     = COALESCE($4, started_at),
		    completed_at   = COALESCE($5, completed_at),
		    version        = version + 1
		WHERE id = $6::uuid
		  AND version = $7`,
		string(newStatus),
		outputPayload,
		errorDetail,
		startedAt,
		completedAt,
		taskID,
		currentVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to update task status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("optimistic lock failure on task %s (version %d)", taskID, currentVersion)
	}

	if newStatus == model.TaskStatusInProgress && requiresApproval {
		if approvalGateID == nil || strings.TrimSpace(*approvalGateID) == "" {
			return fmt.Errorf("task %s requires approval but has no approval gate", taskID)
		}
		if err := r.activateApprovalGate(ctx, tx, taskID, caseID, *approvalGateID); err != nil {
			return fmt.Errorf("failed to activate approval gate %s for task %s: %w", *approvalGateID, taskID, err)
		}
	}

	// SLA pause/resume hooks on status transitions.
	if current == model.TaskStatusAwaitingExternal && newStatus == model.TaskStatusInProgress {
		var pausedAt time.Time
		var elapsedBeforePauseMS int64
		err := tx.QueryRow(ctx, `
			SELECT paused_at, elapsed_before_pause_ms
			FROM sla_pause_log
			WHERE entity_type = 'TASK'
			  AND entity_id = $1::uuid
			  AND action = 'PAUSE'
			ORDER BY created_at DESC
			LIMIT 1`, taskID).Scan(&pausedAt, &elapsedBeforePauseMS)
		if err != nil {
			pausedAt = now
			elapsedBeforePauseMS = 0
		}

		if durationMS != nil && *durationMS > 0 {
			remaining := time.Duration(*durationMS-elapsedBeforePauseMS) * time.Millisecond
			if remaining < 0 {
				remaining = 0
			}

			var newDueAt time.Time
			if r.SQLX != nil && calendarID != nil && *calendarID != "" {
				newDueAt, err = sla.AddBusinessHours(ctx, r.SQLX, now, remaining, *calendarID)
				if err != nil {
					return fmt.Errorf("failed to recompute SLA due_at on resume for task %s: %w", taskID, err)
				}
			} else {
				newDueAt = now.Add(remaining)
			}

			_, err = tx.Exec(ctx, `
				UPDATE tasks
				SET effective_start_time = $1,
				    task_due_at = $2,
				    due_at = $2,
				    sla_warning_issued_at = NULL,
				    sla_critical_issued_at = NULL,
				    updated_at = now(),
				    version = version + 1
				WHERE id = $3::uuid`,
				now, newDueAt, taskID,
			)
			if err != nil {
				return fmt.Errorf("failed to update task SLA after resume: %w", err)
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO sla_pause_log (
				entity_type, entity_id, paused_at, resumed_at, pause_reason, elapsed_before_pause_ms, action
			)
			VALUES ('TASK', $1::uuid, $2, $3, 'IN_PROGRESS', 0, 'RESUME')`,
			taskID, pausedAt, now,
		)
		if err != nil {
			return fmt.Errorf("failed to append task SLA resume log: %w", err)
		}

		eventPayload, _ := json.Marshal(map[string]interface{}{
			"entity_type": "TASK",
			"entity_id":   taskID,
			"case_id":     caseID,
			"task_id":     taskID,
			"reason":      "IN_PROGRESS",
		})
		if err := r.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			TaskID:    &taskID,
			EventType: model.EventSLAResumed,
			Payload:   eventPayload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("failed to publish SLA_RESUMED for task %s: %w", taskID, err)
		}
	}
	if current != model.TaskStatusAwaitingExternal && newStatus == model.TaskStatusAwaitingExternal {
		elapsedBeforePauseMS := int64(0)
		if r.SQLX != nil && calendarID != nil && *calendarID != "" {
			elapsed, elapsedErr := sla.BusinessHoursElapsed(ctx, r.SQLX, effectiveStart, now, *calendarID)
			if elapsedErr != nil {
				return fmt.Errorf("failed to compute elapsed business time for task %s: %w", taskID, elapsedErr)
			}
			elapsedBeforePauseMS = elapsed.Milliseconds()
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO sla_pause_log (
				entity_type, entity_id, paused_at, resumed_at, pause_reason, elapsed_before_pause_ms, action
			)
			VALUES ('TASK', $1::uuid, $2, NULL, 'AWAITING_EXTERNAL', $3, 'PAUSE')`,
			taskID, now, elapsedBeforePauseMS,
		)
		if err != nil {
			return fmt.Errorf("failed to append task SLA pause log: %w", err)
		}

		eventPayload, _ := json.Marshal(map[string]interface{}{
			"entity_type": "TASK",
			"entity_id":   taskID,
			"case_id":     caseID,
			"task_id":     taskID,
			"reason":      "AWAITING_EXTERNAL",
		})
		if err := r.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			TaskID:    &taskID,
			EventType: model.EventSLAPaused,
			Payload:   eventPayload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("failed to publish SLA_PAUSED for task %s: %w", taskID, err)
		}
	}

	return nil
}

func (r *Repository) ensureApprovalGateSatisfied(ctx context.Context, tx DBExecutor, gateID string) error {
	var gateStatus string
	err := tx.QueryRow(ctx, `
		SELECT gate_status
		FROM approval_gates
		WHERE id = $1::uuid
		FOR UPDATE
	`, gateID).Scan(&gateStatus)
	if err != nil {
		return fmt.Errorf("ensureApprovalGateSatisfied: load gate: %w", err)
	}
	switch model.ApprovalGateStatus(gateStatus) {
	case model.ApprovalGateStatusSatisfied:
		return nil
	case model.ApprovalGateStatusFailed, model.ApprovalGateStatusRejected, model.ApprovalGateStatusRejectedReworkInitiated, model.ApprovalGateStatusExpired:
		return fmt.Errorf("approval gate %s is %s", gateID, gateStatus)
	default:
		return fmt.Errorf("approval gate %s is not satisfied (status=%s)", gateID, gateStatus)
	}
}

func (r *Repository) activateApprovalGate(ctx context.Context, tx DBExecutor, taskID string, caseID string, gateID string) error {
	type gateRow struct {
		GateStatus             string
		ApproverSelection      string
		Approvers              []byte
		AuthorityLimit         *float64
		ApprovalAmount         *float64
		ApprovalTimeoutHours   float64
		OnTimeoutAction        string
		RejectionBehavior      string
		ReworkTargetStageCode  *string
		FallbackSupervisorRole *string
		DynamicRuleExpression  *string
		ChainDefinition        []byte
	}
	var gate gateRow
	if err := tx.QueryRow(ctx, `
		SELECT
			gate_status,
			approver_selection,
			approvers,
			authority_limit,
			approval_amount,
			approval_timeout_hours,
			on_timeout_action,
			rejection_behavior,
			rework_target_stage_code,
			fallback_supervisor_role,
			dynamic_rule_expression,
			chain_definition
		FROM approval_gates
		WHERE id = $1::uuid
		FOR UPDATE
	`, gateID).Scan(
		&gate.GateStatus,
		&gate.ApproverSelection,
		&gate.Approvers,
		&gate.AuthorityLimit,
		&gate.ApprovalAmount,
		&gate.ApprovalTimeoutHours,
		&gate.OnTimeoutAction,
		&gate.RejectionBehavior,
		&gate.ReworkTargetStageCode,
		&gate.FallbackSupervisorRole,
		&gate.DynamicRuleExpression,
		&gate.ChainDefinition,
	); err != nil {
		return fmt.Errorf("activateApprovalGate: load gate: %w", err)
	}

	switch model.ApprovalGateStatus(gate.GateStatus) {
	case model.ApprovalGateStatusSatisfied:
		return nil
	case model.ApprovalGateStatusFailed, model.ApprovalGateStatusRejected, model.ApprovalGateStatusRejectedReworkInitiated, model.ApprovalGateStatusExpired:
		return fmt.Errorf("activateApprovalGate: gate %s is terminal with status %s", gateID, gate.GateStatus)
	}

	var pending int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM approval_requests
		WHERE approval_gate_id = $1::uuid
		  AND status = 'PENDING'
	`, gateID).Scan(&pending); err != nil {
		return fmt.Errorf("activateApprovalGate: count pending requests: %w", err)
	}
	if pending > 0 {
		_, err := tx.Exec(ctx, `
			UPDATE approval_gates
			SET gate_status = 'ACTIVE',
			    opened_at = COALESCE(opened_at, now()),
			    updated_at = now(),
			    version = version + 1
			WHERE id = $1::uuid
		`, gateID)
		if err != nil {
			return fmt.Errorf("activateApprovalGate: mark active: %w", err)
		}
		return nil
	}

	var caseMetadata []byte
	var assignedTo *string
	var calendarID *string
	err := tx.QueryRow(ctx, `
		SELECT metadata, assigned_to, case_sla_calendar_id::text
		FROM cases
		WHERE id = $1::uuid
	`, caseID).Scan(&caseMetadata, &assignedTo, &calendarID)
	if err != nil {
		return fmt.Errorf("activateApprovalGate: load case data: %w", err)
	}

	caseInst := model.CaseInstance{
		ID:         caseID,
		Metadata:   caseMetadata,
		AssignedTo: assignedTo,
	}
	gateModel := model.ApprovalGate{
		ID:                     gateID,
		TaskID:                 taskID,
		CaseID:                 caseID,
		ApproverSelection:      model.ApproverSelection(gate.ApproverSelection),
		Approvers:              gate.Approvers,
		AuthorityLimit:         gate.AuthorityLimit,
		ApprovalAmount:         gate.ApprovalAmount,
		ApprovalTimeoutHours:   gate.ApprovalTimeoutHours,
		OnTimeoutAction:        model.TimeoutAction(gate.OnTimeoutAction),
		RejectionBehavior:      model.RejectionBehavior(gate.RejectionBehavior),
		ReworkTargetStageCode:  gate.ReworkTargetStageCode,
		FallbackSupervisorRole: gate.FallbackSupervisorRole,
		DynamicRuleExpression:  gate.DynamicRuleExpression,
		ChainDefinition:        gate.ChainDefinition,
	}
	if gateModel.ApprovalTimeoutHours <= 0 {
		gateModel.ApprovalTimeoutHours = 24
	}

	if r.SQLX == nil {
		return fmt.Errorf("activateApprovalGate: sqlx is not configured")
	}
	approverIDs, err := approval.SelectApprovers(ctx, r.SQLX, gateModel, caseInst)
	if err != nil {
		if err == model.ErrNoEligibleApprover {
			if gate.FallbackSupervisorRole != nil && strings.TrimSpace(*gate.FallbackSupervisorRole) != "" {
				var fallbackUser string
				fallbackErr := tx.QueryRow(ctx, `
					SELECT id
					FROM users
					WHERE role_code = $1
					  AND status = 'ACTIVE'
					ORDER BY created_at ASC
					LIMIT 1
				`, strings.TrimSpace(*gate.FallbackSupervisorRole)).Scan(&fallbackUser)
				if fallbackErr == nil && fallbackUser != "" {
					approverIDs = []string{fallbackUser}
					err = nil
				}
			}
		}
		if err != nil {
			if err == model.ErrNoEligibleApprover {
				payload, _ := json.Marshal(map[string]interface{}{
					"gate_id":      gateID,
					"case_id":      caseID,
					"task_id":      taskID,
					"event_reason": "no_eligible_approver",
				})
				_ = r.PublishEvent(ctx, tx, model.Event{
					CaseID:    &caseID,
					TaskID:    &taskID,
					EventType: model.EventNoEligibleApprover,
					Payload:   payload,
					Status:    model.EventStatusPending,
				})
			}
			return fmt.Errorf("activateApprovalGate: select approvers: %w", err)
		}
	}
	if len(approverIDs) == 0 {
		return fmt.Errorf("activateApprovalGate: %w", model.ErrNoEligibleApprover)
	}

	timeoutDuration := time.Duration(gateModel.ApprovalTimeoutHours * float64(time.Hour))
	if timeoutDuration <= 0 {
		timeoutDuration = 24 * time.Hour
	}
	expiresAt := time.Now().UTC().Add(timeoutDuration)
	if calendarID != nil && strings.TrimSpace(*calendarID) != "" {
		if dueAt, calErr := sla.AddBusinessHours(ctx, r.SQLX, time.Now().UTC(), timeoutDuration, *calendarID); calErr == nil {
			expiresAt = dueAt
		}
	}

	var tier interface{}
	if len(gateModel.ChainDefinition) > 0 {
		var tiers []model.ApprovalChainTierDefinition
		if err := json.Unmarshal(gateModel.ChainDefinition, &tiers); err == nil && len(tiers) > 0 {
			tier = tiers[0].Tier
		}
	}

	for _, approverID := range approverIDs {
		var requestID string
		err := tx.QueryRow(ctx, `
			INSERT INTO approval_requests (
				approval_gate_id,
				approver_id,
				tier,
				status,
				evidence_refs,
				expires_at,
				delegation_chain
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				'PENDING',
				'[]'::jsonb,
				$4,
				'[]'::jsonb
			)
			RETURNING id::text
		`, gateID, approverID, tier, expiresAt).Scan(&requestID)
		if err != nil {
			// Ignore duplicate pending rows on retry/idempotent activation.
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return fmt.Errorf("activateApprovalGate: insert request for approver %s: %w", approverID, err)
			}
		}

		if strings.TrimSpace(requestID) != "" {
			_, _ = tx.Exec(ctx, `
				INSERT INTO approval_audit_log (
					approval_request_id,
					event_type,
					actor_id,
					evidence_refs,
					previous_state,
					new_state
				)
				VALUES ($1::uuid, 'REQUESTED', 'SYSTEM', '[]'::jsonb, 'PENDING', 'PENDING')
			`, requestID)
		}

		payload, _ := json.Marshal(map[string]interface{}{
			"gate_id":         gateID,
			"request_id":      requestID,
			"case_id":         caseID,
			"task_id":         taskID,
			"approver_id":     approverID,
			"request_status":  string(model.ApprovalRequestStatusPending),
			"event_reason":    "approval_requested",
			"approval_expires": expiresAt,
		})
		if err := r.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			TaskID:    &taskID,
			EventType: model.EventApprovalRequested,
			Payload:   payload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("activateApprovalGate: publish APPROVAL_REQUESTED: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE approval_gates
		SET gate_status = 'ACTIVE',
		    opened_at = COALESCE(opened_at, now()),
		    updated_at = now(),
		    version = version + 1
		WHERE id = $1::uuid
	`, gateID); err != nil {
		return fmt.Errorf("activateApprovalGate: update gate active: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tasks
		SET status = CASE
			WHEN status = 'IN_PROGRESS' THEN 'AWAITING_EXTERNAL'
			ELSE status
		END,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $1::uuid
	`, taskID); err != nil {
		return fmt.Errorf("activateApprovalGate: update task waiting status: %w", err)
	}

	return nil
}
