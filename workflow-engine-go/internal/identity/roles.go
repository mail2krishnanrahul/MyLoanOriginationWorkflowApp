package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ---------------------------------------------------------------------------
// 3. Role Assignment
// ---------------------------------------------------------------------------

func (s *IdentityService) AssignRoleToUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	roleID string,
	tenantID string,
	assignedBy string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("AssignRoleToUser: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Verify both user and role exist in tenant
	var count int
	err = tx.GetContext(ctx, &count, `
		SELECT count(*) FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid
	`, userID, tenantID)
	if err != nil || count == 0 {
		return model.ErrUserNotFound
	}

	err = tx.GetContext(ctx, &count, `
		SELECT count(*) FROM roles WHERE role_id = $1::uuid AND tenant_id = $2::uuid
	`, roleID, tenantID)
	if err != nil {
		return fmt.Errorf("AssignRoleToUser: check role: %w", err)
	}
	if count == 0 {
		return model.ErrRoleNotFound
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_roles (
			user_id, role_id, tenant_id, assigned_by
		) VALUES (
			$1::uuid, $2::uuid, $3::uuid, $4::uuid
		) ON CONFLICT DO NOTHING
	`, userID, roleID, tenantID, assignedBy)
	if err != nil {
		return fmt.Errorf("AssignRoleToUser: insert: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":     userID,
		"role_id":     roleID,
		"assigned_by": assignedBy,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "USER_ROLE_ASSIGNED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("AssignRoleToUser: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("AssignRoleToUser: commit tx: %w", err)
	}

	return nil
}

func (s *IdentityService) RevokeRoleFromUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	roleID string,
	tenantID string,
	revokedBy string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("RevokeRoleFromUser: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check remaining robust count
	var remainingRoles int
	err = tx.GetContext(ctx, &remainingRoles, `
		SELECT count(*) FROM user_roles
		WHERE user_id = $1::uuid AND tenant_id = $2::uuid AND role_id != $3::uuid
	`, userID, tenantID, roleID)
	if err != nil {
		return fmt.Errorf("RevokeRoleFromUser: count roles: %w", err)
	}

	if remainingRoles == 0 {
		return model.ErrLastRoleRevocation
	}

	res, err := tx.ExecContext(ctx, `
		DELETE FROM user_roles
		WHERE user_id = $1::uuid AND role_id = $2::uuid AND tenant_id = $3::uuid
	`, userID, roleID, tenantID)
	if err != nil {
		return fmt.Errorf("RevokeRoleFromUser: delete: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil // idempotent success if they didn't have the role
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":    userID,
		"role_id":    roleID,
		"revoked_by": revokedBy,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "USER_ROLE_REVOKED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("RevokeRoleFromUser: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RevokeRoleFromUser: commit tx: %w", err)
	}

	return nil
}

func (s *IdentityService) GetUserRoles(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) ([]model.Role, error) {
	var roles []model.Role

	// Custom struct scanner required since pq.StringArray is needed for postgres text[] -> Go []string
	rows, err := db.QueryxContext(ctx, `
		SELECT r.role_id, r.tenant_id, r.role_code, r.display_name, r.description,
		       r.is_system_role, r.permissions, r.created_at, r.updated_at
		FROM roles r
		JOIN user_roles ur ON r.role_id = ur.role_id
		WHERE ur.user_id = $1::uuid AND ur.tenant_id = $2::uuid
	`, userID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("GetUserRoles: query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r model.Role
		var pgPermissions string // Array string `{PERM1,PERM2}` due to how sqlx maps to interface{}
		err := rows.Scan(
			&r.RoleID, &r.TenantID, &r.RoleCode, &r.DisplayName, &r.Description,
			&r.IsSystemRole, &pgPermissions, &r.CreatedAt, &r.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("GetUserRoles: scan: %w", err)
		}
		pgPermissions = strings.Trim(pgPermissions, "{}")
		if len(pgPermissions) > 0 {
			r.Permissions = strings.Split(pgPermissions, ",")
		} else {
			r.Permissions = []string{}
		}
		roles = append(roles, r)
	}

	return roles, nil
}
