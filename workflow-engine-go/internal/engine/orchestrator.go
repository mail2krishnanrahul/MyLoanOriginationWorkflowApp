package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/internal/document"
	"workflow-engine/internal/exception"
	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/repository"
	"workflow-engine/internal/sla"
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
	ctx = multitenancy.EnsureTenantContextForEvent(ctx, event)
	tenantID, _ := multitenancy.TenantFromContext(ctx)
	slog.Info("handling event",
		"event_id", event.ID,
		"event_type", event.EventType,
		"tenant_id", tenantID,
		"case_id", event.CaseID,
		"task_id", event.TaskID)

	switch event.EventType {
	case model.EventTaskCompleted:
		return o.onTaskCompleted(ctx, event)
	case model.EventTaskFailed:
		return o.onTaskFailed(ctx, event)
	case model.EventTaskRequeued:
		return o.onTaskRequeued(ctx, event)
	case model.EventActivityCompleted:
		return o.onActivityCompleted(ctx, event)
	case model.EventCaseStageChanged:
		return o.onCaseStageChanged(ctx, event)
	case model.EventCaseExceptionRaised, model.EventCaseExceptionPropagated:
		return o.onCaseExceptionEvent(ctx, event)
	case model.EventCompensationStarted, model.EventCompensationCompleted, model.EventCompensationFailed:
		return o.onCompensationEvent(ctx, event)
	case model.EventSLAWarning, model.EventSLACritical, model.EventSLABreached:
		return o.onSLAThresholdEvent(ctx, event)
	case model.EventSLAPaused, model.EventSLAResumed, model.EventSLAReset, model.EventSLAExtended:
		return o.onSLALifecycleEvent(ctx, event)
	case model.EventApprovalGateCreated,
		model.EventApprovalRequested,
		model.EventApprovalGranted,
		model.EventApprovalRejected,
		model.EventApprovalDelegated,
		model.EventApprovalExpired,
		model.EventApprovalGateSatisfied,
		model.EventApprovalGateFailed,
		model.EventNoEligibleApprover:
		return o.onApprovalEvent(ctx, event)
	case model.EventCaseSentToRework,
		model.EventCaseRejected,
		model.EventCaseMaxReworkExceeded:
		return o.onApprovalCaseEvent(ctx, event)
	case model.EventTenantConfigUpdated:
		return o.onTenantConfigUpdated(ctx, event)
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
	taskID := ""
	if event.TaskID != nil {
		taskID = *event.TaskID
	}
	if taskID == "" {
		var payload map[string]interface{}
		if parseErr := json.Unmarshal(event.Payload, &payload); parseErr == nil {
			if v, ok := payload["task_id"].(string); ok {
				taskID = v
			}
		}
	}

	if taskID != "" {
		if err := o.applyAggregationRulesForTask(ctx, caseID, taskID); err != nil {
			return fmt.Errorf("onTaskCompleted: apply aggregation rules: %w", err)
		}
		if err := o.syncCompensationForTask(ctx, taskID, model.TaskStatusDone); err != nil {
			return fmt.Errorf("onTaskCompleted: sync compensation status: %w", err)
		}
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

	if taskID != "" {
		if err := o.syncCompensationForTask(ctx, taskID, model.TaskStatusFailed); err != nil {
			return fmt.Errorf("onTaskFailed: sync compensation status: %w", err)
		}
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

func (o *CaseOrchestratorService) onTaskRequeued(ctx context.Context, event model.Event) error {
	_ = ctx
	if event.TaskID != nil {
		slog.Info("TASK_REQUEUED received", "task_id", *event.TaskID, "case_id", event.CaseID)
	}
	return nil
}

func (o *CaseOrchestratorService) onCaseExceptionEvent(ctx context.Context, event model.Event) error {
	_ = ctx
	caseID := ""
	if event.CaseID != nil {
		caseID = *event.CaseID
	}
	slog.Warn("case exception event received", "event_type", event.EventType, "case_id", caseID)
	return nil
}

func (o *CaseOrchestratorService) onCompensationEvent(ctx context.Context, event model.Event) error {
	_ = ctx
	taskID := ""
	if event.TaskID != nil {
		taskID = *event.TaskID
	}
	slog.Info("compensation event received", "event_type", event.EventType, "task_id", taskID, "case_id", event.CaseID)
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

	count, err := CreateTasksForStage(ctx, tx, o.Repo, caseID, toStage, *stageDef, caseType.Config)
	if err != nil {
		return fmt.Errorf("failed to create tasks for stage %s: %w", toStage, err)
	}

	slog.Info("tasks created for stage", "count", count, "stage", toStage, "case_id", caseID)

	return tx.Commit(ctx)
}

// onSLAThresholdEvent handles SLA_WARNING / SLA_CRITICAL / SLA_BREACHED.
func (o *CaseOrchestratorService) onSLAThresholdEvent(ctx context.Context, event model.Event) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("onSLAThresholdEvent: parse payload: %w", err)
	}

	caseID := strFromMap(payload, "case_id")
	entityType := strFromMap(payload, "entity_type")
	entityID := strFromMap(payload, "entity_id")

	slog.Info("SLA threshold event",
		"event_type", event.EventType,
		"case_id", caseID,
		"entity_type", entityType,
		"entity_id", entityID)
	if event.EventType == model.EventSLABreached {
		tenantID, _ := multitenancy.TenantFromContext(ctx)
		if tenantID == "" {
			tenantID = event.TenantID
		}
		caseTypeCode := strFromMap(payload, "case_type_code")
		if caseTypeCode == "" {
			caseTypeCode = "UNKNOWN"
		}
		multitenancy.IncSLABreached(tenantID, caseTypeCode)
	}

	// Threshold events are side-effect driven in the SLA service. The orchestrator
	// records auditable visibility and leaves domain state untouched here.
	if caseID == "" {
		return nil
	}

	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("onSLAThresholdEvent: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseID,
		Action:      model.AuditAction(string(event.EventType)),
		EntityType:  model.AuditEntityCase,
		EntityID:    &entityID,
		ActorID:     "sla-sweeper",
		ActorType:   model.AuditActorSystem,
		ChangeDelta: event.Payload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "event_type", event.EventType)
	}

	return tx.Commit(ctx)
}

// onSLALifecycleEvent handles SLA_PAUSED / SLA_RESUMED / SLA_RESET / SLA_EXTENDED.
func (o *CaseOrchestratorService) onSLALifecycleEvent(ctx context.Context, event model.Event) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("onSLALifecycleEvent: parse payload: %w", err)
	}

	caseID := strFromMap(payload, "case_id")
	entityType := strFromMap(payload, "entity_type")
	entityID := strFromMap(payload, "entity_id")

	slog.Info("SLA lifecycle event",
		"event_type", event.EventType,
		"case_id", caseID,
		"entity_type", entityType,
		"entity_id", entityID)

	if caseID == "" {
		return nil
	}

	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("onSLALifecycleEvent: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseID,
		Action:      model.AuditAction(string(event.EventType)),
		EntityType:  model.AuditEntityCase,
		EntityID:    &entityID,
		ActorID:     "sla-service",
		ActorType:   model.AuditActorSystem,
		ChangeDelta: event.Payload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "event_type", event.EventType)
	}

	return tx.Commit(ctx)
}

func (o *CaseOrchestratorService) onApprovalEvent(ctx context.Context, event model.Event) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("onApprovalEvent: parse payload: %w", err)
	}

	caseID := ""
	if event.CaseID != nil {
		caseID = *event.CaseID
	}
	entityID := strFromMap(payload, "gate_id")
	if entityID == "" {
		entityID = strFromMap(payload, "request_id")
	}

	slog.Info("approval event",
		"event_type", event.EventType,
		"case_id", caseID,
		"entity_id", entityID)

	if caseID == "" {
		return nil
	}

	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("onApprovalEvent: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseID,
		Action:      model.AuditAction(string(event.EventType)),
		EntityType:  model.AuditEntityCase,
		EntityID:    &entityID,
		ActorID:     "approval-service",
		ActorType:   model.AuditActorSystem,
		ChangeDelta: event.Payload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "event_type", event.EventType)
	}

	return tx.Commit(ctx)
}

func (o *CaseOrchestratorService) onTenantConfigUpdated(ctx context.Context, event model.Event) error {
	if err := multitenancy.HandleTenantConfigUpdatedEvent(ctx, event.Payload); err != nil {
		return fmt.Errorf("onTenantConfigUpdated: %w", err)
	}
	slog.Info("tenant feature cache invalidated from TENANT_CONFIG_UPDATED event")
	return nil
}

func (o *CaseOrchestratorService) onApprovalCaseEvent(ctx context.Context, event model.Event) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("onApprovalCaseEvent: parse payload: %w", err)
	}

	caseID := ""
	if event.CaseID != nil {
		caseID = *event.CaseID
	}
	if caseID == "" {
		caseID = strFromMap(payload, "case_id")
	}
	if caseID == "" {
		return nil
	}

	tx, err := o.Repo.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("onApprovalCaseEvent: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Ensure CASE_REJECTED signals actually put the case in terminal REJECTED state.
	if event.EventType == model.EventCaseRejected || event.EventType == model.EventCaseMaxReworkExceeded {
		_, err := tx.Exec(ctx, `
			UPDATE cases
			SET status = 'REJECTED',
			    completed_at = COALESCE(completed_at, now()),
			    updated_at = now(),
			    row_version = row_version + 1
			WHERE id = $1::uuid
		`, caseID)
		if err != nil {
			return fmt.Errorf("onApprovalCaseEvent: update case rejected: %w", err)
		}
	}

	entityID := strFromMap(payload, "gate_id")
	if entityID == "" {
		entityID = caseID
	}
	if auditErr := o.Repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseID,
		Action:      model.AuditAction(string(event.EventType)),
		EntityType:  model.AuditEntityCase,
		EntityID:    &entityID,
		ActorID:     "approval-service",
		ActorType:   model.AuditActorSystem,
		ChangeDelta: event.Payload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "event_type", event.EventType)
	}

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
	caseTypeConfig model.CaseTypeConfig,
) (int, error) {
	count := 0
	validator := document.DefaultSchemaValidator()
	caseMetadata, err := loadCaseMetadataForApproval(ctx, tx, caseID)
	if err != nil {
		return count, fmt.Errorf("failed to load case metadata for approvals: %w", err)
	}

	for _, activity := range stageDef.Activities {
		for _, taskDef := range activity.TaskDefs {
			idempotencyKey := fmt.Sprintf("%s:%s:%s:%s",
				caseID, stageCode, activity.Code, taskDef.Code)

			resolvedInputs := map[string]interface{}{}
			if len(taskDef.Config) > 0 {
				if err := json.Unmarshal(taskDef.Config, &resolvedInputs); err != nil {
					return count, fmt.Errorf("failed to parse default input config for task %s: %w", taskDef.Code, err)
				}
			}

			if len(taskDef.Inputs) > 0 {
				if repo.SQLX == nil {
					return count, fmt.Errorf("sqlx db is not configured for dependency resolution on task %s", taskDef.Code)
				}
				resolvedInputs, err = document.ResolveTaskInputs(ctx, repo.SQLX, caseID, taskDef)
				if err != nil {
					var dependencyErr *document.DependencyError
					if errors.As(err, &dependencyErr) {
						reason := dependencyErr.Error()
						blockPayload, _ := json.Marshal(map[string]interface{}{
							"case_id":               caseID,
							"stage_code":            stageCode,
							"activity_code":         activity.Code,
							"task_definition_code":  taskDef.Code,
							"reason":                reason,
						})
						if publishErr := repo.PublishEvent(ctx, tx, model.Event{
							CaseID:        &caseID,
							EventType:     model.EventTaskCreationBlocked,
							Payload:       blockPayload,
							Status:        model.EventStatusPending,
							TargetService: "case-orchestrator",
						}); publishErr != nil {
							return count, fmt.Errorf("failed to publish TASK_CREATION_BLOCKED for %s: %w", taskDef.Code, publishErr)
						}
						slog.Warn("task creation blocked due to unmet dependency", "task_definition_code", taskDef.Code, "reason", reason)
						continue
					}
					return count, fmt.Errorf("failed to resolve task inputs for %s: %w", taskDef.Code, err)
				}
			}

			if err := document.ValidateTaskInput(ctx, validator, taskDef, resolvedInputs); err != nil {
				return count, fmt.Errorf("input schema validation failed for task %s: %w", taskDef.Code, err)
			}

			inputPayload, err := json.Marshal(resolvedInputs)
			if err != nil {
				return count, fmt.Errorf("failed to marshal resolved input payload for task %s: %w", taskDef.Code, err)
			}
			if inputPayload == nil {
				inputPayload = json.RawMessage("{}")
			}

			task := model.Task{
				CaseID:             caseID,
				TaskDefinitionCode: taskDef.Code,
				ActivityCode:       activity.Code,
				StageCode:          stageCode,
				IsDocumentVerification: taskDef.IsDocumentVerification,
				Status:             model.TaskStatusPending,
				Priority:           model.TaskPriorityNormal,
				MaxRetries:         exception.ResolveRetryPolicy(taskDef).MaxRetries,
				InputPayload:       inputPayload,
				IdempotencyKey:     idempotencyKey,
			}
			if taskDef.RequiresApproval && taskDef.Approval != nil {
				task.RequiresApproval = true
				approvalAmount, amountErr := resolveApprovalAmountForTask(taskDef, caseMetadata)
				if amountErr != nil {
					return count, fmt.Errorf("failed to resolve approval amount for task %s: %w", taskDef.Code, amountErr)
				}
				task.ApprovalAmount = approvalAmount
			}

			resolvedSLA, err := sla.ResolveEffectiveSLADefinition(caseTypeConfig, stageCode, activity.Code, taskDef.Code)
			if err != nil {
				return count, fmt.Errorf("failed to resolve SLA for task %s: %w", taskDef.Code, err)
			}

			var dueAt time.Time
			var durationMS int64
			var calendarID string
			if resolvedSLA != nil {
				if repo.SQLX == nil {
					return count, fmt.Errorf("sqlx db is not configured for SLA task %s", taskDef.Code)
				}
				dueAt, durationMS, calendarID, err = sla.ComputeSLADeadline(
					ctx,
					repo.SQLX,
					time.Now().UTC(),
					caseTypeConfig.DefaultCalendarID,
					*resolvedSLA,
				)
				if err != nil {
					return count, fmt.Errorf("failed to compute SLA deadline for task %s: %w", taskDef.Code, err)
				}
				task.DueAt = &dueAt
			}

			createdTask, err := repo.CreateTask(ctx, tx, task)
			if err != nil {
				return count, fmt.Errorf("failed to create task %s: %w", taskDef.Code, err)
			}

			if resolvedSLA != nil {
				_, err = tx.Exec(ctx, `
					UPDATE tasks
					SET task_due_at = $1,
					    effective_start_time = COALESCE(effective_start_time, created_at),
					    sla_duration_ms = $2,
					    sla_warning_threshold_pct = $3,
					    sla_critical_threshold_pct = $4,
					    sla_breach_action = $5,
					    sla_calendar_id = $6::uuid,
					    updated_at = now(),
					    version = version + 1
					WHERE id = $7::uuid
				`, dueAt, durationMS, resolvedSLA.WarningThresholdPct, resolvedSLA.CriticalThresholdPct, string(resolvedSLA.BreachAction), calendarID, createdTask.ID)
				if err != nil {
					return count, fmt.Errorf("failed to persist SLA snapshot for task %s: %w", taskDef.Code, err)
				}
			}

			if taskDef.RequiresApproval && taskDef.Approval != nil {
				if _, err := createApprovalGateForTask(ctx, tx, repo, caseID, createdTask.ID, taskDef, caseTypeConfig, task.ApprovalAmount); err != nil {
					return count, fmt.Errorf("failed to create approval gate for task %s: %w", taskDef.Code, err)
				}
			}

			count++
		}
	}

	return count, nil
}

func (o *CaseOrchestratorService) applyAggregationRulesForTask(ctx context.Context, caseID string, taskID string) error {
	if o.Repo == nil || o.Repo.SQLX == nil {
		return nil
	}

	tx, err := o.Repo.SQLX.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("applyAggregationRulesForTask: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var task model.Task
	if err := tx.QueryRowxContext(ctx, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			task_definition_code,
			input_payload,
			output_payload
		FROM tasks
		WHERE id = $1::uuid
	`, taskID).StructScan(&task); err != nil {
		return fmt.Errorf("applyAggregationRulesForTask: load task %s: %w", taskID, err)
	}

	var caseTypeID string
	if err := tx.QueryRowContext(ctx, `
		SELECT case_type_id::text
		FROM cases
		WHERE id = $1::uuid
	`, caseID).Scan(&caseTypeID); err != nil {
		return fmt.Errorf("applyAggregationRulesForTask: load case_type_id for case %s: %w", caseID, err)
	}

	config, err := o.Repo.GetCaseTypeConfig(ctx, caseTypeID)
	if err != nil {
		return fmt.Errorf("applyAggregationRulesForTask: load case type config: %w", err)
	}
	if config == nil || len(config.AggregationRules) == 0 {
		return tx.Commit()
	}

	if err := document.ApplyAggregationRules(ctx, tx, caseID, task, config.AggregationRules); err != nil {
		return fmt.Errorf("applyAggregationRulesForTask: apply rules: %w", err)
	}
	return tx.Commit()
}

func (o *CaseOrchestratorService) syncCompensationForTask(ctx context.Context, taskID string, status model.TaskStatus) error {
	if o.Repo == nil || o.Repo.SQLX == nil {
		return nil
	}
	tx, err := o.Repo.SQLX.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("syncCompensationForTask: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := exception.SyncCompensationStateForTask(ctx, tx, taskID, status); err != nil {
		return fmt.Errorf("syncCompensationForTask: sync state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("syncCompensationForTask: commit: %w", err)
	}
	return nil
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
