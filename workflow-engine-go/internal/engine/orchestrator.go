package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// CaseOrchestrator interface
// ---------------------------------------------------------------------------

// CaseOrchestrator is the choreography brain that reacts to domain events
// and decides what happens next in a case's lifecycle.
type CaseOrchestrator interface {
	HandleEvent(ctx context.Context, event model.Event) error
}

// ---------------------------------------------------------------------------
// CaseOrchestratorService — concrete implementation
// ---------------------------------------------------------------------------

// CaseOrchestratorService implements the CaseOrchestrator interface.
type CaseOrchestratorService struct {
	Repo *repository.Repository
}

// NewCaseOrchestratorService creates a new orchestrator.
func NewCaseOrchestratorService(repo *repository.Repository) *CaseOrchestratorService {
	return &CaseOrchestratorService{Repo: repo}
}

// HandleEvent is the central event dispatcher. It inspects the event type
// and delegates to the appropriate handler.
func (o *CaseOrchestratorService) HandleEvent(ctx context.Context, event model.Event) error {
	slog.Info("handling event",
		"event_id", event.ID,
		"event_type", event.EventType,
		"case_id", event.CaseID,
		"task_id", event.TaskID)

	switch event.EventType {
	case model.EventTaskCompleted:
		return o.onTaskCompleted(ctx, event)
	case model.EventTaskFailed:
		return o.onTaskFailed(ctx, event)
	case model.EventActivityCompleted:
		return o.onActivityCompleted(ctx, event)
	case model.EventCaseStageChanged:
		return o.onCaseStageChanged(ctx, event)
	default:
		slog.Warn("unhandled event type", "event_type", event.EventType)
		return nil
	}
}

// ---------------------------------------------------------------------------
// Event handlers
// ---------------------------------------------------------------------------

// onTaskCompleted checks if the task's activity is now complete.
// If yes, publishes ACTIVITY_COMPLETED.
func (o *CaseOrchestratorService) onTaskCompleted(ctx context.Context, event model.Event) error {
	caseID, stageCode, activityCode, err := extractTaskContext(event.Payload)
	if err != nil {
		return fmt.Errorf("onTaskCompleted: %w", err)
	}

	slog.Info("TASK_COMPLETED", "case_id", caseID, "stage", stageCode, "activity", activityCode)

	// Check if all tasks in the activity are done
	total, completed, err := o.Repo.CountTasksByActivityAndStatus(ctx, nil, caseID, stageCode, activityCode)
	if err != nil {
		return fmt.Errorf("failed to count tasks: %w", err)
	}

	slog.Info("activity progress", "activity", activityCode, "completed", completed, "total", total)

	if completed >= total && total > 0 {
		// Activity is complete — publish event
		payload, _ := json.Marshal(map[string]interface{}{
			"case_id":       caseID,
			"stage_code":    stageCode,
			"activity_code": activityCode,
		})

		tx, err := o.Repo.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		if err := o.Repo.PublishEvent(ctx, tx, model.Event{
			CaseID:        &caseID,
			EventType:     model.EventActivityCompleted,
			Payload:       payload,
			TargetService: "case-orchestrator",
		}); err != nil {
			return fmt.Errorf("failed to publish ACTIVITY_COMPLETED: %w", err)
		}

		// Audit: activity completed (non-critical — log on failure)
		if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
			CaseID:      caseID,
			Action:      model.AuditActivityCompleted,
			EntityType:  model.AuditEntityActivity,
			ActorID:     "case-orchestrator",
			ActorType:   model.AuditActorSystem,
			ChangeDelta: payload,
		}); auditErr != nil {
			slog.Warn("audit insert failed", "error", auditErr, "action", model.AuditActivityCompleted)
		}

		slog.Info("published ACTIVITY_COMPLETED", "case_id", caseID, "activity", activityCode)
		return tx.Commit(ctx)
	}

	return nil
}

// onTaskFailed checks if retries are exhausted and escalates.
func (o *CaseOrchestratorService) onTaskFailed(ctx context.Context, event model.Event) error {
	taskID := ""
	if event.TaskID != nil {
		taskID = *event.TaskID
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("onTaskFailed: failed to parse payload: %w", err)
	}

	retriesExhausted := false
	if v, ok := payload["retries_exhausted"]; ok {
		if b, ok := v.(bool); ok {
			retriesExhausted = b
		}
	}

	if retriesExhausted {
		slog.Warn("TASK_FAILED retries exhausted, escalating", "task_id", taskID)

		escalationPayload, _ := json.Marshal(map[string]interface{}{
			"case_id":  event.CaseID,
			"task_id":  taskID,
			"reason":   "retries_exhausted",
			"original": payload,
		})

		tx, err := o.Repo.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin tx: %w", err)
		}
		defer tx.Rollback(ctx)

		if err := o.Repo.PublishEvent(ctx, tx, model.Event{
			CaseID:        event.CaseID,
			TaskID:        event.TaskID,
			EventType:     "TASK_ESCALATED",
			Payload:       escalationPayload,
			TargetService: "escalation-service",
		}); err != nil {
			return fmt.Errorf("failed to publish TASK_ESCALATED: %w", err)
		}

		// Audit: task escalated (non-critical — log on failure)
		caseIDStr := ""
		if event.CaseID != nil {
			caseIDStr = *event.CaseID
		}
		if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
			CaseID:      caseIDStr,
			Action:      model.AuditTaskEscalated,
			EntityType:  model.AuditEntityTask,
			EntityID:    &taskID,
			ActorID:     "case-orchestrator",
			ActorType:   model.AuditActorSystem,
			ChangeDelta: escalationPayload,
		}); auditErr != nil {
			slog.Warn("audit insert failed", "error", auditErr, "action", model.AuditTaskEscalated)
		}

		return tx.Commit(ctx)
	}

	slog.Debug("TASK_FAILED will retry", "task_id", taskID)
	return nil
}

// onActivityCompleted checks if all required activities in the stage are done.
// If yes, advances the case to the next stage.
func (o *CaseOrchestratorService) onActivityCompleted(ctx context.Context, event model.Event) error {
	caseID, stageCode, _, err := extractTaskContext(event.Payload)
	if err != nil {
		return fmt.Errorf("onActivityCompleted: %w", err)
	}

	slog.Info("ACTIVITY_COMPLETED", "case_id", caseID, "stage", stageCode)

	// Load case and case_type config
	caseInst, err := o.Repo.GetCaseInstance(ctx, nil, caseID)
	if err != nil {
		return fmt.Errorf("failed to load case: %w", err)
	}

	caseType, err := o.Repo.GetCaseType(ctx, nil, caseInst.CaseTypeID)
	if err != nil {
		return fmt.Errorf("failed to load case_type: %w", err)
	}

	// Find the current stage in config
	currentStage := findStage(caseType.Config, stageCode)
	if currentStage == nil {
		return fmt.Errorf("stage %s not found in case_type config", stageCode)
	}

	// Check if ALL activities in this stage are complete
	allComplete := true
	for _, activity := range currentStage.Activities {
		total, completed, err := o.Repo.CountTasksByActivityAndStatus(ctx, nil, caseID, stageCode, activity.Code)
		if err != nil {
			return fmt.Errorf("failed to check activity %s: %w", activity.Code, err)
		}
		if completed < total || total == 0 {
			allComplete = false
			break
		}
	}

	if !allComplete {
		slog.Debug("stage not yet complete", "stage", stageCode)
		return nil
	}

	// Find the next stage
	nextStage := findNextStage(caseType.Config, caseInst.CurrentStageOrdinal)
	if nextStage == nil {
		// No more stages — evaluate case completion
		slog.Info("final stage completed, evaluating case", "stage", stageCode, "case_id", caseID)
		return o.evaluateAndCompleteCase(ctx, caseID)
	}

	// Advance to next stage
	slog.Info("advancing case stage", "case_id", caseID, "from", stageCode, "to", nextStage.Code)

	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := o.Repo.RecordStageTransition(ctx, tx, model.TransitionInput{
		CaseID:         caseID,
		ToStageCode:    nextStage.Code,
		ToStageOrdinal: nextStage.SequenceOrder,
		TriggeredBy:    "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("failed to record stage transition: %w", err)
	}

	// Publish CASE_STAGE_CHANGED
	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":        caseID,
		"from_stage":     stageCode,
		"to_stage":       nextStage.Code,
		"to_stage_order": nextStage.SequenceOrder,
	})

	if err := o.Repo.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		EventType:     model.EventCaseStageChanged,
		Payload:       payload,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("failed to publish CASE_STAGE_CHANGED: %w", err)
	}

	// Audit: stage changed (non-critical — log on failure)
	if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseID,
		Action:      model.AuditStageChanged,
		EntityType:  model.AuditEntityStage,
		ActorID:     "case-orchestrator",
		ActorType:   model.AuditActorSystem,
		ChangeDelta: payload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "action", model.AuditStageChanged)
	}

	return tx.Commit(ctx)
}

// onCaseStageChanged loads the new stage's task definitions and creates
// all PENDING tasks for the new stage.
func (o *CaseOrchestratorService) onCaseStageChanged(ctx context.Context, event model.Event) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("onCaseStageChanged: failed to parse payload: %w", err)
	}

	caseID := strFromMap(payload, "case_id")
	toStage := strFromMap(payload, "to_stage")

	slog.Info("CASE_STAGE_CHANGED", "case_id", caseID, "new_stage", toStage)

	// Load case and case_type config
	caseInst, err := o.Repo.GetCaseInstance(ctx, nil, caseID)
	if err != nil {
		return fmt.Errorf("failed to load case: %w", err)
	}

	caseType, err := o.Repo.GetCaseType(ctx, nil, caseInst.CaseTypeID)
	if err != nil {
		return fmt.Errorf("failed to load case_type: %w", err)
	}

	stageDef := findStage(caseType.Config, toStage)
	if stageDef == nil {
		return fmt.Errorf("stage %s not found in case_type config", toStage)
	}

	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	count, err := CreateTasksForStage(ctx, tx, o.Repo, caseID, toStage, *stageDef)
	if err != nil {
		return fmt.Errorf("failed to create tasks for stage %s: %w", toStage, err)
	}

	slog.Info("tasks created for stage", "count", count, "stage", toStage, "case_id", caseID)

	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// CreateTasksForStage — bulk-inserts tasks for a new stage from config
// ---------------------------------------------------------------------------

// CreateTasksForStage reads task definitions from the case_type config and
// creates PENDING tasks for the new stage. Uses idempotency_key
// (caseID:stageCode:activityCode:taskCode) to avoid duplicates on retry.
func CreateTasksForStage(
	ctx context.Context,
	tx repository.DBExecutor,
	repo *repository.Repository,
	caseID, stageCode string,
	stageDef model.StageDefinitionV2,
) (int, error) {
	count := 0

	for _, activity := range stageDef.Activities {
		for _, taskDef := range activity.TaskDefs {
			idempotencyKey := fmt.Sprintf("%s:%s:%s:%s",
				caseID, stageCode, activity.Code, taskDef.Code)

			inputPayload, _ := json.Marshal(taskDef.Config)
			if inputPayload == nil {
				inputPayload = json.RawMessage("{}")
			}

			task := model.Task{
				CaseID:             caseID,
				TaskDefinitionCode: taskDef.Code,
				ActivityCode:       activity.Code,
				StageCode:          stageCode,
				Status:             model.TaskStatusPending,
				Priority:           model.TaskPriorityNormal,
				MaxRetries:         3,
				InputPayload:       inputPayload,
				IdempotencyKey:     idempotencyKey,
			}

			if _, err := repo.CreateTask(ctx, tx, task); err != nil {
				return count, fmt.Errorf("failed to create task %s: %w", taskDef.Code, err)
			}
			count++
		}
	}

	return count, nil
}

// ---------------------------------------------------------------------------
// EvaluateCaseCompletion — checks if a case should be marked COMPLETED
// ---------------------------------------------------------------------------

// EvaluateCaseCompletion returns true if all tasks across all stages
// for the given case are in a terminal status (COMPLETED, SKIPPED, CANCELLED).
func EvaluateCaseCompletion(ctx context.Context, tx repository.DBExecutor, caseID string) (bool, error) {
	var pending int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tasks
		WHERE case_id = $1::uuid
		  AND status NOT IN ('COMPLETED', 'SKIPPED', 'CANCELLED')`, caseID,
	).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate case completion: %w", err)
	}
	return pending == 0, nil
}

// evaluateAndCompleteCase wraps EvaluateCaseCompletion + CompleteCase in a tx.
func (o *CaseOrchestratorService) evaluateAndCompleteCase(ctx context.Context, caseID string) error {
	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	complete, err := EvaluateCaseCompletion(ctx, tx, caseID)
	if err != nil {
		return err
	}

	if !complete {
		slog.Debug("case has pending tasks", "case_id", caseID)
		return nil
	}

	if err := o.Repo.CompleteCase(ctx, tx, caseID); err != nil {
		return err
	}

	// Publish CASE_COMPLETED event
	payload, _ := json.Marshal(map[string]interface{}{
		"case_id": caseID,
	})

	if err := o.Repo.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		EventType:     model.EventCaseCompleted,
		Payload:       payload,
		TargetService: "notification-service",
	}); err != nil {
		return fmt.Errorf("failed to publish CASE_COMPLETED: %w", err)
	}

	// Audit: case completed (non-critical — log on failure)
	if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseID,
		Action:      model.AuditCaseCompleted,
		EntityType:  model.AuditEntityCase,
		ActorID:     "case-orchestrator",
		ActorType:   model.AuditActorSystem,
		ChangeDelta: payload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "action", model.AuditCaseCompleted)
	}

	slog.Info("case COMPLETED", "case_id", caseID)
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------------------
// Config traversal helpers
// ---------------------------------------------------------------------------

func findStage(config model.CaseTypeConfig, stageCode string) *model.StageDefinitionV2 {
	for i, s := range config.Stages {
		if s.Code == stageCode {
			return &config.Stages[i]
		}
	}
	return nil
}

func findNextStage(config model.CaseTypeConfig, currentOrdinal int) *model.StageDefinitionV2 {
	for i, s := range config.Stages {
		if s.SequenceOrder > currentOrdinal {
			return &config.Stages[i]
		}
	}
	return nil
}

func extractTaskContext(payload json.RawMessage) (caseID, stageCode, activityCode string, err error) {
	var p map[string]interface{}
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", "", "", fmt.Errorf("failed to parse payload: %w", err)
	}
	return strFromMap(p, "case_id"), strFromMap(p, "stage_code"), strFromMap(p, "activity_code"), nil
}

func strFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
