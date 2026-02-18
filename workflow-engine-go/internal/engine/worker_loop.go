package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// TaskHandler is the function signature that workers plug in to process tasks.
type TaskHandler func(ctx context.Context, task model.Task) error

// WorkerLoopConfig holds tuning parameters for the worker loop.
type WorkerLoopConfig struct {
	ServiceName       string        // e.g. "credit-check-service"
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
	Repo    *repository.Repository
	Config  WorkerLoopConfig
	Handler TaskHandler

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

// Start begins the worker loop in a background goroutine.
// It also starts a background goroutine for stale-task reclamation.
func (w *WorkerLoop) Start(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)

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
			count, err := w.Repo.ReclaimStaleTasks(ctx, w.Config.StaleDuration)
			if err != nil {
				slog.Error("stale task reclaim failed", "error", err, "service", w.Config.ServiceName)
				continue
			}
			if count > 0 {
				slog.Info("reclaimed stale tasks", "count", count, "service", w.Config.ServiceName)
			}
		}
	}
}

func (w *WorkerLoop) processBatch(ctx context.Context) {
	tasks, err := w.Repo.ClaimTasks(ctx, w.Config.ServiceName, w.Config.BatchSize)
	if err != nil {
		slog.Error("task claim failed", "error", err, "service", w.Config.ServiceName)
		return
	}

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return
		default:
			w.processTask(ctx, task)
		}
	}
}

func (w *WorkerLoop) processTask(ctx context.Context, task model.Task) {
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 1. Mark as IN_PROGRESS
	if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusInProgress, nil, nil); err != nil {
		slog.Error("failed to mark task IN_PROGRESS",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
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
	handlerErr := w.Handler(taskCtx, task)
	cancel() // stop heartbeat

	// 4. Wait for heartbeat goroutine to finish
	<-heartbeatDone

	// 5. Handle result
	if handlerErr != nil {
		w.handleFailure(ctx, task, handlerErr)
	} else {
		w.handleSuccess(ctx, task)
	}
}

func (w *WorkerLoop) handleSuccess(ctx context.Context, task model.Task) {
	if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusDone, nil, nil); err != nil {
		slog.Error("failed to complete task",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
	}
}

func (w *WorkerLoop) handleFailure(ctx context.Context, task model.Task, handlerErr error) {
	errDetail, _ := json.Marshal(map[string]interface{}{
		"error":     handlerErr.Error(),
		"timestamp": time.Now().UTC(),
	})

	// Mark FAILED first
	if err := w.Repo.UpdateTaskStatus(ctx, w.Repo.Pool, task.ID, model.TaskStatusFailed, nil, errDetail); err != nil {
		slog.Error("failed to mark task FAILED",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
		return
	}

	// Attempt retry scheduling (exponential backoff)
	if err := w.Repo.ScheduleRetry(ctx, w.Repo.Pool, task.ID, w.Config.RetryBaseInterval); err != nil {
		// Max retries exceeded or other issue — task stays FAILED
		slog.Warn("task not retryable",
			"error", err, "task_id", task.ID, "service", w.Config.ServiceName)
		return
	}

	slog.Info("task scheduled for retry",
		"task_id", task.ID, "attempt", task.RetryCount+1, "service", w.Config.ServiceName)
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
