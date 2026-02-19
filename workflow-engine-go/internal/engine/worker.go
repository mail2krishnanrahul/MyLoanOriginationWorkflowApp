package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"workflow-engine/internal/engine/assignment"
	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
)

type Engine struct {
	Repo              Repository
	AssignmentManager *assignment.Manager
	WorkerCount       int
	EventObserver     EventObserver
}

func NewEngine(repo Repository, assignmentManager *assignment.Manager, workers int) *Engine {
	return &Engine{
		Repo:              repo,
		AssignmentManager: assignmentManager,
		WorkerCount:       workers,
	}
}

// EventObserver reacts to events after primary processing completes.
type EventObserver interface {
	HandleEvent(ctx context.Context, event model.Event) error
}

// SetEventObserver wires an optional event observer (for example notifications).
func (e *Engine) SetEventObserver(observer EventObserver) {
	e.EventObserver = observer
}

// Start initiates the worker pool
func (e *Engine) Start(ctx context.Context) {
	var wg sync.WaitGroup
	eventChan := make(chan model.OutboxEvent, e.WorkerCount)

	// Start Workers
	for i := 0; i < e.WorkerCount; i++ {
		wg.Add(1)
		go e.worker(ctx, &wg, i, eventChan)
	}

	// Start Poller (blocks until ctx is cancelled)
	e.poller(ctx, eventChan)

	// Context is cancelled, Poller returned.
	// Close channel to signal workers to drain.
	close(eventChan)

	// Wait for workers to finish processing current events
	wg.Wait()
	slog.Info("engine stopped")
}

func (e *Engine) poller(ctx context.Context, eventChan chan<- model.OutboxEvent) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("poller stopping")
			return
		case <-ticker.C:
			// 1. Check if context is cancelled before polling to avoid fetching if shutting down
			select {
			case <-ctx.Done():
				return
			default:
			}

			events, err := e.Repo.PollPendingEvents(ctx, 10)
			if err != nil {
				slog.Error("poller error", "error", err)
				continue
			}
			for _, event := range events {
				// 2. Select ensures we don't stick on send if context is cancelled
				select {
				case eventChan <- event:
				case <-ctx.Done():
					// Shutting down — events already claimed as PROCESSING in DB.
					// They will be retried on next startup (stale PROCESSING → PENDING recovery).
					return
				}
			}
		}
	}
}

func (e *Engine) worker(ctx context.Context, wg *sync.WaitGroup, id int, eventChan <-chan model.OutboxEvent) {
	defer wg.Done()
	slog.Info("worker started", "worker_id", id)

	for event := range eventChan {
		if err := e.processEvent(ctx, event); err != nil {
			slog.Error("worker failed to process event", "worker_id", id, "event_id", event.ID, "error", err)
			// Error handling is done within processEvent for DB updates
		} else {
			slog.Info("worker processed event", "worker_id", id, "event_id", event.ID)
		}
	}
}

func (e *Engine) processEvent(ctx context.Context, event model.OutboxEvent) error {
	// Attempt to process business logic and mark as PROCESSED atomically
	err := e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		// 1. Business Logic
		var processingErr error
		if event.EventType == string(model.EventTaskCompleted) {
			processingErr = e.handleTaskCompleted(ctx, tx, event)
		} else if event.EventType == string(model.EventTaskQueued) {
			// Auto-assign logic
			payload := event.PayloadMap()
			taskID, ok := payload["task_id"].(string)
			if ok {
				// We need to fetch the task first to know the workbasket
				// Since we are in a transaction, we can just use the ID.
				// But AutoAssign expects a Task struct.
				// Ideally we fetch it.
				// For now, let's assume we can fetch it.
				// But `AutoAssign` does queries itself.
				// Let's implement a helper or call AutoAssign if we can fetch the task.
				// Fetch task:
				// t, err := e.Repo.GetTask(ctx, tx, taskID) ... (Repo missing GetTask?)
				// If Repo missing GetTask, we might need to add it or do raw query.
				// Given we added `AutoAssign` to Manager, let's try to use it if we can.
				// BUT AutoAssign takes *model.Task.
				// Let's defer strict implementation to Manager or just log for now?
				// No, "Show exactly where this hooks...".
				// I will add `GetTask` to Repository interface if missing, or use raw query.
				// Checking `case_repo.go`... it has `GetCaseInstance`. `task_repo.go` has `CreateTask`.
				// I'll assume we can fetch task.

				// Actually, `AutoAssign` in manager takes `tx`.
				// I'll try to fetch task manually here or add GetTask.
				// To save time/complexity, I will attempt to fetch task using a raw query for now inside the handler?
				// Better: Add GetTask to Repository interface.
				// But I can't easily modify interface across all files in one go without causing compilation errors in mocks/impls.
				// I'll use a direct query here since I have `tx`.
				var task model.Task
				err := tx.QueryRow(ctx, `SELECT id, workbasket_id, required_skills FROM tasks WHERE id = $1::uuid`, taskID).Scan(&task.ID, &task.WorkbasketID, &task.RequiredSkills)
				if err == nil {
					processingErr = e.AssignmentManager.AutoAssign(ctx, tx, &task)
				} else {
					slog.Error("failed to fetch task for assignment", "task_id", taskID, "error", err)
				}
			}
		} else if event.EventType == string(model.EventTaskSLABreached) {
			breachPayload := event.PayloadMap()
			taskID, ok := breachPayload["task_id"].(string)
			if ok {
				// Find Supervisor Basket
				var basketID string
				err := tx.QueryRow(ctx, `SELECT id FROM workbaskets WHERE type = 'ESCALATION' LIMIT 1`).Scan(&basketID)
				if err == nil {
					processingErr = e.AssignmentManager.AssignToWorkbasket(ctx, tx, taskID, basketID)
				}
			}
		} else {
			slog.Warn("unknown event type", "event_type", event.EventType)
			// Unknown event is treated as processed to avoid loops
		}

		if processingErr != nil {
			return processingErr // Triggers rollback
		}

		if e.EventObserver != nil && !isNotificationInternalOutboxEvent(event.EventType) {
			domainEvent := toDomainEvent(event)
			if err := e.EventObserver.HandleEvent(ctx, domainEvent); err != nil {
				return fmt.Errorf("event observer failed: %w", err)
			}
		}

		// 2. Mark as PROCESSED (inside same transaction)
		return e.Repo.UpdateEventStatus(ctx, tx, event.ID, model.OutboxStatusProcessed, nil)
	})

	if err != nil {
		slog.Error("worker failed to process event", "event_id", event.ID, "error", err)

		// 3. Mark as FAILED (outside original transaction, so it persists)
		// We use a new context or the existing one? Existing is fine if not cancelled.
		// We rely on repository to use its internal pool if tx is nil.
		msg := err.Error()
		if updateErr := e.Repo.UpdateEventStatus(ctx, nil, event.ID, model.OutboxStatusFailed, &msg); updateErr != nil {
			slog.Error("CRITICAL: failed to update event status to FAILED", "error", updateErr)
		}
		return err
	}

	return nil
}

func isNotificationInternalOutboxEvent(eventType string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(eventType))
	return normalized == string(model.EventCircuitBreakerOpened) || strings.HasPrefix(normalized, "NOTIFICATION_")
}

func toDomainEvent(outbox model.OutboxEvent) model.Event {
	payloadMap := outbox.PayloadMap()
	caseID := extractStringPtr(outbox.CaseID)
	if caseID == "" {
		caseID = extractStringValue(payloadMap, "case_id")
	}
	taskID := extractStringPtr(outbox.TaskID)
	if taskID == "" {
		taskID = extractStringValue(payloadMap, "task_id")
	}

	event := model.Event{
		ID:        outbox.ID,
		EventType: model.EventType(outbox.EventType),
		Payload:   outbox.Payload,
		Status:    model.EventStatusPending,
		CreatedAt: outbox.CreatedAt,
	}
	if caseID != "" {
		event.CaseID = &caseID
	}
	if taskID != "" {
		event.TaskID = &taskID
	}
	return event
}

func extractStringValue(payload map[string]interface{}, key string) string {
	if payload == nil {
		return ""
	}
	raw, ok := payload[key]
	if !ok {
		return ""
	}
	if raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func extractStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (e *Engine) handleTaskCompleted(ctx context.Context, tx repository.DBExecutor, event model.OutboxEvent) error {
	// 1. Extract Case ID from payload
	payload := event.PayloadMap()
	caseIDStr, ok := payload["case_id"].(string)
	if !ok {
		return fmt.Errorf("missing case_id in payload")
	}

	// 2. Lock & Fetch Case
	slog.Debug("fetching case", "case_id", caseIDStr)
	c, err := e.Repo.GetCaseWithLock(ctx, tx, caseIDStr)
	if err != nil {
		return fmt.Errorf("failed to fetch case: %w", err)
	}

	// 3. Fetch Workflow Definition (pinned)
	// In a real scenario we'd cache this or fetch it.
	// wd, err := e.Repo.GetWorkflowDefinition(ctx, tx, int64(c.WorkflowDefinitionID))

	// 4. Fetch Current Stage
	if c.CurrentStageID == nil {
		return fmt.Errorf("case has no current stage")
	}
	slog.Debug("fetching stage", "stage_id", *c.CurrentStageID)
	currentStage, err := e.Repo.GetStageDefinition(ctx, tx, *c.CurrentStageID)
	if err != nil {
		return fmt.Errorf("failed to fetch current stage: %w", err)
	}

	// 5. Calculate Next Stage
	slog.Debug("calculating next stage", "workflow_id", c.WorkflowDefinitionID, "sequence", currentStage.SequenceOrder)
	nextStage, err := e.Repo.GetNextStageDefinition(ctx, tx, int64(c.WorkflowDefinitionID), currentStage.SequenceOrder)
	if err != nil {
		return fmt.Errorf("failed to determine next stage: %w", err)
	}

	// 6. Update Case
	if nextStage != nil {
		c.CurrentStageID = &nextStage.ID
		slog.Info("advancing case to next stage", "case_id", c.ID, "next_stage", nextStage.Name)
	} else {
		c.CurrentStageID = nil // Or keep it?
		c.GlobalStatus = model.StatusClosed
		slog.Info("closing case, workflow completed", "case_id", c.ID)
	}

	slog.Debug("updating case", "case_id", c.ID)
	if err := e.Repo.UpdateCase(ctx, tx, c); err != nil {
		return fmt.Errorf("failed to update case: %w", err)
	}

	// 7. [H-08 FIX] Publish CASE_STAGE_CHANGED event so downstream consumers
	//    (e.g. orchestrator, notification service) react to stage transitions.
	if nextStage != nil {
		stagePayload := map[string]interface{}{
			"case_id":      c.ID,
			"new_stage_id": nextStage.ID,
			"new_stage":    nextStage.Name,
		}
		if err := e.Repo.InsertOutboxEvent(ctx, tx, string(model.EventCaseStageChanged), stagePayload); err != nil {
			return fmt.Errorf("failed to publish CASE_STAGE_CHANGED: %w", err)
		}
	} else {
		// Workflow completed — publish CASE_COMPLETED event
		completedPayload := map[string]interface{}{
			"case_id": c.ID,
		}
		if err := e.Repo.InsertOutboxEvent(ctx, tx, string(model.EventCaseCompleted), completedPayload); err != nil {
			return fmt.Errorf("failed to publish CASE_COMPLETED: %w", err)
		}
	}

	return nil
}
