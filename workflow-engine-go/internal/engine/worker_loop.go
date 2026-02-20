package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"workflow-engine/internal/exception"
	"workflow-engine/internal/integration"
	"workflow-engine/internal/multitenancy"
	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// TaskHandler is the function signature that workers plug in to process tasks.
type TaskHandler func(ctx context.Context, task model.Task) error

// WorkerLoopConfig holds tuning parameters for the worker loop.
type WorkerLoopConfig struct {
	ServiceName       string        // e.g. "credit-check-service"
	TenantID          string        // optional hard tenant pin for dedicated worker pools
	BatchSize         int           // tasks to claim per poll cycle
	PollInterval      time.Duration // how often to poll when idle
	HeartbeatInterval time.Duration // how often to heartbeat during processing
	StaleDuration     time.Duration // reclaim tasks with heartbeats older than this
	RetryBaseInterval time.Duration // base interval for exponential backoff
}

// DefaultWorkerConfig returns sensible production defaults.
func DefaultWorkerConfig(serviceName string) WorkerLoopConfig {
	return WorkerLoopConfig{
		ServiceName:       serviceName,
		BatchSize:         10,
		PollInterval:      2 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		StaleDuration:     5 * time.Minute,
		RetryBaseInterval: 5 * time.Second,
	}
}

// WorkerLoop is the main processing loop that claims, executes, and
// manages the lifecycle of tasks for a specific service.
type WorkerLoop struct {
	Repo     *repository.Repository
	Config   WorkerLoopConfig
	Handler  TaskHandler
	Registry *integration.HandlerRegistry

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewWorkerLoop creates a new WorkerLoop.
func NewWorkerLoop(repo *repository.Repository, config WorkerLoopConfig, handler TaskHandler) *WorkerLoop {
	return &WorkerLoop{
		Repo:    repo,
		Config:  config,
		Handler: handler,
	}
}

// SetHandlerRegistry installs a plugin registry for service-name based handlers.
func (w *WorkerLoop) SetHandlerRegistry(registry *integration.HandlerRegistry) {
	w.Registry = registry
}

// Start begins the worker loop in a background goroutine.
// It also starts a background goroutine for stale-task reclamation.
func (w *WorkerLoop) Start(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)
	if w.Registry != nil {
		w.Registry.MarkStarted()
	}

	// Main processing loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.pollLoop(ctx)
	}()

	// Stale-task reclamation loop
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.reclaimLoop(ctx)
	}()

	slog.Info("worker loop started",
		"service", w.Config.ServiceName,
		"batch_size", w.Config.BatchSize,
		"poll_interval", w.Config.PollInterval,
		"heartbeat_interval", w.Config.HeartbeatInterval)
}

// Stop gracefully shuts down the worker loop and waits for in-flight work.
func (w *WorkerLoop) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	slog.Info("worker loop stopped", "service", w.Config.ServiceName)
}

// ---------------------------------------------------------------------------
// Internal loops
// ---------------------------------------------------------------------------

func (w *WorkerLoop) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(w.Config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *WorkerLoop) reclaimLoop(ctx context.Context) {
	// Run reclamation at 2× the stale duration
	interval := w.Config.StaleDuration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scopedCtx := w.workerTenantContext(ctx)
			count, err := w.Repo.ReclaimStaleTasks(scopedCtx, w.Config.StaleDuration)
			if err != nil {
				slog.Error("stale task reclaim failed", "error", err, "service", w.Config.ServiceName, "tenant_id", w.Config.TenantID)
				continue
			}
			if count > 0 {
				slog.Info("reclaimed stale tasks", "count", count, "service", w.Config.ServiceName, "tenant_id", w.Config.TenantID)
			}
		}
	}
}

func (w *WorkerLoop) processBatch(ctx context.Context) {
	scopedCtx := w.workerTenantContext(ctx)
	tasks, err := w.Repo.ClaimTasks(scopedCtx, w.Config.ServiceName, w.Config.BatchSize)
	if err != nil {
		slog.Error("task claim failed", "error", err, "service", w.Config.ServiceName, "tenant_id", w.Config.TenantID)
		return
	}

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return
		default:
			w.processTask(scopedCtx, task)
		}
	}
}

func (w *WorkerLoop) processTask(ctx context.Context, task model.Task) {
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		registryHandler integration.TaskHandler
		registryEnabled bool
	)
	if w.Registry != nil {
		serviceName := w.Config.ServiceName
		if task.AssignedService != nil && strings.TrimSpace(*task.AssignedService) != "" {
			serviceName = strings.TrimSpace(*task.AssignedService)
		}
		h, ok := w.Registry.Lookup(serviceName)
		if !ok {
			if err := w.Repo.ReleaseTaskClaim(ctx, task.ID); err != nil {
				slog.Warn("no registered task handler and failed to release claim",
					"error", err, "task_id", task.ID, "assigned_service", serviceName, "tenant_id", task.TenantID)
			} else {
				slog.Warn("no registered task handler; task claim released",
					"task_id", task.ID, "assigned_service", serviceName, "tenant_id", task.TenantID)
			}
			return
		}
		registryEnabled = true
		registryHandler = h
	}

	// 1. Mark as IN_PROGRESS
	if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusInProgress, nil, nil); err != nil {
		slog.Error("failed to mark task IN_PROGRESS",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName, "tenant_id", task.TenantID)
		return
	}

	// Approval-enabled tasks may be moved to AWAITING_EXTERNAL when gate activation
	// creates approval requests. In that state the worker must not execute handler logic.
	var liveStatus string
	if err := w.Repo.Pool.QueryRow(ctx, `
		SELECT status
		FROM tasks
		WHERE id = $1::uuid
	`, task.ID).Scan(&liveStatus); err == nil && liveStatus == string(model.TaskStatusAwaitingExternal) {
		slog.Info("task moved to awaiting external; skipping worker execution", "task_id", task.ID, "service", w.Config.ServiceName)
		return
	}

	// 2. Start heartbeat goroutine
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(w.Config.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-taskCtx.Done():
				return
			case <-ticker.C:
				if err := w.Repo.Heartbeat(taskCtx, task.ID); err != nil {
					slog.Warn("heartbeat failed",
						"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
				}
			}
		}
	}()

	// 3. Execute handler
	var (
		handlerErr     error
		recoveredStack *string
		handlerResult  integration.TaskResult
	)
	if registryEnabled {
		handlerResult, recoveredStack, handlerErr = w.executeRegistryHandler(taskCtx, task, registryHandler)
	} else {
		recoveredStack, handlerErr = w.executeHandler(taskCtx, task)
	}
	cancel() // stop heartbeat

	// 4. Wait for heartbeat goroutine to finish
	<-heartbeatDone

	// 5. Handle result
	if handlerErr != nil {
		w.handleFailure(ctx, task, handlerErr, recoveredStack)
	} else {
		if registryEnabled {
			w.handleRegistryResult(ctx, task, handlerResult)
		} else {
			w.handleSuccess(ctx, task)
		}
	}
}

func (w *WorkerLoop) workerTenantContext(ctx context.Context) context.Context {
	if strings.TrimSpace(w.Config.TenantID) == "" {
		return ctx
	}
	return multitenancy.WithTenant(ctx, w.Config.TenantID)
}

func (w *WorkerLoop) executeHandler(ctx context.Context, task model.Task) (recoveredStack *string, handlerErr error) {
	defer func() {
		if rec := recover(); rec != nil {
			stack := string(debug.Stack())
			recoveredStack = &stack
			handlerErr = fmt.Errorf("worker panic recovered: %v", rec)
		}
	}()
	return nil, w.Handler(ctx, task)
}

func (w *WorkerLoop) executeRegistryHandler(ctx context.Context, task model.Task, handler integration.TaskHandler) (result integration.TaskResult, recoveredStack *string, handlerErr error) {
	defer func() {
		if rec := recover(); rec != nil {
			stack := string(debug.Stack())
			recoveredStack = &stack
			handlerErr = fmt.Errorf("worker panic recovered: %v", rec)
		}
	}()
	result, err := handler.Handle(ctx, task)
	if err != nil {
		return integration.TaskResult{}, nil, err
	}
	return result, nil, nil
}

func (w *WorkerLoop) handleSuccess(ctx context.Context, task model.Task) {
	if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusDone, nil, nil); err != nil {
		slog.Error("failed to complete task",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
	}
}

func (w *WorkerLoop) handleRegistryResult(ctx context.Context, task model.Task, result integration.TaskResult) {
	switch result.Status {
	case model.TaskStatusDone:
		var output json.RawMessage
		if len(result.OutputPayload) > 0 {
			output = json.RawMessage(result.OutputPayload)
		}
		if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusDone, output, nil); err != nil {
			slog.Error("failed to complete task via registry result",
				"error", err, "task_id", task.ID, "service", w.Config.ServiceName, "tenant_id", task.TenantID)
		}
	case model.TaskStatusFailed:
		var detail json.RawMessage
		if len(result.ErrorDetail) > 0 {
			detail = json.RawMessage(result.ErrorDetail)
		}
		if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusFailed, nil, detail); err != nil {
			slog.Error("failed to mark registry task as failed",
				"error", err, "task_id", task.ID, "service", w.Config.ServiceName, "tenant_id", task.TenantID)
		}
	default:
		slog.Error("invalid registry handler result status",
			"status", result.Status, "task_id", task.ID, "service", w.Config.ServiceName, "tenant_id", task.TenantID)
	}
}

func (w *WorkerLoop) handleFailure(ctx context.Context, task model.Task, handlerErr error, recoveredStack *string) {
	tenantID := task.TenantID
	if strings.TrimSpace(tenantID) == "" {
		if fromCtx, err := multitenancy.TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if strings.TrimSpace(tenantID) != "" {
		multitenancy.IncTasksFailed(tenantID, w.Config.ServiceName, "handler_error")
	}
	if w.Repo.SQLX != nil {
		tx, err := w.Repo.SQLX.BeginTxx(ctx, nil)
		if err != nil {
			slog.Error("failed to begin sqlx transaction for exception handling",
				"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
		} else {
			exceptionErr := exception.HandleTaskFailure(ctx, tx, exception.TaskFailureInput{
				TaskID:         task.ID,
				SourceService:  w.Config.ServiceName,
				Err:            handlerErr,
				RecoveredStack: recoveredStack,
			})
			if exceptionErr != nil {
				_ = tx.Rollback()
				slog.Error("exception handling failed, falling back to legacy failure path",
					"error", exceptionErr, "task_id", task.ID, "service", w.Config.ServiceName)
			} else if commitErr := tx.Commit(); commitErr != nil {
				slog.Error("failed to commit exception handling transaction, falling back to legacy failure path",
					"error", commitErr, "task_id", task.ID, "service", w.Config.ServiceName)
			} else {
				return
			}
		}
	}

	if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusFailed, nil, nil); err != nil {
		slog.Error("legacy fallback failed to mark task FAILED",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
		return
	}
	if err := w.Repo.ScheduleRetry(ctx, w.Repo.Pool, task.ID, w.Config.RetryBaseInterval); err != nil {
		slog.Warn("legacy fallback task not retryable",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
		return
	}
	slog.Info("legacy fallback scheduled task retry", "task_id", task.ID, "service", w.Config.ServiceName)
}

// ---------------------------------------------------------------------------
// Convenience: start a worker with a one-liner
// ---------------------------------------------------------------------------

// RunWorker is a convenience function that creates and starts a WorkerLoop,
// blocking until the context is cancelled.
func RunWorker(ctx context.Context, repo *repository.Repository, serviceName string, handler TaskHandler) {
	config := DefaultWorkerConfig(serviceName)
	loop := NewWorkerLoop(repo, config, handler)
	loop.Start(ctx)

	<-ctx.Done()
	loop.Stop()

	slog.Info("worker shutdown complete", "service", serviceName)
}
