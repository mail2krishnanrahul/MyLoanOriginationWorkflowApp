package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"workflow-engine/internal/repository"
	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
)

// ValidateLifecycleTransition enforces the state machine rules.
func (e *Engine) ValidateLifecycleTransition(
	ctx context.Context,
	currentStatus string,
	requestedStatus string,
	initiatedBy string,
) error {
	// Allowed transitions map
	// From -> [To]
	allowed := map[string]map[string]bool{
		model.CaseStatusOpen: {
			model.CaseStatusInProgress: true,
			model.CaseStatusCancelled:  true, // Withdrawal
			model.CaseStatusSuspended:  true,
		},
		model.CaseStatusInProgress: {
			model.CaseStatusCompleted:   true,
			model.CaseStatusCancelled:   true, // Withdrawal or Emergency Close
			model.CaseStatusSuspended:   true,
			model.CaseStatusAllocatable: true, // Release back to pool
		},
		model.CaseStatusSuspended: {
			model.CaseStatusInProgress: true, // Resume
			model.CaseStatusCancelled:  true, // Withdrawal during suspension
		},
		model.CaseStatusCloned: {
			model.CaseStatusOpen:       true, // Activated
			model.CaseStatusInProgress: true,
		},
		// Terminals cannot transition
		model.CaseStatusCompleted: {},
		model.CaseStatusCancelled: {},
		model.CaseStatusRejected:  {},
	}

	if validTargets, ok := allowed[currentStatus]; ok {
		if validTargets[requestedStatus] {
			return nil
		}
	}

	return fmt.Errorf("%w: from %s to %s (initiated by %s)",
		model.ErrInvalidStateTransition, currentStatus, requestedStatus, initiatedBy)
}

// CloneCase creates a duplicate of the given case.
func (e *Engine) CloneCase(ctx context.Context, sourceCaseID string) (string, error) {
	var newCaseID string
	err := e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		// 1. Validate Source
		source, err := e.Repo.GetCaseInstance(ctx, tx, sourceCaseID)
		if err != nil {
			return err
		}
		if source == nil {
			return fmt.Errorf("CloneCase: %w: %s", model.ErrCaseNotFound, sourceCaseID)
		}

		// 2. Perform Clone
		id, err := e.Repo.CloneCase(ctx, tx, sourceCaseID)
		if err != nil {
			return err
		}
		newCaseID = id

		// 3. Publish Event
		payload := map[string]interface{}{
			"source_case_id": sourceCaseID,
			"new_case_id":    newCaseID,
			"cloned_at":      time.Now().UTC(),
		}
		event := model.Event{
			CaseID:    &newCaseID,
			EventType: model.EventCaseCloned,
			Status:    model.EventStatusPending,
		}
		plBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("CloneCase: failed to marshal payload: %w", err)
		}
		event.Payload = plBytes

		return e.Repo.PublishEvent(ctx, tx, event)
	})

	return newCaseID, err
}

// SuspendCase pauses a case and atomically suspends all in-flight tasks.
func (e *Engine) SuspendCase(ctx context.Context, caseID string, reason string, resumeAt *time.Time) error {
	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		// 1. Get Current State (with row lock to prevent concurrent modifications)
		c, err := e.Repo.GetCaseInstanceWithLock(ctx, tx, caseID)
		if err != nil {
			return err
		}

		// 2. Validate Transition
		if err := e.ValidateLifecycleTransition(ctx, c.Status, model.CaseStatusSuspended, "system"); err != nil {
			return err
		}

		// 3. Update Case status
		updates := map[string]interface{}{
			"suspend_reason": reason,
			"resume_at":      resumeAt,
		}
		if err := e.Repo.UpdateCaseLifecycle(ctx, tx, caseID, model.CaseStatusSuspended, updates); err != nil {
			return err
		}

		// 4. [B-05 FIX] Atomically pause all in-flight tasks in the same transaction.
		// Reset non-terminal tasks to PENDING and clear assignment so workers release them.
		_, err = tx.Exec(ctx, `
			UPDATE tasks
			SET status = 'PENDING',
			    assigned_service = NULL,
			    assignee_id = NULL,
			    assigned_at = NULL,
			    updated_at = now(),
			    version = version + 1
			WHERE case_id = $1::uuid
			  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')`, caseID)
		if err != nil {
			return fmt.Errorf("SuspendCase: failed to pause in-flight tasks: %w", err)
		}

		if err := appendCaseSLAPauseLogs(ctx, e.Repo, tx, caseID, "CASE_SUSPENDED"); err != nil {
			return fmt.Errorf("SuspendCase: failed to write SLA pause logs: %w", err)
		}

		// 5. Publish Event
		payload := map[string]interface{}{
			"case_id":   caseID,
			"reason":    reason,
			"resume_at": resumeAt,
		}
		event := model.Event{
			CaseID:    &caseID,
			EventType: model.EventCaseSuspended,
			Status:    model.EventStatusPending,
		}
		plBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("SuspendCase: failed to marshal payload: %w", err)
		}
		event.Payload = plBytes

		return e.Repo.PublishEvent(ctx, tx, event)
	})
}

// ResumeCase resumes a suspended case and re-queues paused tasks for assignment.
func (e *Engine) ResumeCase(ctx context.Context, caseID string) error {
	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		c, err := e.Repo.GetCaseInstanceWithLock(ctx, tx, caseID)
		if err != nil {
			return err
		}

		targetStatus := model.CaseStatusInProgress

		if err := e.ValidateLifecycleTransition(ctx, c.Status, targetStatus, "system"); err != nil {
			return err
		}

		updates := map[string]interface{}{
			"resume_at":      nil, // Clear it
			"suspend_reason": nil,
		}
		if err := e.Repo.UpdateCaseLifecycle(ctx, tx, caseID, targetStatus, updates); err != nil {
			return err
		}

		// [H-07 FIX] Re-activate paused tasks so they become available for
		// assignment again. SuspendCase set them to PENDING and cleared their
		// assignments; we emit TASK_QUEUED events for tasks in workbaskets so
		// the auto-distributor picks them up.
		_, err = tx.Exec(ctx, `
			UPDATE tasks
			SET updated_at = now(),
			    version = version + 1
			WHERE case_id = $1::uuid
			  AND status = 'PENDING'`, caseID)
		if err != nil {
			return fmt.Errorf("ResumeCase: failed to re-activate tasks: %w", err)
		}

		// Publish TASK_QUEUED events for tasks that have a workbasket
		// so the auto-distributor can re-assign them.
		queuedRows, err := tx.Query(ctx, `
			SELECT id, workbasket_id
			FROM tasks
			WHERE case_id = $1::uuid
			  AND status = 'PENDING'
			  AND workbasket_id IS NOT NULL`, caseID)
		if err != nil {
			return fmt.Errorf("ResumeCase: query queued tasks: %w", err)
		}
		defer queuedRows.Close()

		for queuedRows.Next() {
			var taskID, workbasketID string
			if err := queuedRows.Scan(&taskID, &workbasketID); err != nil {
				continue
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"task_id":       taskID,
				"workbasket_id": workbasketID,
			})
			if err := e.Repo.PublishEvent(ctx, tx, model.Event{
				CaseID:    &caseID,
				TaskID:    &taskID,
				EventType: model.EventTaskQueued,
				Payload:   payload,
				Status:    model.EventStatusPending,
			}); err != nil {
				return fmt.Errorf("ResumeCase: publish TASK_QUEUED for task %s: %w", taskID, err)
			}
		}
		if err := queuedRows.Err(); err != nil {
			return fmt.Errorf("ResumeCase: iterate queued tasks: %w", err)
		}

		if err := appendCaseSLAResumeLogs(ctx, e.Repo, tx, caseID, "CASE_RESUMED"); err != nil {
			return fmt.Errorf("ResumeCase: failed to write SLA resume logs: %w", err)
		}

		event := model.Event{
			CaseID:    &caseID,
			EventType: model.EventCaseResumed,
			Status:    model.EventStatusPending,
		}
		event.Payload = json.RawMessage("{}")

		return e.Repo.PublishEvent(ctx, tx, event)
	})
}

func appendCaseSLAPauseLogs(ctx context.Context, repoIface Repository, tx pgx.Tx, caseID string, reason string) error {
	repoImpl, ok := repoIface.(*repository.Repository)
	if !ok {
		return nil
	}

	now := time.Now().UTC()
	var caseStart time.Time
	var caseCalendarID *string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(case_effective_start_time, created_at), case_sla_calendar_id::text
		FROM cases
		WHERE id = $1::uuid
	`, caseID).Scan(&caseStart, &caseCalendarID)
	if err != nil {
		return fmt.Errorf("appendCaseSLAPauseLogs: load case SLA state: %w", err)
	}

	caseElapsedMS := int64(0)
	if repoImpl.SQLX != nil && caseCalendarID != nil && *caseCalendarID != "" {
		elapsed, err := sla.BusinessHoursElapsed(ctx, repoImpl.SQLX, caseStart, now, *caseCalendarID)
		if err != nil {
			return fmt.Errorf("appendCaseSLAPauseLogs: compute case elapsed business time: %w", err)
		}
		caseElapsedMS = elapsed.Milliseconds()
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO sla_pause_log (
			entity_type, entity_id, paused_at, resumed_at, pause_reason, elapsed_before_pause_ms, action
		)
		VALUES ('CASE', $1::uuid, $2, NULL, $3, $4, 'PAUSE')
	`, caseID, now, reason, caseElapsedMS)
	if err != nil {
		return fmt.Errorf("appendCaseSLAPauseLogs: insert case pause log: %w", err)
	}

	casePayload, _ := json.Marshal(map[string]interface{}{
		"entity_type": "CASE",
		"entity_id":   caseID,
		"case_id":     caseID,
		"reason":      reason,
	})
	if err := repoImpl.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		EventType: model.EventSLAPaused,
		Payload:   casePayload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("appendCaseSLAPauseLogs: publish case SLA_PAUSED: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, COALESCE(effective_start_time, created_at), sla_calendar_id::text
		FROM tasks
		WHERE case_id = $1::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, caseID)
	if err != nil {
		return fmt.Errorf("appendCaseSLAPauseLogs: query task SLA state: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var taskID string
		var taskStart time.Time
		var taskCalendarID *string
		if err := rows.Scan(&taskID, &taskStart, &taskCalendarID); err != nil {
			return fmt.Errorf("appendCaseSLAPauseLogs: scan task row: %w", err)
		}

		taskElapsedMS := int64(0)
		if repoImpl.SQLX != nil && taskCalendarID != nil && *taskCalendarID != "" {
			elapsed, err := sla.BusinessHoursElapsed(ctx, repoImpl.SQLX, taskStart, now, *taskCalendarID)
			if err != nil {
				return fmt.Errorf("appendCaseSLAPauseLogs: compute task elapsed business time: %w", err)
			}
			taskElapsedMS = elapsed.Milliseconds()
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO sla_pause_log (
				entity_type, entity_id, paused_at, resumed_at, pause_reason, elapsed_before_pause_ms, action
			)
			VALUES ('TASK', $1::uuid, $2, NULL, $3, $4, 'PAUSE')
		`, taskID, now, reason, taskElapsedMS)
		if err != nil {
			return fmt.Errorf("appendCaseSLAPauseLogs: insert task pause log %s: %w", taskID, err)
		}

		taskPayload, _ := json.Marshal(map[string]interface{}{
			"entity_type": "TASK",
			"entity_id":   taskID,
			"case_id":     caseID,
			"task_id":     taskID,
			"reason":      reason,
		})
		if err := repoImpl.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			TaskID:    &taskID,
			EventType: model.EventSLAPaused,
			Payload:   taskPayload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("appendCaseSLAPauseLogs: publish task SLA_PAUSED %s: %w", taskID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("appendCaseSLAPauseLogs: iterate task rows: %w", err)
	}

	return nil
}

func appendCaseSLAResumeLogs(ctx context.Context, repoIface Repository, tx pgx.Tx, caseID string, reason string) error {
	repoImpl, ok := repoIface.(*repository.Repository)
	if !ok {
		return nil
	}

	now := time.Now().UTC()
	var casePausedAt time.Time
	var caseElapsedMS int64
	var caseDurationMS *int64
	var caseCalendarID *string
	err := tx.QueryRow(ctx, `
		SELECT
			COALESCE((
				SELECT paused_at
				FROM sla_pause_log
				WHERE entity_type = 'CASE'
				  AND entity_id = $1::uuid
				  AND action = 'PAUSE'
				ORDER BY created_at DESC
				LIMIT 1
			), now()),
			COALESCE((
				SELECT elapsed_before_pause_ms
				FROM sla_pause_log
				WHERE entity_type = 'CASE'
				  AND entity_id = $1::uuid
				  AND action = 'PAUSE'
				ORDER BY created_at DESC
				LIMIT 1
			), 0),
			case_sla_duration_ms,
			case_sla_calendar_id::text
		FROM cases
		WHERE id = $1::uuid
	`, caseID).Scan(&casePausedAt, &caseElapsedMS, &caseDurationMS, &caseCalendarID)
	if err != nil {
		return fmt.Errorf("appendCaseSLAResumeLogs: load case resume state: %w", err)
	}

	if caseDurationMS != nil && *caseDurationMS > 0 {
		remaining := time.Duration(*caseDurationMS-caseElapsedMS) * time.Millisecond
		if remaining < 0 {
			remaining = 0
		}
		var newCaseDueAt time.Time
		if repoImpl.SQLX != nil && caseCalendarID != nil && *caseCalendarID != "" {
			newCaseDueAt, err = sla.AddBusinessHours(ctx, repoImpl.SQLX, now, remaining, *caseCalendarID)
			if err != nil {
				return fmt.Errorf("appendCaseSLAResumeLogs: recompute case due_at: %w", err)
			}
		} else {
			newCaseDueAt = now.Add(remaining)
		}
		_, err = tx.Exec(ctx, `
			UPDATE cases
			SET case_effective_start_time = $1,
			    case_due_at = $2,
			    case_sla_warning_issued_at = NULL,
			    case_sla_critical_issued_at = NULL,
			    updated_at = now(),
			    row_version = row_version + 1
			WHERE id = $3::uuid
		`, now, newCaseDueAt, caseID)
		if err != nil {
			return fmt.Errorf("appendCaseSLAResumeLogs: update case due_at: %w", err)
		}
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO sla_pause_log (
			entity_type, entity_id, paused_at, resumed_at, pause_reason, elapsed_before_pause_ms, action
		)
		VALUES ('CASE', $1::uuid, $2, $3, $4, $5, 'RESUME')
	`, caseID, casePausedAt, now, reason, caseElapsedMS)
	if err != nil {
		return fmt.Errorf("appendCaseSLAResumeLogs: insert case resume log: %w", err)
	}

	casePayload, _ := json.Marshal(map[string]interface{}{
		"entity_type": "CASE",
		"entity_id":   caseID,
		"case_id":     caseID,
		"reason":      reason,
	})
	if err := repoImpl.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		EventType: model.EventSLAResumed,
		Payload:   casePayload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("appendCaseSLAResumeLogs: publish case SLA_RESUMED: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id::text, sla_duration_ms, sla_calendar_id::text
		FROM tasks
		WHERE case_id = $1::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, caseID)
	if err != nil {
		return fmt.Errorf("appendCaseSLAResumeLogs: query task resume state: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var taskID string
		var taskDurationMS *int64
		var taskCalendarID *string
		if err := rows.Scan(&taskID, &taskDurationMS, &taskCalendarID); err != nil {
			return fmt.Errorf("appendCaseSLAResumeLogs: scan task row: %w", err)
		}

		var pausedAt time.Time
		var elapsedMS int64
		err := tx.QueryRow(ctx, `
			SELECT paused_at, elapsed_before_pause_ms
			FROM sla_pause_log
			WHERE entity_type = 'TASK'
			  AND entity_id = $1::uuid
			  AND action = 'PAUSE'
			ORDER BY created_at DESC
			LIMIT 1
		`, taskID).Scan(&pausedAt, &elapsedMS)
		if err != nil {
			pausedAt = now
			elapsedMS = 0
		}

		if taskDurationMS != nil && *taskDurationMS > 0 {
			remaining := time.Duration(*taskDurationMS-elapsedMS) * time.Millisecond
			if remaining < 0 {
				remaining = 0
			}

			var newTaskDueAt time.Time
			if repoImpl.SQLX != nil && taskCalendarID != nil && *taskCalendarID != "" {
				newTaskDueAt, err = sla.AddBusinessHours(ctx, repoImpl.SQLX, now, remaining, *taskCalendarID)
				if err != nil {
					return fmt.Errorf("appendCaseSLAResumeLogs: recompute task due_at %s: %w", taskID, err)
				}
			} else {
				newTaskDueAt = now.Add(remaining)
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
				WHERE id = $3::uuid
			`, now, newTaskDueAt, taskID)
			if err != nil {
				return fmt.Errorf("appendCaseSLAResumeLogs: update task due_at %s: %w", taskID, err)
			}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO sla_pause_log (
				entity_type, entity_id, paused_at, resumed_at, pause_reason, elapsed_before_pause_ms, action
			)
			VALUES ('TASK', $1::uuid, $2, $3, $4, $5, 'RESUME')
		`, taskID, pausedAt, now, reason, elapsedMS)
		if err != nil {
			return fmt.Errorf("appendCaseSLAResumeLogs: insert task resume log %s: %w", taskID, err)
		}

		taskPayload, _ := json.Marshal(map[string]interface{}{
			"entity_type": "TASK",
			"entity_id":   taskID,
			"case_id":     caseID,
			"task_id":     taskID,
			"reason":      reason,
		})
		if err := repoImpl.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			TaskID:    &taskID,
			EventType: model.EventSLAResumed,
			Payload:   taskPayload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("appendCaseSLAResumeLogs: publish task SLA_RESUMED %s: %w", taskID, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("appendCaseSLAResumeLogs: iterate task rows: %w", err)
	}

	return nil
}

// ReleaseCase releases an assigned case back to the allocation pool.
// The case status reverts to ALLOCATABLE and assigned_user_id is cleared,
// making it available for GetNext to redistribute.
func (e *Engine) ReleaseCase(ctx context.Context, caseID string, userID string) error {
	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		c, err := e.Repo.GetCaseInstanceWithLock(ctx, tx, caseID)
		if err != nil {
			return err
		}

		if err := e.ValidateLifecycleTransition(ctx, c.Status, model.CaseStatusAllocatable, userID); err != nil {
			return err
		}

		// Clear assignment and revert to ALLOCATABLE so GetNext can re-distribute.
		_, err = tx.Exec(ctx, `
			UPDATE cases
			SET status           = 'ALLOCATABLE',
			    assigned_user_id = NULL,
			    row_version      = row_version + 1,
			    updated_at       = now()
			WHERE id = $1::uuid`, caseID)
		if err != nil {
			return fmt.Errorf("ReleaseCase: failed to update case: %w", err)
		}

		// Reset in-flight tasks to PENDING and clear their assignments
		// so they can be re-assigned when the case is next claimed.
		_, err = tx.Exec(ctx, `
			UPDATE tasks
			SET status           = 'PENDING',
			    assigned_service = NULL,
			    assignee_id      = NULL,
			    assigned_at      = NULL,
			    updated_at       = now(),
			    version          = version + 1
			WHERE case_id = $1::uuid
			  AND status IN ('ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')`, caseID)
		if err != nil {
			return fmt.Errorf("ReleaseCase: failed to reset tasks: %w", err)
		}

		payload := map[string]interface{}{
			"case_id":     caseID,
			"released_by": userID,
			"released_at": time.Now().UTC(),
		}
		plBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("ReleaseCase: failed to marshal payload: %w", err)
		}

		return e.Repo.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			EventType: model.EventCaseReleased,
			Status:    model.EventStatusPending,
			Payload:   plBytes,
		})
	})
}

// WithdrawCase cancels a case by applicant request and cancels all open tasks.
func (e *Engine) WithdrawCase(ctx context.Context, caseID string, reason string) error {
	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		c, err := e.Repo.GetCaseInstanceWithLock(ctx, tx, caseID)
		if err != nil {
			return err
		}

		if err := e.ValidateLifecycleTransition(ctx, c.Status, model.CaseStatusCancelled, "applicant"); err != nil {
			return err
		}

		updates := map[string]interface{}{
			"withdrawal_reason": reason,
			"completed_at":      time.Now().UTC(),
		}
		if err := e.Repo.UpdateCaseLifecycle(ctx, tx, caseID, model.CaseStatusCancelled, updates); err != nil {
			return err
		}

		// [B-06 FIX] Cancel all non-terminal tasks so workers stop processing them.
		_, err = tx.Exec(ctx, `
			UPDATE tasks
			SET status = 'CANCELLED',
			    completed_at = now(),
			    updated_at = now(),
			    version = version + 1
			WHERE case_id = $1::uuid
			  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'FAILED')`, caseID)
		if err != nil {
			return fmt.Errorf("WithdrawCase: failed to cancel open tasks: %w", err)
		}

		event := model.Event{
			CaseID:    &caseID,
			EventType: model.EventCaseWithdrawn,
			Status:    model.EventStatusPending,
		}
		plBytes, err := json.Marshal(map[string]string{
			"case_id": caseID,
			"reason":  reason,
		})
		if err != nil {
			return fmt.Errorf("WithdrawCase: failed to marshal payload: %w", err)
		}
		event.Payload = plBytes

		return e.Repo.PublishEvent(ctx, tx, event)
	})
}

// EmergencyClose force-closes a case with 4-eyes (dual-supervisor) approval.
// The fourEyesToken must have been issued by a different supervisor for this
// specific case and must not be expired or already consumed.
func (e *Engine) EmergencyClose(ctx context.Context, caseID string, reason string, supervisorID string, fourEyesToken string) error {
	// [B-08 FIX] Validate 4-eyes token before allowing emergency close
	if fourEyesToken == "" {
		return model.ErrFourEyesTokenRequired
	}

	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		// 1. Validate the 4-eyes token
		var tokenCaseID, tokenIssuerID string
		err := tx.QueryRow(ctx, `
			SELECT case_id, issuer_id
			FROM four_eyes_tokens
			WHERE token = $1
			  AND expires_at > now()
			  AND consumed_at IS NULL
			FOR UPDATE`, fourEyesToken,
		).Scan(&tokenCaseID, &tokenIssuerID)
		if err != nil {
			return fmt.Errorf("EmergencyClose: %w: %v", model.ErrFourEyesTokenInvalid, err)
		}
		if tokenCaseID != caseID {
			return fmt.Errorf("EmergencyClose: %w: token not issued for case %s", model.ErrFourEyesTokenInvalid, caseID)
		}
		if tokenIssuerID == supervisorID {
			return fmt.Errorf("EmergencyClose: %w", model.ErrFourEyesSameSupervisor)
		}

		// 2. Consume the token
		_, err = tx.Exec(ctx, `
			UPDATE four_eyes_tokens
			SET consumed_at = now(), consumed_by = $1
			WHERE token = $2`, supervisorID, fourEyesToken)
		if err != nil {
			return fmt.Errorf("EmergencyClose: failed to consume four-eyes token: %w", err)
		}

		// 3. Get case and validate it's non-terminal (with row lock)
		c, err := e.Repo.GetCaseInstanceWithLock(ctx, tx, caseID)
		if err != nil {
			return err
		}
		if c.IsTerminal() {
			return fmt.Errorf("EmergencyClose: %w: case %s status %s", model.ErrCaseTerminal, caseID, c.Status)
		}

		// 4. Validate transition through state machine
		if err := e.ValidateLifecycleTransition(ctx, c.Status, model.CaseStatusCancelled, supervisorID); err != nil {
			return err
		}

		// 5. Update case
		updates := map[string]interface{}{
			"emergency_reason":    reason,
			"supervisor_id":       supervisorID,
			"emergency_closed_at": time.Now().UTC(),
			"completed_at":        time.Now().UTC(),
		}
		if err := e.Repo.UpdateCaseLifecycle(ctx, tx, caseID, model.CaseStatusCancelled, updates); err != nil {
			return err
		}

		// 6. Cancel all open tasks (same as withdrawal)
		_, err = tx.Exec(ctx, `
			UPDATE tasks
			SET status = 'CANCELLED',
			    completed_at = now(),
			    updated_at = now(),
			    version = version + 1
			WHERE case_id = $1::uuid
			  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'FAILED')`, caseID)
		if err != nil {
			return fmt.Errorf("EmergencyClose: failed to cancel open tasks: %w", err)
		}

		// 7. [B-04 FIX] Publish CASE_EMERGENCY_CLOSED (not CASE_WITHDRAWN)
		plPayload := map[string]string{
			"case_id":          caseID,
			"reason":           reason,
			"supervisor_id":    supervisorID,
			"approver_id":      tokenIssuerID,
			"four_eyes_token":  fourEyesToken,
		}
		plBytes, err := json.Marshal(plPayload)
		if err != nil {
			return fmt.Errorf("EmergencyClose: failed to marshal payload: %w", err)
		}

		event := model.Event{
			CaseID:    &caseID,
			EventType: model.EventCaseEmergencyClosed,
			Status:    model.EventStatusPending,
			Payload:   plBytes,
		}

		return e.Repo.PublishEvent(ctx, tx, event)
	})
}
