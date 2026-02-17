package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
			idempotency_key
		) VALUES (
			$1::uuid, $2, $3, $4,
			$5, $6, $7,
			$8, $9,
			$10, $11, $12,
			$13
		)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		task.CaseID, task.TaskDefinitionCode, task.ActivityCode, task.StageCode,
		string(task.Status), int(task.Priority), task.AssignedService,
		task.DueAt, task.MaxRetries,
		task.InputPayload, task.OutputPayload, task.Metadata,
		task.IdempotencyKey,
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
		       status, priority, assigned_service,
		       assigned_at, started_at, completed_at, due_at,
		       retry_count, max_retries,
		       input_payload, output_payload, metadata, error_detail,
		       idempotency_key, version, created_at, updated_at
		FROM tasks
		WHERE idempotency_key = $1`, key,
	).Scan(
		&t.ID, &t.CaseID, &t.TaskDefinitionCode, &t.ActivityCode, &t.StageCode,
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
	err := tx.QueryRow(ctx, `
		SELECT status, version
		FROM tasks
		WHERE id = $1::uuid
		FOR UPDATE`, taskID,
	).Scan(&currentStatus, &currentVersion)
	if err != nil {
		return fmt.Errorf("failed to lock task %s: %w", taskID, err)
	}

	// 2. Guard: terminal tasks cannot transition
	current := model.TaskStatus(currentStatus)
	if current.IsTerminal() {
		return fmt.Errorf("task %s is in terminal status %s", taskID, currentStatus)
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

	return nil
}
