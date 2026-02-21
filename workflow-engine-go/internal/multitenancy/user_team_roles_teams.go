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

// AssignRoleToUser grants a role to a user within the same tenant (idempotent).
func AssignRoleToUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	roleID string,
	tenantID string,
	assignedBy string,
) error {
	userID = strings.TrimSpace(userID)
	roleID = strings.TrimSpace(roleID)
	assignedBy = strings.TrimSpace(assignedBy)
	if userID == "" || roleID == "" {
		return fmt.Errorf("AssignRoleToUser: userID and roleID are required")
	}
	if assignedBy == "" {
		return fmt.Errorf("AssignRoleToUser: assignedBy is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "AssignRoleToUser")
	if err != nil {
		return fmt.Errorf("AssignRoleToUser: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "AssignRoleToUser")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var userExists bool
	if err := tx.GetContext(ctx, &userExists, `
		SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
		)
	`, resolvedTenantID, userID); err != nil {
		return fmt.Errorf("AssignRoleToUser: check user tenant: %w", err)
	}
	if !userExists {
		return fmt.Errorf("AssignRoleToUser: %w", ErrUserNotFound)
	}

	var roleTenantID string
	if err := tx.GetContext(ctx, &roleTenantID, `
		SELECT tenant_id::text
		FROM roles
		WHERE role_id = $1::uuid
	`, roleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AssignRoleToUser: %w", ErrRoleNotFound)
		}
		return fmt.Errorf("AssignRoleToUser: load role tenant: %w", err)
	}
	if roleTenantID != resolvedTenantID {
		return fmt.Errorf("AssignRoleToUser: %w", ErrRoleTenantMismatch)
	}

	tag, err := tx.ExecContext(ctx, `
		INSERT INTO user_roles (
			user_id,
			role_id,
			tenant_id,
			assigned_by,
			assigned_at
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3::uuid,
			$4::uuid,
			now()
		)
		ON CONFLICT (user_id, role_id) DO NOTHING
	`, userID, roleID, resolvedTenantID, assignedBy)
	if err != nil {
		return fmt.Errorf("AssignRoleToUser: insert assignment: %w", err)
	}

	rowsAffected, _ := tag.RowsAffected()
	if rowsAffected > 0 {
		if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventUserRoleAssigned, map[string]interface{}{
			"user_id":     userID,
			"role_id":     roleID,
			"tenant_id":   resolvedTenantID,
			"assigned_by": assignedBy,
			"occurred_at": time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("AssignRoleToUser: publish USER_ROLE_ASSIGNED: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AssignRoleToUser: commit: %w", err)
	}

	logUserTeamInfo("role assigned to user", "tenant_id", resolvedTenantID, "user_id", userID, "role_id", roleID, "assigned_by", assignedBy)
	return nil
}

// RevokeRoleFromUser revokes a role while preventing zero-role users.
func RevokeRoleFromUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	roleID string,
	tenantID string,
	revokedBy string,
) error {
	userID = strings.TrimSpace(userID)
	roleID = strings.TrimSpace(roleID)
	revokedBy = strings.TrimSpace(revokedBy)
	if userID == "" || roleID == "" {
		return fmt.Errorf("RevokeRoleFromUser: userID and roleID are required")
	}
	if revokedBy == "" {
		revokedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "RevokeRoleFromUser")
	if err != nil {
		return fmt.Errorf("RevokeRoleFromUser: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "RevokeRoleFromUser")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var roleTenantID string
	if err := tx.GetContext(ctx, &roleTenantID, `
		SELECT tenant_id::text
		FROM roles
		WHERE role_id = $1::uuid
	`, roleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("RevokeRoleFromUser: %w", ErrRoleNotFound)
		}
		return fmt.Errorf("RevokeRoleFromUser: load role tenant: %w", err)
	}
	if roleTenantID != resolvedTenantID {
		return fmt.Errorf("RevokeRoleFromUser: %w", ErrRoleTenantMismatch)
	}

	var assigned bool
	if err := tx.GetContext(ctx, &assigned, `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
			  AND role_id = $3::uuid
		)
	`, resolvedTenantID, userID, roleID); err != nil {
		return fmt.Errorf("RevokeRoleFromUser: check assignment: %w", err)
	}
	if !assigned {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("RevokeRoleFromUser: commit no-op: %w", err)
		}
		return nil
	}

	var roleCount int
	if err := tx.GetContext(ctx, &roleCount, `
		SELECT COUNT(*)::int
		FROM user_roles
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, resolvedTenantID, userID); err != nil {
		return fmt.Errorf("RevokeRoleFromUser: count user roles: %w", err)
	}
	if roleCount <= 1 {
		return fmt.Errorf("RevokeRoleFromUser: %w", ErrLastRoleRevocation)
	}

	tag, err := tx.ExecContext(ctx, `
		DELETE FROM user_roles
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
		  AND role_id = $3::uuid
	`, resolvedTenantID, userID, roleID)
	if err != nil {
		return fmt.Errorf("RevokeRoleFromUser: delete assignment: %w", err)
	}

	rowsAffected, _ := tag.RowsAffected()
	if rowsAffected > 0 {
		if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventUserRoleRevoked, map[string]interface{}{
			"user_id":     userID,
			"role_id":     roleID,
			"tenant_id":   resolvedTenantID,
			"revoked_by":  revokedBy,
			"occurred_at": time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("RevokeRoleFromUser: publish USER_ROLE_REVOKED: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RevokeRoleFromUser: commit: %w", err)
	}

	logUserTeamInfo("role revoked from user", "tenant_id", resolvedTenantID, "user_id", userID, "role_id", roleID, "revoked_by", revokedBy)
	return nil
}

// GetUserRoles returns all current role assignments for a user.
func GetUserRoles(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) ([]Role, error) {
	if db == nil {
		return nil, fmt.Errorf("GetUserRoles: db is nil")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("GetUserRoles: userID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetUserRoles")
	if err != nil {
		return nil, fmt.Errorf("GetUserRoles: %w", err)
	}

	roles := make([]Role, 0)
	if err := db.SelectContext(ctx, &roles, `
		SELECT
			r.role_id::text AS role_id,
			r.tenant_id::text AS tenant_id,
			r.role_code,
			r.display_name,
			r.description,
			r.is_system_role,
			r.permissions,
			r.created_at,
			r.updated_at
		FROM user_roles ur
		JOIN roles r
		  ON r.tenant_id = ur.tenant_id
		 AND r.role_id = ur.role_id
		WHERE ur.tenant_id = $1::uuid
		  AND ur.user_id = $2::uuid
		ORDER BY r.role_code ASC
	`, resolvedTenantID, userID); err != nil {
		return nil, fmt.Errorf("GetUserRoles: query roles: %w", err)
	}
	logUserTeamInfo("user roles fetched", "tenant_id", resolvedTenantID, "user_id", userID, "role_count", len(roles))
	return roles, nil
}

// CreateTeam creates a new team row and emits TEAM_CREATED.
func CreateTeam(
	ctx context.Context,
	db *sqlx.DB,
	input CreateTeamInput,
) (Team, error) {
	if err := validateCreateTeamInput(&input); err != nil {
		return Team{}, fmt.Errorf("CreateTeam: %w", err)
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, input.TenantID, "CreateTeam")
	if err != nil {
		return Team{}, fmt.Errorf("CreateTeam: %w", err)
	}
	input.TenantID = resolvedTenantID

	tx, err := beginSQLXTx(ctx, db, "CreateTeam")
	if err != nil {
		return Team{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if input.ParentTeamID != nil && strings.TrimSpace(*input.ParentTeamID) != "" {
		parentID := strings.TrimSpace(*input.ParentTeamID)
		var parentDepth int
		if err := tx.GetContext(ctx, &parentDepth, `
			WITH RECURSIVE lineage AS (
				SELECT team_id, parent_team_id, 1 AS depth
				FROM teams
				WHERE tenant_id = $1::uuid
				  AND team_id = $2::uuid
				UNION ALL
				SELECT t.team_id, t.parent_team_id, l.depth + 1
				FROM teams t
				JOIN lineage l ON t.team_id = l.parent_team_id
				WHERE t.tenant_id = $1::uuid
			)
			SELECT COALESCE(MAX(depth), 0)
			FROM lineage
		`, resolvedTenantID, parentID); err != nil {
			return Team{}, fmt.Errorf("CreateTeam: compute parent hierarchy depth: %w", err)
		}
		if parentDepth == 0 {
			return Team{}, fmt.Errorf("CreateTeam: %w", ErrTeamNotFound)
		}
		if parentDepth >= 3 {
			return Team{}, fmt.Errorf("CreateTeam: %w", ErrTeamHierarchyTooDeep)
		}
	}

	if input.ManagerUserID != nil && strings.TrimSpace(*input.ManagerUserID) != "" {
		managerID := strings.TrimSpace(*input.ManagerUserID)
		var status string
		if err := tx.GetContext(ctx, &status, `
			SELECT status
			FROM users
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
		`, resolvedTenantID, managerID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Team{}, fmt.Errorf("CreateTeam: manager user: %w", ErrUserNotFound)
			}
			return Team{}, fmt.Errorf("CreateTeam: load manager status: %w", err)
		}
		s := normalizeUserStatus(UserStatus(status))
		if s == UserStatusSuspended {
			return Team{}, fmt.Errorf("CreateTeam: manager user status invalid: %w", ErrUserSuspended)
		}
		if s == UserStatusDeactivated {
			return Team{}, fmt.Errorf("CreateTeam: manager user status invalid: %w", ErrUserDeactivated)
		}
	}

	var team Team
	err = tx.GetContext(ctx, &team, `
		INSERT INTO teams (
			tenant_id,
			team_code,
			display_name,
			team_type,
			parent_team_id,
			manager_user_id,
			status,
			metadata
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4,
			$5::uuid,
			$6::uuid,
			'ACTIVE',
			$7::jsonb
		)
		RETURNING
			team_id::text AS team_id,
			tenant_id::text AS tenant_id,
			team_code,
			display_name,
			team_type,
			parent_team_id::text AS parent_team_id,
			manager_user_id::text AS manager_user_id,
			status,
			metadata,
			created_at,
			updated_at
	`, resolvedTenantID, input.TeamCode, input.DisplayName, string(input.TeamType), input.ParentTeamID, input.ManagerUserID, normalizeJSON(input.Metadata))
	if err != nil {
		return Team{}, fmt.Errorf("CreateTeam: insert team: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventTeamCreated, map[string]interface{}{
		"team_id":     team.TeamID,
		"tenant_id":   resolvedTenantID,
		"team_code":   team.TeamCode,
		"created_by":  input.CreatedBy,
		"occurred_at": time.Now().UTC(),
	}); err != nil {
		return Team{}, fmt.Errorf("CreateTeam: publish TEAM_CREATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Team{}, fmt.Errorf("CreateTeam: commit: %w", err)
	}

	logUserTeamInfo("team created", "tenant_id", resolvedTenantID, "team_id", team.TeamID, "team_code", team.TeamCode)
	return team, nil
}

// AddUserToTeam adds or updates membership for a user in an ACTIVE team.
func AddUserToTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	userID string,
	tenantID string,
	roleInTeam TeamMemberRole,
	addedBy string,
) error {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	addedBy = strings.TrimSpace(addedBy)
	roleInTeam = normalizeTeamMemberRole(roleInTeam)
	if teamID == "" || userID == "" {
		return fmt.Errorf("AddUserToTeam: teamID and userID are required")
	}
	if roleInTeam == "" {
		return fmt.Errorf("AddUserToTeam: roleInTeam is required")
	}
	if addedBy == "" {
		return fmt.Errorf("AddUserToTeam: addedBy is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "AddUserToTeam")
	if err != nil {
		return fmt.Errorf("AddUserToTeam: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "AddUserToTeam")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var teamStatus string
	var teamTenantID string
	if err := tx.QueryRowxContext(ctx, `
		SELECT status, tenant_id::text AS tenant_id
		FROM teams
		WHERE team_id = $1::uuid
		FOR UPDATE
	`, teamID).Scan(&teamStatus, &teamTenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("AddUserToTeam: %w", ErrTeamNotFound)
		}
		return fmt.Errorf("AddUserToTeam: lock team: %w", err)
	}
	if teamTenantID != resolvedTenantID {
		return fmt.Errorf("AddUserToTeam: %w", ErrTeamTenantMismatch)
	}
	if normalizeTeamStatus(TeamStatus(teamStatus)) == TeamStatusDisbanded {
		return fmt.Errorf("AddUserToTeam: %w", ErrTeamDisbanded)
	}

	var userExists bool
	if err := tx.GetContext(ctx, &userExists, `
		SELECT EXISTS (
			SELECT 1
			FROM users
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
		)
	`, resolvedTenantID, userID); err != nil {
		return fmt.Errorf("AddUserToTeam: check user tenant: %w", err)
	}
	if !userExists {
		return fmt.Errorf("AddUserToTeam: %w", ErrUserNotFound)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO team_members (
			team_id,
			user_id,
			tenant_id,
			role_in_team,
			joined_at,
			added_by
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3::uuid,
			$4,
			now(),
			$5::uuid
		)
		ON CONFLICT (team_id, user_id)
		DO UPDATE
		SET role_in_team = EXCLUDED.role_in_team,
		    added_by = EXCLUDED.added_by
	`, teamID, userID, resolvedTenantID, string(roleInTeam), addedBy); err != nil {
		return fmt.Errorf("AddUserToTeam: upsert membership: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventTeamMemberAdded, map[string]interface{}{
		"team_id":      teamID,
		"user_id":      userID,
		"tenant_id":    resolvedTenantID,
		"role_in_team": roleInTeam,
		"added_by":     addedBy,
		"occurred_at":  time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("AddUserToTeam: publish TEAM_MEMBER_ADDED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AddUserToTeam: commit: %w", err)
	}

	logUserTeamInfo("user added to team", "tenant_id", resolvedTenantID, "team_id", teamID, "user_id", userID, "role_in_team", roleInTeam)
	return nil
}

// RemoveUserFromTeam removes membership while ensuring team remains non-empty by MEMBER/LEAD semantics.
func RemoveUserFromTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	userID string,
	tenantID string,
	removedBy string,
) error {
	teamID = strings.TrimSpace(teamID)
	userID = strings.TrimSpace(userID)
	removedBy = strings.TrimSpace(removedBy)
	if teamID == "" || userID == "" {
		return fmt.Errorf("RemoveUserFromTeam: teamID and userID are required")
	}
	if removedBy == "" {
		removedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "RemoveUserFromTeam")
	if err != nil {
		return fmt.Errorf("RemoveUserFromTeam: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "RemoveUserFromTeam")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var teamTenantID string
	if err := tx.GetContext(ctx, &teamTenantID, `
		SELECT tenant_id::text
		FROM teams
		WHERE team_id = $1::uuid
		FOR UPDATE
	`, teamID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("RemoveUserFromTeam: %w", ErrTeamNotFound)
		}
		return fmt.Errorf("RemoveUserFromTeam: lock team: %w", err)
	}
	if teamTenantID != resolvedTenantID {
		return fmt.Errorf("RemoveUserFromTeam: %w", ErrTeamTenantMismatch)
	}

	var roleInTeam string
	if err := tx.GetContext(ctx, &roleInTeam, `
		SELECT role_in_team
		FROM team_members
		WHERE tenant_id = $1::uuid
		  AND team_id = $2::uuid
		  AND user_id = $3::uuid
	`, resolvedTenantID, teamID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("RemoveUserFromTeam: commit no-op: %w", err)
			}
			return nil
		}
		return fmt.Errorf("RemoveUserFromTeam: load membership: %w", err)
	}

	if strings.EqualFold(roleInTeam, string(TeamMemberRoleMember)) || strings.EqualFold(roleInTeam, string(TeamMemberRoleLead)) {
		var remaining int
		if err := tx.GetContext(ctx, &remaining, `
			SELECT COUNT(*)::int
			FROM team_members
			WHERE tenant_id = $1::uuid
			  AND team_id = $2::uuid
			  AND user_id <> $3::uuid
			  AND role_in_team IN ('MEMBER', 'LEAD')
		`, resolvedTenantID, teamID, userID); err != nil {
			return fmt.Errorf("RemoveUserFromTeam: count remaining core members: %w", err)
		}
		if remaining == 0 {
			return fmt.Errorf("RemoveUserFromTeam: %w", ErrTeamWouldBeEmpty)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM team_members
		WHERE tenant_id = $1::uuid
		  AND team_id = $2::uuid
		  AND user_id = $3::uuid
	`, resolvedTenantID, teamID, userID); err != nil {
		return fmt.Errorf("RemoveUserFromTeam: delete membership: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventTeamMemberRemoved, map[string]interface{}{
		"team_id":     teamID,
		"user_id":     userID,
		"tenant_id":   resolvedTenantID,
		"removed_by":  removedBy,
		"occurred_at": time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("RemoveUserFromTeam: publish TEAM_MEMBER_REMOVED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RemoveUserFromTeam: commit: %w", err)
	}

	logUserTeamInfo("user removed from team", "tenant_id", resolvedTenantID, "team_id", teamID, "user_id", userID, "removed_by", removedBy)
	return nil
}

// DisbandTeam marks a team DISBANDED only when no open tasks are assigned to it.
func DisbandTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
	disbandedBy string,
) error {
	teamID = strings.TrimSpace(teamID)
	disbandedBy = strings.TrimSpace(disbandedBy)
	if teamID == "" {
		return fmt.Errorf("DisbandTeam: teamID is required")
	}
	if disbandedBy == "" {
		disbandedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "DisbandTeam")
	if err != nil {
		return fmt.Errorf("DisbandTeam: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "DisbandTeam")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var status string
	var teamTenantID string
	if err := tx.QueryRowxContext(ctx, `
		SELECT status, tenant_id::text AS tenant_id
		FROM teams
		WHERE team_id = $1::uuid
		FOR UPDATE
	`, teamID).Scan(&status, &teamTenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("DisbandTeam: %w", ErrTeamNotFound)
		}
		return fmt.Errorf("DisbandTeam: lock team: %w", err)
	}
	if teamTenantID != resolvedTenantID {
		return fmt.Errorf("DisbandTeam: %w", ErrTeamTenantMismatch)
	}

	var openTaskCount int
	if err := tx.GetContext(ctx, &openTaskCount, `
		SELECT COUNT(*)::int
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND assigned_team_id = $2::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, resolvedTenantID, teamID); err != nil {
		return fmt.Errorf("DisbandTeam: count open tasks: %w", err)
	}
	if openTaskCount > 0 {
		return fmt.Errorf("DisbandTeam: %w", teamHasOpenTasksError(openTaskCount))
	}

	if normalizeTeamStatus(TeamStatus(status)) != TeamStatusDisbanded {
		if _, err := tx.ExecContext(ctx, `
			UPDATE teams
			SET status = 'DISBANDED',
			    updated_at = now()
			WHERE tenant_id = $1::uuid
			  AND team_id = $2::uuid
		`, resolvedTenantID, teamID); err != nil {
			return fmt.Errorf("DisbandTeam: update status: %w", err)
		}
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventTeamDisbanded, map[string]interface{}{
		"team_id":      teamID,
		"tenant_id":    resolvedTenantID,
		"disbanded_by": disbandedBy,
		"occurred_at":  time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("DisbandTeam: publish TEAM_DISBANDED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DisbandTeam: commit: %w", err)
	}

	logUserTeamInfo("team disbanded", "tenant_id", resolvedTenantID, "team_id", teamID, "disbanded_by", disbandedBy)
	return nil
}

// ListTeams returns paginated team list.
func ListTeams(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filters ListTeamsFilters,
	page, size int,
) ([]Team, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListTeams: db is nil")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "ListTeams")
	if err != nil {
		return nil, 0, fmt.Errorf("ListTeams: %w", err)
	}
	page, size = sanitizePagination(page, size)
	offset := (page - 1) * size

	args := []interface{}{resolvedTenantID}
	where := []string{"t.tenant_id = $1::uuid"}
	if filters.Status != nil && normalizeTeamStatus(*filters.Status) != "" {
		args = append(args, string(normalizeTeamStatus(*filters.Status)))
		where = append(where, fmt.Sprintf("t.status = $%d", len(args)))
	}
	if filters.TeamType != nil && normalizeTeamType(*filters.TeamType) != "" {
		args = append(args, string(normalizeTeamType(*filters.TeamType)))
		where = append(where, fmt.Sprintf("t.team_type = $%d", len(args)))
	}
	if strings.TrimSpace(filters.ParentTeamID) != "" {
		args = append(args, strings.TrimSpace(filters.ParentTeamID))
		where = append(where, fmt.Sprintf("t.parent_team_id = $%d::uuid", len(args)))
	}

	args = append(args, size, offset)
	query := fmt.Sprintf(`
		SELECT
			t.team_id::text AS team_id,
			t.tenant_id::text AS tenant_id,
			t.team_code,
			t.display_name,
			t.team_type,
			t.parent_team_id::text AS parent_team_id,
			t.manager_user_id::text AS manager_user_id,
			t.status,
			t.metadata,
			t.created_at,
			t.updated_at,
			COUNT(*) OVER() AS total_count
		FROM teams t
		WHERE %s
		ORDER BY t.display_name ASC, t.created_at DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), len(args)-1, len(args))

	type row struct {
		Team
		TotalCount int `db:"total_count"`
	}
	result := make([]row, 0)
	if err := db.SelectContext(ctx, &result, query, args...); err != nil {
		return nil, 0, fmt.Errorf("ListTeams: query teams: %w", err)
	}

	teams := make([]Team, 0, len(result))
	total := 0
	for i := range result {
		if i == 0 {
			total = result[i].TotalCount
		}
		teams = append(teams, result[i].Team)
	}
	logUserTeamInfo("teams listed", "tenant_id", resolvedTenantID, "count", len(teams), "total", total, "page", page, "size", size)
	return teams, total, nil
}

// GetTeamMembers returns paginated membership rows for a team.
func GetTeamMembers(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
	page, size int,
) ([]TeamMember, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetTeamMembers: db is nil")
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return nil, 0, fmt.Errorf("GetTeamMembers: teamID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetTeamMembers")
	if err != nil {
		return nil, 0, fmt.Errorf("GetTeamMembers: %w", err)
	}
	page, size = sanitizePagination(page, size)
	offset := (page - 1) * size

	var teamExists bool
	if err := db.GetContext(ctx, &teamExists, `
		SELECT EXISTS (
			SELECT 1
			FROM teams
			WHERE tenant_id = $1::uuid
			  AND team_id = $2::uuid
		)
	`, resolvedTenantID, teamID); err != nil {
		return nil, 0, fmt.Errorf("GetTeamMembers: check team existence: %w", err)
	}
	if !teamExists {
		return nil, 0, fmt.Errorf("GetTeamMembers: %w", ErrTeamNotFound)
	}

	type row struct {
		TeamMember
		TotalCount int `db:"total_count"`
	}
	result := make([]row, 0)
	if err := db.SelectContext(ctx, &result, `
		SELECT
			tm.team_id::text AS team_id,
			tm.user_id::text AS user_id,
			tm.tenant_id::text AS tenant_id,
			tm.role_in_team,
			tm.joined_at,
			tm.added_by::text AS added_by,
			u.display_name,
			u.username,
			u.email,
			COUNT(*) OVER() AS total_count
		FROM team_members tm
		JOIN users u
		  ON u.tenant_id = tm.tenant_id
		 AND u.user_id = tm.user_id
		WHERE tm.tenant_id = $1::uuid
		  AND tm.team_id = $2::uuid
		ORDER BY tm.joined_at ASC, u.display_name ASC
		LIMIT $3 OFFSET $4
	`, resolvedTenantID, teamID, size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetTeamMembers: query members: %w", err)
	}

	members := make([]TeamMember, 0, len(result))
	total := 0
	for i := range result {
		if i == 0 {
			total = result[i].TotalCount
		}
		members = append(members, result[i].TeamMember)
	}
	logUserTeamInfo("team members listed", "tenant_id", resolvedTenantID, "team_id", teamID, "count", len(members), "total", total, "page", page, "size", size)
	return members, total, nil
}
