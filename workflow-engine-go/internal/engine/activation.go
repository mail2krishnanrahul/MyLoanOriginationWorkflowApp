package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// ActivateStage handles the transition logic when a case enters a new stage
func (e *Engine) ActivateStage(ctx context.Context, tx repository.DBExecutor, caseID string, stageDef *model.StageYAML) error {
	slog.Info("activating stage", "stage", stageDef.Name, "case_id", caseID)

	// 1. Execute Pre-Hooks
	for _, hook := range stageDef.PreHooks {
		if err := e.triggerHook(ctx, tx, caseID, hook); err != nil {
			return fmt.Errorf("failed to trigger pre-hook %s: %w", hook.Name, err)
		}
	}

	// 2. Create Task Instances
	for _, taskDef := range stageDef.Tasks {
		if err := e.createTaskInstance(ctx, tx, caseID, taskDef); err != nil {
			return fmt.Errorf("failed to create task %s: %w", taskDef.Name, err)
		}
	}

	return nil
}

func (e *Engine) triggerHook(ctx context.Context, tx repository.DBExecutor, caseID string, hook model.HookYAML) error {
	payload := map[string]interface{}{
		"case_id":   caseID,
		"hook_name": hook.Name,
		"config":    hook.Config,
	}
	return e.Repo.InsertOutboxEvent(ctx, tx, "HOOK_TRIGGERED", payload)
}

func (e *Engine) createTaskInstance(ctx context.Context, tx repository.DBExecutor, caseID string, taskDef model.TaskYAML) error {
	// Prepare Data Payload (UI Config + Metadata)
	payload := make(map[string]interface{})
	if taskDef.UIConfig != nil {
		payload["ui_config"] = taskDef.UIConfig
	}
	payload["task_name"] = taskDef.Name
	payload["task_type"] = taskDef.Type

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Determine Initial Status
	status := model.TaskStatusPending
	if taskDef.Type == "USER" {
		status = "AWAITING_USER"
	}

	// Insert Task Instance (Direct SQL for now as we don't have a specific Repo method for tasks yet)
	// Note: We need a TaskDefinition ID if we stick to the schema strictly.
	// The schema says `task_instances` references `task_definitions`.
	// For this exercise, we'll assume we either look it up or the schema allows null/dynamic.
	// Since we are parsing YAML dynamically, we might not have pre-seeded DB definitions.
	// We will Insert into `task_instances` directly, assuming we can relax the foreign key or we insert a definition first.
	// LIMITATION: The current schema enforces `task_definition_id`.
	// STRATEGY: We will insert a dummy/dynamic task definition OR (preferred for this DSL approach)
	// we assume the schema has been adjusted to allow purely dynamic tasks or we insert a definition on the fly.
	// Let's Insert a definition on the fly for correctness with the schema.

	var taskDefID int64
	// Check if exists or insert (simplified: always insert/get)
	queryDef := `
		INSERT INTO task_definitions (stage_definition_id, name, task_type, required_data_schema)
		VALUES (NULL, $1, $2, $3)
		RETURNING id`
	// Note: stage_definition_id is NULL here because we are driving from YAML, not DB-stored Stage Definitions.
	// If the DB constraint is strict, we'd need a DB stage def too.
	// For this task, we assume we can insert with NULL or we'd need to sync YAML to DB first.
	// Let's assume we can proceed with NULL for now or we just map it.

	err = tx.QueryRow(ctx, queryDef, taskDef.Name, taskDef.Type, payloadJSON).Scan(&taskDefID)
	if err != nil {
		// Fallback: If FK constraint fails (e.g. stage_definition_id cannot be null),
		// we might need to change approach.
		// But let's assume valid Schema for now or that we synced StageDefs.
		return fmt.Errorf("failed to ensure task definition: %w", err)
	}

	queryInstance := `
		INSERT INTO task_instances (case_id, task_definition_id, status, data_payload)
		VALUES ($1::uuid, $2, $3, $4::jsonb)
		RETURNING id`

	var taskInstanceID string
	err = tx.QueryRow(ctx, queryInstance, caseID, taskDefID, status, payloadJSON).Scan(&taskInstanceID)
	if err != nil {
		return fmt.Errorf("failed to create task instance: %w", err)
	}

	// If SYSTEM task, trigger execution immediately
	if taskDef.Type == "SYSTEM" {
		eventPayload := map[string]interface{}{
			"case_id":          caseID,
			"task_instance_id": taskInstanceID,
			"endpoint":         taskDef.IntegrationEndpoint,
		}
		if err := e.Repo.InsertOutboxEvent(ctx, tx, "TASK_SCHEDULED", eventPayload); err != nil {
			return fmt.Errorf("failed to schedule system task: %w", err)
		}
	}

	return nil
}
