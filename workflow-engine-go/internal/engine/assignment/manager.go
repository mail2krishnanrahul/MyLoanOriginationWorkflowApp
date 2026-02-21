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
	// The auto_distributor logic historically mapped users to workers.
	// We'll pass tenantID from context (ideally) or pull from Task if we need it.
	// For now we map workerID -> userID.
	return m.AssignTaskToUser(ctx, tx, task.ID, workerID, task.TenantID, "auto_distributor")
}

// AssignTaskToUser assigns a task to a specific user (identity)
func (m *Manager) AssignTaskToUser(ctx context.Context, tx repository.DBExecutor, taskID string, userID string, tenantID string, assignedBy string) error {
	// Status Transition: Must be PENDING, ASSIGNED, or IN_PROGRESS to move
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assigned_user_id = $1::uuid,
		    status = 'IN_PROGRESS',
		    assigned_at = now(),
		    updated_at = now()
		WHERE id = $2::uuid AND tenant_id = $3::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS')`, userID, taskID, tenantID)
	if err != nil {
		return fmt.Errorf("AssignTaskToUser: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrTaskNotAssignable
	}

	// Publish Event
	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_ASSIGNED", map[string]interface{}{
		"task_id":     taskID,
		"user_id":     userID,
		"assigned_by": assignedBy,
	})
}

// AssignTaskToTeam allocates a task to a team pool
func (m *Manager) AssignTaskToTeam(ctx context.Context, tx repository.DBExecutor, taskID string, teamID string, tenantID string, assignedBy string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assigned_team_id = $1::uuid,
		    status = 'PENDING',
		    updated_at = now()
		WHERE id = $2::uuid AND tenant_id = $3::uuid
		  AND status = 'PENDING'`, teamID, taskID, tenantID)
	if err != nil {
		return fmt.Errorf("AssignTaskToTeam: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrTaskNotAssignable
	}

	// Publish Event
	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_ASSIGNED_TO_TEAM", map[string]interface{}{
		"task_id":     taskID,
		"team_id":     teamID,
		"assigned_by": assignedBy,
	})
}

// ClaimTask allows a user to pick a task from a team pool they belong to.
// Uses optimistic locking to handle concurrent claiming.
func (m *Manager) ClaimTask(ctx context.Context, tx repository.DBExecutor, taskID string, userID string, tenantID string) error {
	// 1. Read task to verify assignment pool and retrieve optimistic version
	var task struct {
		AssignedTeamID *string `db:"assigned_team_id"`
		Version        int     `db:"version"`
	}
	err := tx.QueryRow(ctx, `
		SELECT assigned_team_id::text, version 
		FROM tasks 
		WHERE id = $1::uuid AND tenant_id = $2::uuid
	`, taskID, tenantID).Scan(&task.AssignedTeamID, &task.Version)
	if err != nil {
		return fmt.Errorf("ClaimTask: load task: %w", err)
	}

	// 2. Verify User is a member of the required team if one is set
	if task.AssignedTeamID != nil {
		var isMember bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM team_members 
				WHERE team_id = $1::uuid AND user_id = $2::uuid AND tenant_id = $3::uuid
			)
		`, *task.AssignedTeamID, userID, tenantID).Scan(&isMember)
		if err != nil {
			return fmt.Errorf("ClaimTask: team check: %w", err)
		}
		if !isMember {
			return model.ErrUserNotTeamMember
		}
	}

	// 3. Optimistic Update
	res, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assigned_user_id = $1::uuid,
		    status = 'IN_PROGRESS',
		    assigned_at = now(),
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid AND tenant_id = $3::uuid
		  AND assigned_user_id IS NULL 
		  AND version = $4`, userID, taskID, tenantID, task.Version)
	if err != nil {
		return fmt.Errorf("ClaimTask: update task: %w", err)
	}
	rowsAffected := res.RowsAffected()
	if rowsAffected == 0 {
		return model.ErrTaskAlreadyClaimed
	}

	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_CLAIMED", map[string]interface{}{
		"task_id": taskID,
		"user_id": userID,
	})
}

// UnassignTask clears the AssignedUserID while leaving AssignedTeamID intact
// Returns the task to the original team pool to be claimed by others.
func (m *Manager) UnassignTask(ctx context.Context, tx repository.DBExecutor, taskID string, tenantID string, unassignedBy string, reason string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assigned_user_id = NULL,
		    status = CASE WHEN status = 'IN_PROGRESS' THEN 'PENDING' ELSE status END,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $1::uuid AND tenant_id = $2::uuid
		  AND assigned_user_id IS NOT NULL`, taskID, tenantID)
	if err != nil {
		return fmt.Errorf("UnassignTask: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrTaskNotAssignable
	}

	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_UNASSIGNED", map[string]interface{}{
		"task_id":       taskID,
		"unassigned_by": unassignedBy,
		"reason":        reason,
	})
}

// ReassignTask combines Unassign and Assign into a single atomic action for supervisors.
func (m *Manager) ReassignTask(ctx context.Context, tx repository.DBExecutor, taskID string, toUserID string, tenantID string, reassignedBy string, reason string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tasks
		SET assigned_user_id = $1::uuid,
		    status = 'IN_PROGRESS',
		    assigned_at = now(),
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid AND tenant_id = $3::uuid`, toUserID, taskID, tenantID)
	if err != nil {
		return fmt.Errorf("ReassignTask: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrTaskNotFound
	}

	return m.Repo.InsertOutboxEvent(ctx, tx, "TASK_REASSIGNED", map[string]interface{}{
		"task_id":       taskID,
		"to_user_id":    toUserID,
		"reassigned_by": reassignedBy,
		"reason":        reason,
	})
}
