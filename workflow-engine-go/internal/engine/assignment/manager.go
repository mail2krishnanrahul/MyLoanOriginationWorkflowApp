package assignment

import (
	"context"
	"fmt"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// ManagerRepository defines the data access methods required by Manager.
// Satisfied by *repository.Repository in production and by mocks in tests.
type ManagerRepository interface {
	InsertOutboxEvent(ctx context.Context, executor repository.DBExecutor, eventType string, payload map[string]interface{}) error
}

// Manager handles all assignment-related operations
type Manager struct {
	Repo ManagerRepository
}

func NewManager(repo ManagerRepository) *Manager {
	return &Manager{Repo: repo}
}

// AssignTaskToWorkbasket moves a task to a workbasket (UNASSIGNED -> WORKBASKET_QUEUED)
func (m *Manager) AssignToWorkbasket(ctx context.Context, tx repository.DBExecutor, taskID string, workbasketID string) error {
	// 1. Validate Workbasket exists? (FK handles it, but good to check)

	// 2. Update Task
	_, err := tx.Exec(ctx, `
		UPDATE tasks 
		SET workbasket_id = $1::uuid,
		    assignee_id = NULL,
		    status = 'PENDING', -- Workbasket Queued is modeled as PENDING state? Or 'ASSIGNED' to basket?
			-- The prompt says "UNASSIGNED -> WORKBASKET_QUEUED". 
			-- TaskStatus enum has 'PENDING', 'ASSIGNED', 'IN_PROGRESS'.
			-- PENDING usually means 'Created but not started/assigned'.
			-- Let's use PENDING for "In Workbasket".
			updated_at = now()
		WHERE id = $2::uuid`, workbasketID, taskID)
	if err != nil {
		return fmt.Errorf("failed to assign to workbasket: %w", err)
	}

	// 3. Publish Event
	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_QUEUED", map[string]interface{}{
		"task_id":       taskID,
		"workbasket_id": workbasketID,
	})
}

// AutoAssign triggers the AssignmentEngine to find a worker for a task in a bucket
func (m *Manager) AutoAssign(ctx context.Context, tx repository.DBExecutor, task *model.Task) error {
	// 1. Get Workbasket Strategy
	var strategy model.AssignmentStrategy
	err := tx.QueryRow(ctx, `SELECT strategy FROM workbaskets WHERE id = $1::uuid`, task.WorkbasketID).Scan(&strategy)
	if err != nil {
		return fmt.Errorf("failed to fetch workbasket strategy: %w", err)
	}

	var engine AssignmentEngine
	switch strategy {
	case model.StrategyRoundRobin:
		engine = NewRoundRobinAssignmentEngine()
	case model.StrategyLeastLoaded:
		engine = NewLeastLoadedAssignmentEngine()
	case model.StrategySkillScore:
		engine = NewSkillScoreAssignmentEngine()
	default:
		engine = NewRoundRobinAssignmentEngine()
	}

	// 2. Find Candidate
	workerID, err := engine.FindCandidate(ctx, tx, task)
	if err == ErrNoEligibleWorker {
		// Log warning, leave in basket
		return nil
	}
	if err != nil {
		return err
	}

	// 3. Assign
	return m.AssignTask(ctx, tx, task.ID, workerID, "auto_distributor")
}

// AssignTask assigns a task to a specific worker
func (m *Manager) AssignTask(ctx context.Context, tx repository.DBExecutor, taskID string, workerID string, assignedBy string) error {
	// Update Task
	_, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assignee_id = $1,
		    status = 'ASSIGNED',
		    assigned_at = now(),
		    updated_at = now()
		WHERE id = $2::uuid`, workerID, taskID)
	if err != nil {
		return fmt.Errorf("failed to assign task: %w", err)
	}

	// Publish Event
	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_ASSIGNED", map[string]interface{}{
		"task_id":     taskID,
		"worker_id":   workerID,
		"assigned_by": assignedBy,
	})
}

// ClaimTask allows a worker to pick a task from a basket
func (m *Manager) ClaimTask(ctx context.Context, tx repository.DBExecutor, taskID string, workerID string) error {
	// Validation: Is task in a basket? Is it already assigned?
	// Doing strict SQL check update

	res, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assignee_id = $1,
		    status = 'ASSIGNED',
		    assigned_at = now(),
		    updated_at = now()
		WHERE id = $2::uuid
		  AND assignee_id IS NULL -- Must be unassigned
		  AND workbasket_id IS NOT NULL`, workerID, taskID) // Must be in basket
	if err != nil {
		return fmt.Errorf("failed to claim task: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("task not available for claim")
	}

	return m.Repo.InsertOutboxEvent(ctx, tx, "WORKBASKET_TASK_CLAIMED", map[string]interface{}{
		"task_id":   taskID,
		"worker_id": workerID,
	})
}

// DelegateTask hands off a task from one worker to another.
// It atomically verifies that fromID is the current assignee to prevent
// broken delegation chains when concurrent delegations race.
func (m *Manager) DelegateTask(ctx context.Context, tx repository.DBExecutor, taskID string, fromID string, toID string, reason string, delegationType model.DelegationType) error {
	// 1. Atomically update task only if fromID is the current assignee
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assignee_id = $1,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid
		  AND assignee_id = $3`, toID, taskID, fromID)
	if err != nil {
		return fmt.Errorf("DelegateTask: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("DelegateTask: %w: task %s not assigned to %s", model.ErrDelegationChainBroken, taskID, fromID)
	}

	// 2. Record delegation audit (append-only chain)
	_, err = tx.Exec(ctx, `
		INSERT INTO task_delegations (task_id, from_assignee, to_assignee, reason, delegation_type, delegated_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $2)`,
		taskID, fromID, toID, reason, string(delegationType))
	if err != nil {
		return fmt.Errorf("DelegateTask: insert delegation audit: %w", err)
	}

	// 3. Publish event
	return m.Repo.InsertOutboxEvent(ctx, tx, string(model.EventTaskDelegated), map[string]interface{}{
		"task_id": taskID,
		"from":    fromID,
		"to":      toID,
		"reason":  reason,
		"type":    string(delegationType),
	})
}

// ---------------------------------------------------------------------------
// [B-07 FIX] ReassignTask — supervisor-only reassignment, distinct from delegation
// ---------------------------------------------------------------------------

// ReassignTask is a supervisor-initiated reassignment of a task to a different
// worker. Unlike DelegateTask (which is worker-to-worker), reassignment
// requires the SUPERVISOR role, publishes a distinct TASK_REASSIGNED event,
// and is recorded separately in the delegation audit trail.
func (m *Manager) ReassignTask(ctx context.Context, tx repository.DBExecutor, taskID string, newWorkerID string, supervisorID string, reason string) error {
	// 1. Enforce SUPERVISOR role via assignment guard
	if err := ValidateAssignmentTransition(ctx, StateAssigned, StateReassigned, supervisorID, "SUPERVISOR"); err != nil {
		return fmt.Errorf("ReassignTask: %w", err)
	}

	// 2. Lock and read current assignee
	var currentAssignee *string
	err := tx.QueryRow(ctx, `
		SELECT assignee_id
		FROM tasks
		WHERE id = $1::uuid
		FOR UPDATE`, taskID,
	).Scan(&currentAssignee)
	if err != nil {
		return fmt.Errorf("ReassignTask: read task %s: %w", taskID, err)
	}

	// 3. Prevent self-reassignment to same worker
	if currentAssignee != nil && *currentAssignee == newWorkerID {
		return fmt.Errorf("ReassignTask: task %s is already assigned to %s", taskID, newWorkerID)
	}

	// 4. Validate target worker is active and under capacity
	var workerStatus string
	var maxConcurrent, currentLoad int
	err = tx.QueryRow(ctx, `
		SELECT w.status, w.max_concurrent_tasks,
		       (SELECT COUNT(*) FROM tasks t WHERE t.assignee_id = w.id AND t.status = 'IN_PROGRESS')
		FROM workers w
		WHERE w.id = $1`, newWorkerID,
	).Scan(&workerStatus, &maxConcurrent, &currentLoad)
	if err != nil {
		return fmt.Errorf("ReassignTask: validate worker %s: %w", newWorkerID, err)
	}
	if workerStatus != "ACTIVE" {
		return fmt.Errorf("ReassignTask: worker %s is %s, not ACTIVE", newWorkerID, workerStatus)
	}
	if currentLoad >= maxConcurrent {
		return fmt.Errorf("ReassignTask: %w: worker %s has %d/%d tasks",
			model.ErrWorkerAtCapacity, newWorkerID, currentLoad, maxConcurrent)
	}

	// 5. Reassign the task
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assignee_id = $1,
		    status = 'ASSIGNED',
		    assigned_at = now(),
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid`, newWorkerID, taskID)
	if err != nil {
		return fmt.Errorf("ReassignTask: update task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ReassignTask: %w: %s", model.ErrTaskNotFound, taskID)
	}

	// 6. Record in delegation audit trail (as MANUAL type by supervisor)
	var fromStr string
	if currentAssignee != nil {
		fromStr = *currentAssignee
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO task_delegations (task_id, from_assignee, to_assignee, reason, delegation_type, delegated_by)
		VALUES ($1::uuid, $2, $3, $4, 'MANUAL', $5)`,
		taskID, fromStr, newWorkerID, reason, supervisorID)
	if err != nil {
		return fmt.Errorf("ReassignTask: insert audit: %w", err)
	}

	// 7. Publish TASK_REASSIGNED event (distinct from TASK_DELEGATED)
	return m.Repo.InsertOutboxEvent(ctx, tx, string(model.EventTaskReassigned), map[string]interface{}{
		"task_id":       taskID,
		"from_worker":   fromStr,
		"to_worker":     newWorkerID,
		"supervisor_id": supervisorID,
		"reason":        reason,
	})
}
