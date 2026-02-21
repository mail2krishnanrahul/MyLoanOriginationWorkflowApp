package multitenancy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// AssignTaskToUser assigns a PENDING task directly to an ACTIVE, claim-capable user.
func AssignTaskToUser(
	ctx context.Context,
	db *sqlx.DB,
	taskID string,
	userID string,
	tenantID string,
	assignedBy string,
) error {
	taskID = strings.TrimSpace(taskID)
	userID = strings.TrimSpace(userID)
	assignedBy = strings.TrimSpace(assignedBy)
	if taskID == "" || userID == "" {
		return fmt.Errorf("AssignTaskToUser: taskID and userID are required")
	}
	if assignedBy == "" {
		assignedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "AssignTaskToUser")
	if err != nil {
		return fmt.Errorf("AssignTaskToUser: %w", err)
	}

	var status string
	if err := db.GetContext(ctx, &status, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AssignTaskToUser: %w", ErrUserNotFound)
		}
		return fmt.Errorf("AssignTaskToUser: load user status: %w", err)
	}

	s := normalizeUserStatus(UserStatus(status))
	if s == UserStatusSuspended {
		return fmt.Errorf("AssignTaskToUser: %w", ErrUserSuspended)
	}
	if s == UserStatusDeactivated {
		return fmt.Errorf("AssignTaskToUser: %w", ErrUserDeactivated)
	}

	hasClaimPermission, err := UserHasPermission(ctx, db, userID, resolvedTenantID, PermissionTaskClaim)
	if err != nil {
		return fmt.Errorf("AssignTaskToUser: check TASK_CLAIM permission: %w", err)
	}
	if !hasClaimPermission {
		return fmt.Errorf("AssignTaskToUser: %w", permissionDeniedError(PermissionTaskClaim))
	}

	tx, err := beginSQLXTx(ctx, db, "AssignTaskToUser")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var caseID string
	if err := tx.GetContext(ctx, &caseID, `
		UPDATE tasks
		SET assigned_user_id = $1::uuid,
		    status = 'IN_PROGRESS',
		    assigned_at = now(),
		    updated_at = now(),
		    version = version + 1
		WHERE tenant_id = $2::uuid
		  AND id = $3::uuid
		  AND status = 'PENDING'
		RETURNING case_id::text
	`, userID, resolvedTenantID, taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AssignTaskToUser: %w", ErrTaskNotAssignable)
		}
		return fmt.Errorf("AssignTaskToUser: update task assignment: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, &caseID, &taskID, model.EventTaskAssigned, map[string]interface{}{
		"task_id":          taskID,
		"case_id":          caseID,
		"tenant_id":        resolvedTenantID,
		"assigned_user_id": userID,
		"assigned_by":      assignedBy,
		"occurred_at":      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("AssignTaskToUser: publish TASK_ASSIGNED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AssignTaskToUser: commit: %w", err)
	}

	logUserTeamInfo("task assigned to user", "tenant_id", resolvedTenantID, "task_id", taskID, "user_id", userID, "assigned_by", assignedBy)
	return nil
}

// AssignTaskToTeam assigns a PENDING task to a team queue (pool assignment).
func AssignTaskToTeam(
	ctx context.Context,
	db *sqlx.DB,
	taskID string,
	teamID string,
	tenantID string,
	assignedBy string,
) error {
	taskID = strings.TrimSpace(taskID)
	teamID = strings.TrimSpace(teamID)
	assignedBy = strings.TrimSpace(assignedBy)
	if taskID == "" || teamID == "" {
		return fmt.Errorf("AssignTaskToTeam: taskID and teamID are required")
	}
	if assignedBy == "" {
		assignedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "AssignTaskToTeam")
	if err != nil {
		return fmt.Errorf("AssignTaskToTeam: %w", err)
	}

	var teamStatus string
	if err := db.GetContext(ctx, &teamStatus, `
		SELECT status
		FROM teams
		WHERE tenant_id = $1::uuid
		  AND team_id = $2::uuid
	`, resolvedTenantID, teamID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AssignTaskToTeam: %w", ErrTeamNotFound)
		}
		return fmt.Errorf("AssignTaskToTeam: load team status: %w", err)
	}
	if normalizeTeamStatus(TeamStatus(teamStatus)) == TeamStatusDisbanded {
		return fmt.Errorf("AssignTaskToTeam: %w", ErrTeamDisbanded)
	}

	tx, err := beginSQLXTx(ctx, db, "AssignTaskToTeam")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var caseID string
	if err := tx.GetContext(ctx, &caseID, `
		UPDATE tasks
		SET assigned_team_id = $1::uuid,
		    assigned_user_id = NULL,
		    status = 'PENDING',
		    updated_at = now(),
		    version = version + 1
		WHERE tenant_id = $2::uuid
		  AND id = $3::uuid
		  AND status = 'PENDING'
		RETURNING case_id::text
	`, teamID, resolvedTenantID, taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AssignTaskToTeam: %w", ErrTaskNotAssignable)
		}
		return fmt.Errorf("AssignTaskToTeam: update task assignment: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, &caseID, &taskID, model.EventTaskAssignedToTeam, map[string]interface{}{
		"task_id":          taskID,
		"case_id":          caseID,
		"tenant_id":        resolvedTenantID,
		"assigned_team_id": teamID,
		"assigned_by":      assignedBy,
		"occurred_at":      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("AssignTaskToTeam: publish TASK_ASSIGNED_TO_TEAM: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AssignTaskToTeam: commit: %w", err)
	}

	logUserTeamInfo("task assigned to team", "tenant_id", resolvedTenantID, "task_id", taskID, "team_id", teamID, "assigned_by", assignedBy)
	return nil
}

// ClaimTask claims a team-assigned task for a user using optimistic locking on task.version.
func ClaimTask(
	ctx context.Context,
	db *sqlx.DB,
	taskID string,
	userID string,
	tenantID string,
) error {
	taskID = strings.TrimSpace(taskID)
	userID = strings.TrimSpace(userID)
	if taskID == "" || userID == "" {
		return fmt.Errorf("ClaimTask: taskID and userID are required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "ClaimTask")
	if err != nil {
		return fmt.Errorf("ClaimTask: %w", err)
	}

	var userStatus string
	if err := db.GetContext(ctx, &userStatus, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ClaimTask: %w", ErrUserNotFound)
		}
		return fmt.Errorf("ClaimTask: load user status: %w", err)
	}

	s := normalizeUserStatus(UserStatus(userStatus))
	if s == UserStatusSuspended {
		return fmt.Errorf("ClaimTask: %w", ErrUserSuspended)
	}
	if s == UserStatusDeactivated {
		return fmt.Errorf("ClaimTask: %w", ErrUserDeactivated)
	}

	hasClaimPermission, err := UserHasPermission(ctx, db, userID, resolvedTenantID, PermissionTaskClaim)
	if err != nil {
		return fmt.Errorf("ClaimTask: check TASK_CLAIM permission: %w", err)
	}
	if !hasClaimPermission {
		return fmt.Errorf("ClaimTask: %w", permissionDeniedError(PermissionTaskClaim))
	}

	tx, err := beginSQLXTx(ctx, db, "ClaimTask")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	type taskState struct {
		CaseID         string         `db:"case_id"`
		Status         string         `db:"status"`
		Version        int            `db:"version"`
		AssignedTeamID sql.NullString `db:"assigned_team_id"`
		AssignedUserID sql.NullString `db:"assigned_user_id"`
	}
	var state taskState
	if err := tx.GetContext(ctx, &state, `
		SELECT
			case_id::text AS case_id,
			status,
			version,
			assigned_team_id::text AS assigned_team_id,
			assigned_user_id::text AS assigned_user_id
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND id = $2::uuid
	`, resolvedTenantID, taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ClaimTask: %w", ErrTaskNotAssignable)
		}
		return fmt.Errorf("ClaimTask: load task state: %w", err)
	}

	if !state.AssignedTeamID.Valid || strings.TrimSpace(state.AssignedTeamID.String) == "" {
		return fmt.Errorf("ClaimTask: %w", ErrTaskNotAssignable)
	}
	if state.AssignedUserID.Valid && strings.TrimSpace(state.AssignedUserID.String) != "" {
		return fmt.Errorf("ClaimTask: %w", ErrTaskAlreadyClaimed)
	}
	if !strings.EqualFold(state.Status, "PENDING") && !strings.EqualFold(state.Status, "ASSIGNED") {
		return fmt.Errorf("ClaimTask: %w", ErrTaskNotAssignable)
	}

	var membership bool
	if err := tx.GetContext(ctx, &membership, `
		SELECT EXISTS (
			SELECT 1
			FROM team_members
			WHERE tenant_id = $1::uuid
			  AND team_id = $2::uuid
			  AND user_id = $3::uuid
		)
	`, resolvedTenantID, state.AssignedTeamID.String, userID); err != nil {
		return fmt.Errorf("ClaimTask: validate team membership: %w", err)
	}
	if !membership {
		return fmt.Errorf("ClaimTask: %w", ErrUserNotTeamMember)
	}

	tag, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET assigned_user_id = $1::uuid,
		    status = 'IN_PROGRESS',
		    assigned_at = now(),
		    updated_at = now(),
		    version = version + 1
		WHERE tenant_id = $2::uuid
		  AND id = $3::uuid
		  AND version = $4
		  AND assigned_team_id = $5::uuid
		  AND assigned_user_id IS NULL
	`, userID, resolvedTenantID, taskID, state.Version, state.AssignedTeamID.String)
	if err != nil {
		return fmt.Errorf("ClaimTask: claim update: %w", err)
	}
	rowsAffected, _ := tag.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("ClaimTask: %w", ErrTaskAlreadyClaimed)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, &state.CaseID, &taskID, model.EventTaskClaimed, map[string]interface{}{
		"task_id":          taskID,
		"case_id":          state.CaseID,
		"tenant_id":        resolvedTenantID,
		"claimed_by_user":  userID,
		"assigned_team_id": state.AssignedTeamID.String,
		"occurred_at":      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("ClaimTask: publish TASK_CLAIMED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ClaimTask: commit: %w", err)
	}

	logUserTeamInfo("task claimed", "tenant_id", resolvedTenantID, "task_id", taskID, "user_id", userID, "team_id", state.AssignedTeamID.String)
	return nil
}

// UnassignTask clears direct assignee and keeps team pool assignment intact.
func UnassignTask(
	ctx context.Context,
	db *sqlx.DB,
	taskID string,
	tenantID string,
	unassignedBy string,
	reason string,
) error {
	taskID = strings.TrimSpace(taskID)
	unassignedBy = strings.TrimSpace(unassignedBy)
	reason = strings.TrimSpace(reason)
	if taskID == "" {
		return fmt.Errorf("UnassignTask: taskID is required")
	}
	if unassignedBy == "" {
		unassignedBy = actorUserIDFromContext(ctx)
	}
	if unassignedBy == "" {
		return fmt.Errorf("UnassignTask: unassignedBy is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "UnassignTask")
	if err != nil {
		return fmt.Errorf("UnassignTask: %w", err)
	}
	if err := AssertPermission(ctx, db, unassignedBy, resolvedTenantID, PermissionTaskReassign); err != nil {
		if errors.Is(err, ErrPermissionDenied) {
			return fmt.Errorf("UnassignTask: %w", ErrTaskReassignForbidden)
		}
		return fmt.Errorf("UnassignTask: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "UnassignTask")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	type taskState struct {
		CaseID         string         `db:"case_id"`
		Status         string         `db:"status"`
		AssignedUserID sql.NullString `db:"assigned_user_id"`
		AssignedTeamID sql.NullString `db:"assigned_team_id"`
	}
	var state taskState
	if err := tx.GetContext(ctx, &state, `
		SELECT
			case_id::text AS case_id,
			status,
			assigned_user_id::text AS assigned_user_id,
			assigned_team_id::text AS assigned_team_id
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND id = $2::uuid
		FOR UPDATE
	`, resolvedTenantID, taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("UnassignTask: %w", ErrTaskNotAssignable)
		}
		return fmt.Errorf("UnassignTask: lock task: %w", err)
	}

	if !strings.EqualFold(state.Status, "PENDING") &&
		!strings.EqualFold(state.Status, "ASSIGNED") &&
		!strings.EqualFold(state.Status, "IN_PROGRESS") &&
		!strings.EqualFold(state.Status, "AWAITING_EXTERNAL") {
		return fmt.Errorf("UnassignTask: %w", ErrTaskNotAssignable)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET assigned_user_id = NULL,
		    status = 'PENDING',
		    assigned_at = NULL,
		    updated_at = now(),
		    version = version + 1
		WHERE tenant_id = $1::uuid
		  AND id = $2::uuid
	`, resolvedTenantID, taskID); err != nil {
		return fmt.Errorf("UnassignTask: update task: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, &state.CaseID, &taskID, model.EventTaskUnassigned, map[string]interface{}{
		"task_id":          taskID,
		"case_id":          state.CaseID,
		"tenant_id":        resolvedTenantID,
		"previous_user_id": nullableString(state.AssignedUserID),
		"assigned_team_id": nullableString(state.AssignedTeamID),
		"unassigned_by":    unassignedBy,
		"reason":           reason,
		"occurred_at":      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("UnassignTask: publish TASK_UNASSIGNED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("UnassignTask: commit: %w", err)
	}

	logUserTeamInfo("task unassigned", "tenant_id", resolvedTenantID, "task_id", taskID, "unassigned_by", unassignedBy, "reason", reason)
	return nil
}

// ReassignTask atomically reassigns task ownership and emits one TASK_REASSIGNED event.
func ReassignTask(
	ctx context.Context,
	db *sqlx.DB,
	taskID string,
	toUserID string,
	tenantID string,
	reassignedBy string,
	reason string,
) error {
	taskID = strings.TrimSpace(taskID)
	toUserID = strings.TrimSpace(toUserID)
	reassignedBy = strings.TrimSpace(reassignedBy)
	reason = strings.TrimSpace(reason)
	if taskID == "" || toUserID == "" {
		return fmt.Errorf("ReassignTask: taskID and toUserID are required")
	}
	if reassignedBy == "" {
		reassignedBy = actorUserIDFromContext(ctx)
	}
	if reassignedBy == "" {
		return fmt.Errorf("ReassignTask: reassignedBy is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "ReassignTask")
	if err != nil {
		return fmt.Errorf("ReassignTask: %w", err)
	}
	if err := AssertPermission(ctx, db, reassignedBy, resolvedTenantID, PermissionTaskReassign); err != nil {
		if errors.Is(err, ErrPermissionDenied) {
			return fmt.Errorf("ReassignTask: %w", ErrTaskReassignForbidden)
		}
		return fmt.Errorf("ReassignTask: %w", err)
	}

	var targetStatus string
	if err := db.GetContext(ctx, &targetStatus, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, resolvedTenantID, toUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ReassignTask: %w", ErrUserNotFound)
		}
		return fmt.Errorf("ReassignTask: load target user status: %w", err)
	}
	s := normalizeUserStatus(UserStatus(targetStatus))
	if s == UserStatusSuspended {
		return fmt.Errorf("ReassignTask: %w", ErrUserSuspended)
	}
	if s == UserStatusDeactivated {
		return fmt.Errorf("ReassignTask: %w", ErrUserDeactivated)
	}
	hasClaimPermission, err := UserHasPermission(ctx, db, toUserID, resolvedTenantID, PermissionTaskClaim)
	if err != nil {
		return fmt.Errorf("ReassignTask: check target TASK_CLAIM permission: %w", err)
	}
	if !hasClaimPermission {
		return fmt.Errorf("ReassignTask: %w", permissionDeniedError(PermissionTaskClaim))
	}

	tx, err := beginSQLXTx(ctx, db, "ReassignTask")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	type taskState struct {
		CaseID         string         `db:"case_id"`
		Status         string         `db:"status"`
		AssignedUserID sql.NullString `db:"assigned_user_id"`
		AssignedTeamID sql.NullString `db:"assigned_team_id"`
	}
	var state taskState
	if err := tx.GetContext(ctx, &state, `
		SELECT
			case_id::text AS case_id,
			status,
			assigned_user_id::text AS assigned_user_id,
			assigned_team_id::text AS assigned_team_id
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND id = $2::uuid
		FOR UPDATE
	`, resolvedTenantID, taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ReassignTask: %w", ErrTaskNotAssignable)
		}
		return fmt.Errorf("ReassignTask: lock task: %w", err)
	}

	if !strings.EqualFold(state.Status, "PENDING") &&
		!strings.EqualFold(state.Status, "ASSIGNED") &&
		!strings.EqualFold(state.Status, "IN_PROGRESS") &&
		!strings.EqualFold(state.Status, "AWAITING_EXTERNAL") {
		return fmt.Errorf("ReassignTask: %w", ErrTaskNotAssignable)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET assigned_user_id = $1::uuid,
		    status = 'IN_PROGRESS',
		    assigned_at = now(),
		    updated_at = now(),
		    version = version + 1
		WHERE tenant_id = $2::uuid
		  AND id = $3::uuid
	`, toUserID, resolvedTenantID, taskID); err != nil {
		return fmt.Errorf("ReassignTask: update task: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, &state.CaseID, &taskID, model.EventTaskReassigned, map[string]interface{}{
		"task_id":          taskID,
		"case_id":          state.CaseID,
		"tenant_id":        resolvedTenantID,
		"from_user_id":     nullableString(state.AssignedUserID),
		"to_user_id":       toUserID,
		"assigned_team_id": nullableString(state.AssignedTeamID),
		"reassigned_by":    reassignedBy,
		"reason":           reason,
		"occurred_at":      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("ReassignTask: publish TASK_REASSIGNED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ReassignTask: commit: %w", err)
	}

	logUserTeamInfo("task reassigned", "tenant_id", resolvedTenantID, "task_id", taskID, "to_user_id", toUserID, "reassigned_by", reassignedBy)
	return nil
}

func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := strings.TrimSpace(v.String)
	if s == "" {
		return nil
	}
	return &s
}
