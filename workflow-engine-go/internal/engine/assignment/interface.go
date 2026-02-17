package assignment

import (
	"context"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// AssignmentEngine decides who receives a task based on a strategy.
type AssignmentEngine interface {
	// FindCandidate returns the best worker ID for the task,
	// or ErrNoEligibleWorker if none qualify.
	FindCandidate(
		ctx context.Context,
		tx repository.DBExecutor,
		task *model.Task,
	) (workerID string, err error)
}
