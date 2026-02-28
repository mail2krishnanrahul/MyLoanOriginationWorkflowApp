package docverification

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateAdHocTask creates a free-form (is_adhoc=true) task on a case.
// The task does not require a TaskDefinitionV2 entry in the CaseType config.
// If IsBlocking is true, the task will prevent stage advancement until completed.
func CreateAdHocTask(ctx context.Context, pool *pgxpool.Pool, input model.CreateAdHocTaskInput) (string, error) {
	if input.CaseID == "" || input.DisplayName == "" || input.CreatedBy == "" {
		return "", fmt.Errorf("CreateAdHocTask: caseID, displayName, createdBy are required")
	}
	if input.ActivityCode == "" {
		input.ActivityCode = "AD_HOC"
	}
	if input.Priority == 0 {
		input.Priority = model.TaskPriorityNormal
	}

	idempKey := fmt.Sprintf("adhoc-%s-%s-%d", input.CaseID, input.DisplayName, time.Now().UnixNano())

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("CreateAdHocTask: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Load the current stage of the case.
	var stageCode, tenantID string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(current_stage_code, ''), tenant_id::text
		FROM cases WHERE id = $1::uuid FOR SHARE
	`, input.CaseID).Scan(&stageCode, &tenantID)
	if err != nil {
		return "", fmt.Errorf("CreateAdHocTask: load case stage: %w", err)
	}

	var taskID string
	err = tx.QueryRow(ctx, `
		INSERT INTO tasks (
			tenant_id, case_id, task_definition_code, activity_code, stage_code,
			status, priority, assigned_user_id, assigned_team_id, due_at,
			is_adhoc, is_blocking, display_name, description, external_reference,
			idempotency_key, created_by, max_retries, input_payload, output_payload, metadata
		) VALUES (
			$1::uuid, $2::uuid, 'ADHOC_TASK', $3, $4,
			'PENDING', $5, $6::uuid, $7::uuid, $8,
			true, $9, $10, $11, $12,
			$13, $14::uuid, 0, '{}', '{}', '{}'
		)
		RETURNING id::text
	`,
		tenantID, input.CaseID, input.ActivityCode, stageCode,
		int(input.Priority), input.AssignedUserID, input.AssignedTeamID, input.DueDate,
		input.IsBlocking, input.DisplayName, input.Description, input.ExternalReference,
		idempKey, input.CreatedBy,
	).Scan(&taskID)
	if err != nil {
		return "", fmt.Errorf("CreateAdHocTask: insert: %w", err)
	}

	payload := map[string]interface{}{
		"case_id":      input.CaseID,
		"task_id":      taskID,
		"display_name": input.DisplayName,
		"is_blocking":  input.IsBlocking,
		"created_by":   input.CreatedBy,
		"timestamp":    time.Now().UTC(),
	}
	if err := publishEventInTx(ctx, tx, input.CaseID, model.EventAdhocTaskCreated, payload); err != nil {
		return "", fmt.Errorf("CreateAdHocTask: publish event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("CreateAdHocTask: commit: %w", err)
	}

	slog.Info("ad-hoc task created",
		"task_id", taskID,
		"case_id", input.CaseID,
		"display_name", input.DisplayName,
		"is_blocking", input.IsBlocking)
	return taskID, nil
}

// GetAdHocTasks returns all ad-hoc tasks for a case ordered by created_at.
func GetAdHocTasks(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) ([]model.Task, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, tenant_id::text, case_id::text,
		       task_definition_code, activity_code, stage_code,
		       status, priority,
		       assigned_user_id::text, assigned_team_id::text,
		       due_at, created_at, updated_at
		FROM tasks
		WHERE case_id = $1::uuid
		  AND tenant_id = $2::uuid
		  AND is_adhoc = true
		ORDER BY created_at ASC
	`, caseID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("GetAdHocTasks: query: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	for rows.Next() {
		var t model.Task
		if err := rows.Scan(
			&t.ID, &t.TenantID, &t.CaseID,
			&t.TaskDefinitionCode, &t.ActivityCode, &t.StageCode,
			&t.Status, &t.Priority,
			&t.AssignedUserID, &t.AssignedTeamID,
			&t.DueAt, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("GetAdHocTasks: scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetAdHocTasks: rows: %w", err)
	}
	return tasks, nil
}
