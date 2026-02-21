package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// ---------------------------------------------------------------------------
// 2. Extensible Team Management
// ---------------------------------------------------------------------------

func (s *IdentityService) CreateTeam(
	ctx context.Context,
	db *sqlx.DB,
	input model.CreateTeamInput,
) (model.Team, error) {
	if db == nil {
		return model.Team{}, fmt.Errorf("CreateTeam: db is nil")
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return model.Team{}, fmt.Errorf("CreateTeam: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Validate depth if parent is provided
	if input.ParentTeamID != nil {
		depth, err := s.calculateTeamDepth(ctx, tx, *input.ParentTeamID, input.TenantID)
		if err != nil {
			return model.Team{}, fmt.Errorf("CreateTeam: check depth: %w", err)
		}
		// Max depth is 3. If parent is already depth 3, we cannot append.
		if depth >= 3 {
			return model.Team{}, model.ErrTeamHierarchyTooDeep
		}
	}

	meta := input.Metadata
	if meta == nil {
		meta = []byte("{}")
	}

	var team model.Team
	err = tx.QueryRowxContext(ctx, `
		INSERT INTO teams (
			tenant_id,
			team_code,
			display_name,
			team_type,
			parent_team_id,
			manager_user_id,
			status,
			metadata
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, 'ACTIVE', $7::jsonb
		)
		RETURNING
			team_id, tenant_id, team_code, display_name, team_type,
			parent_team_id, manager_user_id, status, metadata,
			created_at, updated_at
	`,
		input.TenantID,
		input.TeamCode,
		input.DisplayName,
		input.TeamType,
		input.ParentTeamID,
		input.ManagerUserID,
		meta,
	).StructScan(&team)

	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return model.Team{}, fmt.Errorf("CreateTeam: team_code already in use")
		}
		return model.Team{}, fmt.Errorf("CreateTeam: insert: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"team_id":   team.TeamID,
		"team_code": team.TeamCode,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  team.TenantID,
		EventType: "TEAM_CREATED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return model.Team{}, fmt.Errorf("CreateTeam: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Team{}, fmt.Errorf("CreateTeam: commit tx: %w", err)
	}

	s.logger.Info("team created", "tenant_id", team.TenantID, "team_id", team.TeamID)
	return team, nil
}

func (s *IdentityService) AddUserToTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	userID string,
	tenantID string,
	roleInTeam model.TeamMemberRole,
	addedBy string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("AddUserToTeam: begin tx: %w", err)
	}
	defer tx.Rollback()

	var teamStatus string
	err = tx.GetContext(ctx, &teamStatus, `SELECT status FROM teams WHERE team_id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE`, teamID, tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrTeamNotFound
		}
		return fmt.Errorf("AddUserToTeam: check team: %w", err)
	}
	if teamStatus != string(model.TeamStatusActive) {
		return model.ErrTeamDisbanded
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO team_members (
			team_id, user_id, tenant_id, role_in_team, added_by
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4, $5::uuid
		) ON CONFLICT (team_id, user_id) DO UPDATE
		SET role_in_team = EXCLUDED.role_in_team
	`, teamID, userID, tenantID, roleInTeam, addedBy)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23503" { // foreign key violation
			return fmt.Errorf("AddUserToTeam: invalid user or team reference: %w", err)
		}
		return fmt.Errorf("AddUserToTeam: upsert: %w", err)
	}

	evtPayload, _ := json.Marshal(map[string]interface{}{
		"team_id":      teamID,
		"user_id":      userID,
		"role_in_team": roleInTeam,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "TEAM_MEMBER_ADDED",
		Payload:   evtPayload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("AddUserToTeam: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AddUserToTeam: commit tx: %w", err)
	}

	s.logger.Info("user added to team", "tenant_id", tenantID, "team_id", teamID, "user_id", userID)
	return nil
}

func (s *IdentityService) RemoveUserFromTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	userID string,
	tenantID string,
	removedBy string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("RemoveUserFromTeam: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check current membership count to prevent empty teams
	var remainingCount int
	err = tx.GetContext(ctx, &remainingCount, `
		SELECT count(*) FROM team_members
		WHERE team_id = $1::uuid AND tenant_id = $2::uuid AND user_id != $3::uuid
	`, teamID, tenantID, userID)
	if err != nil {
		return fmt.Errorf("RemoveUserFromTeam: count members: %w", err)
	}

	if remainingCount == 0 {
		return model.ErrTeamWouldBeEmpty
	}

	res, err := tx.ExecContext(ctx, `
		DELETE FROM team_members
		WHERE team_id = $1::uuid AND user_id = $2::uuid AND tenant_id = $3::uuid
	`, teamID, userID, tenantID)
	if err != nil {
		return fmt.Errorf("RemoveUserFromTeam: delete: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil // idempotent success if they weren't in the team
	}

	evtPayload, _ := json.Marshal(map[string]interface{}{
		"team_id":    teamID,
		"user_id":    userID,
		"removed_by": removedBy,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "TEAM_MEMBER_REMOVED",
		Payload:   evtPayload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("RemoveUserFromTeam: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RemoveUserFromTeam: commit tx: %w", err)
	}

	s.logger.Info("user removed from team", "tenant_id", tenantID, "team_id", teamID, "user_id", userID)
	return nil
}

func (s *IdentityService) DisbandTeam(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
	disbandedBy string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("DisbandTeam: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Are there any open tasks assigned to this team?
	var openTasks int
	err = tx.GetContext(ctx, &openTasks, `
		SELECT count(*) FROM tasks
		WHERE tenant_id = $1::uuid
		  AND assigned_team_id = $2::uuid
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
	`, tenantID, teamID)
	if err != nil {
		return fmt.Errorf("DisbandTeam: count open tasks: %w", err)
	}

	if openTasks > 0 {
		return &model.ErrTeamHasOpenTasks{Count: openTasks}
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE teams
		SET status = 'DISBANDED', updated_at = now()
		WHERE team_id = $1::uuid AND tenant_id = $2::uuid AND status = 'ACTIVE'
	`, teamID, tenantID)
	if err != nil {
		return fmt.Errorf("DisbandTeam: update status: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return model.ErrTeamDisbanded // or not found
	}

	evtPayload, _ := json.Marshal(map[string]interface{}{
		"team_id":      teamID,
		"disbanded_by": disbandedBy,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "TEAM_DISBANDED",
		Payload:   evtPayload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("DisbandTeam: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DisbandTeam: commit tx: %w", err)
	}

	s.logger.Info("team disbanded", "tenant_id", tenantID, "team_id", teamID)
	return nil
}

// ---------------------------------------------------------------------------
// Internal calculations
// ---------------------------------------------------------------------------

func (s *IdentityService) calculateTeamDepth(ctx context.Context, tx *sqlx.Tx, parentID string, tenantID string) (int, error) {
	// Simple recursive query wrapper or iterated scalar resolution max=3
	var depth int = 1
	currentParent := &parentID

	for i := 0; i < 4; i++ {
		var nextParent *string
		err := tx.GetContext(ctx, &nextParent, `
			SELECT parent_team_id FROM teams WHERE team_id = $1::uuid AND tenant_id = $2::uuid
		`, *currentParent, tenantID)
		if err != nil {
			if err == sql.ErrNoRows {
				return 0, model.ErrTeamNotFound
			}
			return 0, err
		}

		if nextParent == nil {
			return depth, nil
		}
		currentParent = nextParent
		depth++

		if depth >= 3 {
			break
		}
	}

	return depth, nil
}
