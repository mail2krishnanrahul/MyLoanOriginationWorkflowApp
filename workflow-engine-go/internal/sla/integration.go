package sla

import (
	"context"
	"fmt"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// InitializeCaseAtCreation computes and stores case_due_at from case-level SLA.
func InitializeCaseAtCreation(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, caseID string, config model.CaseTypeConfig) error {
	if tx == nil {
		return fmt.Errorf("InitializeCaseAtCreation: tx is nil")
	}

	effective, err := ResolveEffectiveSLADefinition(config, "", "", "")
	if err != nil {
		return fmt.Errorf("InitializeCaseAtCreation: %w", err)
	}
	if effective == nil {
		return nil
	}

	dueAt, durationMS, calendarID, err := ComputeSLADeadline(ctx, db, time.Now().UTC(), config.DefaultCalendarID, *effective)
	if err != nil {
		return fmt.Errorf("InitializeCaseAtCreation: %w", err)
	}

	return InitializeCaseSLA(ctx, tx, caseID, dueAt, durationMS, calendarID, effective.WarningThresholdPct, effective.CriticalThresholdPct, effective.BreachAction)
}

// InitializeTaskAtCreation computes and stores task_due_at from inherited task SLA.
func InitializeTaskAtCreation(
	ctx context.Context,
	db *sqlx.DB,
	tx *sqlx.Tx,
	taskID string,
	config model.CaseTypeConfig,
	stageCode string,
	activityCode string,
	taskCode string,
) error {
	if tx == nil {
		return fmt.Errorf("InitializeTaskAtCreation: tx is nil")
	}

	effective, err := ResolveEffectiveSLADefinition(config, stageCode, activityCode, taskCode)
	if err != nil {
		return fmt.Errorf("InitializeTaskAtCreation: %w", err)
	}
	if effective == nil {
		return nil
	}

	dueAt, durationMS, calendarID, err := ComputeSLADeadline(ctx, db, time.Now().UTC(), config.DefaultCalendarID, *effective)
	if err != nil {
		return fmt.Errorf("InitializeTaskAtCreation: %w", err)
	}

	return InitializeTaskSLA(ctx, tx, taskID, dueAt, durationMS, calendarID, effective.WarningThresholdPct, effective.CriticalThresholdPct, effective.BreachAction)
}

// HandleTaskStatusTransitionSLA pauses/resumes task SLA clock on key status changes.
func HandleTaskStatusTransitionSLA(
	ctx context.Context,
	db *sqlx.DB,
	tx *sqlx.Tx,
	taskID string,
	fromStatus model.TaskStatus,
	toStatus model.TaskStatus,
	actor Actor,
	publisher EventPublisher,
) error {
	switch {
	case toStatus == model.TaskStatusAwaitingExternal && fromStatus != model.TaskStatusAwaitingExternal:
		return PauseSLA(ctx, db, tx, PauseSLARequest{
			EntityType: model.SLAEntityTypeTask,
			EntityID:   taskID,
			Reason:     string(model.TaskStatusAwaitingExternal),
			Actor:      actor,
		}, publisher)

	case fromStatus == model.TaskStatusAwaitingExternal && toStatus == model.TaskStatusInProgress:
		return ResumeSLA(ctx, db, tx, ResumeSLARequest{
			EntityType: model.SLAEntityTypeTask,
			EntityID:   taskID,
			Reason:     string(model.TaskStatusInProgress),
			Actor:      actor,
		}, publisher)
	}

	return nil
}

// PauseCaseTasksSLA pauses all active task SLAs when a case is suspended.
func PauseCaseTasksSLA(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, caseID string, reason string, actor Actor, publisher EventPublisher) error {
	if tx == nil {
		return fmt.Errorf("PauseCaseTasksSLA: tx is nil")
	}

	var taskIDs []string
	if err := tx.SelectContext(ctx, &taskIDs, `
		SELECT id::text
		FROM tasks
		WHERE case_id = $1::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, caseID); err != nil {
		return fmt.Errorf("PauseCaseTasksSLA: query tasks: %w", err)
	}

	for _, taskID := range taskIDs {
		if err := PauseSLA(ctx, db, tx, PauseSLARequest{
			EntityType: model.SLAEntityTypeTask,
			EntityID:   taskID,
			Reason:     reason,
			Actor:      actor,
		}, publisher); err != nil {
			return fmt.Errorf("PauseCaseTasksSLA: task %s: %w", taskID, err)
		}
	}

	return nil
}

// ResumeCaseTasksSLA resumes all task SLAs paused during case suspension.
func ResumeCaseTasksSLA(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, caseID string, reason string, actor Actor, publisher EventPublisher) error {
	if tx == nil {
		return fmt.Errorf("ResumeCaseTasksSLA: tx is nil")
	}

	var taskIDs []string
	if err := tx.SelectContext(ctx, &taskIDs, `
		SELECT id::text
		FROM tasks
		WHERE case_id = $1::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, caseID); err != nil {
		return fmt.Errorf("ResumeCaseTasksSLA: query tasks: %w", err)
	}

	for _, taskID := range taskIDs {
		state, err := currentEntityState(ctx, tx, model.SLAEntityTypeTask, taskID, false)
		if err != nil {
			return fmt.Errorf("ResumeCaseTasksSLA: task %s state: %w", taskID, err)
		}
		if state != model.SLAStatePaused {
			continue
		}

		if err := ResumeSLA(ctx, db, tx, ResumeSLARequest{
			EntityType: model.SLAEntityTypeTask,
			EntityID:   taskID,
			Reason:     reason,
			Actor:      actor,
		}, publisher); err != nil {
			return fmt.Errorf("ResumeCaseTasksSLA: task %s: %w", taskID, err)
		}
	}

	return nil
}
