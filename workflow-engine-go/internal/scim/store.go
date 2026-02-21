package scim

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	mt "workflow-engine/internal/multitenancy"

	"github.com/jmoiron/sqlx"
)

type scimUserRow struct {
	UserID       string         `db:"user_id"`
	TenantID     string         `db:"tenant_id"`
	Username     string         `db:"username"`
	Email        string         `db:"email"`
	DisplayName  string         `db:"display_name"`
	Status       string         `db:"status"`
	AuthProvider string         `db:"auth_provider"`
	ExternalID   sql.NullString `db:"external_id"`
	Locale       string         `db:"locale"`
	Timezone     string         `db:"timezone"`
	CreatedAt    time.Time      `db:"created_at"`
	UpdatedAt    time.Time      `db:"updated_at"`
	Version      int            `db:"version"`
}

type scimUserListRow struct {
	scimUserRow
	Roles       []string       `db:"roles"`
	PrimaryTeam sql.NullString `db:"primary_team_id"`
	TotalCount  int            `db:"total_count"`
}

type scimGroupMemberRow struct {
	TeamID      string         `db:"team_id"`
	TenantID    string         `db:"tenant_id"`
	ExternalID  sql.NullString `db:"external_id"`
	DisplayName string         `db:"display_name"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
	Version     int            `db:"version"`
	MemberID    sql.NullString `db:"member_id"`
	MemberName  sql.NullString `db:"member_name"`
}

type scimGroupListRow struct {
	TeamID      string         `db:"team_id"`
	TenantID    string         `db:"tenant_id"`
	ExternalID  sql.NullString `db:"external_id"`
	DisplayName string         `db:"display_name"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
	Version     int            `db:"version"`
	Members     []string       `db:"members"`
	TotalCount  int            `db:"total_count"`
}

func userLocation(baseURL string, userID string) string {
	return strings.TrimRight(baseURL, "/") + "/scim/v2/Users/" + strings.TrimSpace(userID)
}

func groupLocation(baseURL string, groupID string) string {
	return strings.TrimRight(baseURL, "/") + "/scim/v2/Groups/" + strings.TrimSpace(groupID)
}

func mapUserRowToSCIM(row scimUserRow, roles []string, primaryTeamID string, location string, includeExtension bool) SCIMUser {
	active := strings.EqualFold(strings.TrimSpace(row.Status), string(mt.UserStatusActive))
	schemas := []string{SchemaCoreUser}
	var ext *SCIMWorkflowUserExtension
	if includeExtension {
		schemas = append(schemas, SchemaWorkflowUserExtension)
		ext = &SCIMWorkflowUserExtension{
			TenantID:     row.TenantID,
			Roles:        roles,
			TeamID:       strings.TrimSpace(primaryTeamID),
			Timezone:     row.Timezone,
			AuthProvider: row.AuthProvider,
		}
	}
	user := SCIMUser{
		Schemas:     schemas,
		ID:          row.UserID,
		UserName:    row.Username,
		DisplayName: row.DisplayName,
		Emails: []SCIMEmail{
			{Value: row.Email, Primary: true, Type: "work"},
		},
		Active:   boolPtr(active),
		Locale:   row.Locale,
		Timezone: row.Timezone,
		Meta: SCIMMeta{
			ResourceType: "User",
			Created:      formatRFC3339UTC(row.CreatedAt),
			LastModified: formatRFC3339UTC(row.UpdatedAt),
			Location:     location,
			Version:      SCIMETag(row.Version),
		},
		WorkflowUserExtension: ext,
	}
	if row.ExternalID.Valid {
		user.ExternalID = strings.TrimSpace(row.ExternalID.String)
	}
	return user
}

func mapGroupRowsToSCIM(rows []scimGroupMemberRow, location string) (SCIMGroup, bool) {
	if len(rows) == 0 {
		return SCIMGroup{}, false
	}
	head := rows[0]
	members := make([]SCIMGroupMember, 0)
	for i := range rows {
		if !rows[i].MemberID.Valid {
			continue
		}
		memberID := strings.TrimSpace(rows[i].MemberID.String)
		if memberID == "" {
			continue
		}
		member := SCIMGroupMember{Value: memberID}
		if rows[i].MemberName.Valid {
			member.Display = strings.TrimSpace(rows[i].MemberName.String)
		}
		members = append(members, member)
	}
	sort.SliceStable(members, func(i, j int) bool {
		return members[i].Value < members[j].Value
	})
	group := SCIMGroup{
		Schemas:     []string{SchemaCoreGroup},
		ID:          head.TeamID,
		DisplayName: head.DisplayName,
		Members:     members,
		Meta: SCIMMeta{
			ResourceType: "Group",
			Created:      formatRFC3339UTC(head.CreatedAt),
			LastModified: formatRFC3339UTC(head.UpdatedAt),
			Location:     location,
			Version:      SCIMETag(head.Version),
		},
	}
	if head.ExternalID.Valid {
		group.ExternalID = strings.TrimSpace(head.ExternalID.String)
	}
	return group, true
}

func fetchUserRolesAndPrimaryTeam(ctx context.Context, db sqlx.ExtContext, tenantID, userID string) ([]string, string, error) {
	roles := make([]string, 0)
	if err := sqlx.SelectContext(ctx, db, &roles, `
		SELECT r.role_code
		FROM user_roles ur
		JOIN roles r
		  ON r.tenant_id = ur.tenant_id
		 AND r.role_id = ur.role_id
		WHERE ur.tenant_id = $1::uuid
		  AND ur.user_id = $2::uuid
		ORDER BY r.role_code ASC
	`, tenantID, userID); err != nil {
		return nil, "", fmt.Errorf("fetchUserRolesAndPrimaryTeam: query roles: %w", err)
	}

	var teamID sql.NullString
	if err := sqlx.GetContext(ctx, db, &teamID, `
		SELECT team_id::text
		FROM team_members
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
		ORDER BY
		  CASE role_in_team
		    WHEN 'MANAGER' THEN 1
		    WHEN 'LEAD' THEN 2
		    ELSE 3
		  END,
		  joined_at ASC
		LIMIT 1
	`, tenantID, userID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("fetchUserRolesAndPrimaryTeam: query team: %w", err)
	}
	return roles, strings.TrimSpace(teamID.String), nil
}

func getSCIMUserByID(ctx context.Context, db *sqlx.DB, tenantID, userID string, baseURL string, includeExtension bool) (SCIMUser, int, error) {
	var row scimUserRow
	if err := db.GetContext(ctx, &row, `
		SELECT
			u.user_id::text AS user_id,
			u.tenant_id::text AS tenant_id,
			u.username,
			u.email,
			u.display_name,
			u.status,
			u.auth_provider,
			u.external_id,
			u.locale,
			u.timezone,
			u.created_at,
			u.updated_at,
			u.version
		FROM users u
		WHERE u.tenant_id = $1::uuid
		  AND u.user_id = $2::uuid
	`, tenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SCIMUser{}, 0, fmt.Errorf("getSCIMUserByID: %w", mt.ErrUserNotFound)
		}
		return SCIMUser{}, 0, fmt.Errorf("getSCIMUserByID: query user: %w", err)
	}
	roles, primaryTeam, err := fetchUserRolesAndPrimaryTeam(ctx, db, tenantID, userID)
	if err != nil {
		return SCIMUser{}, 0, fmt.Errorf("getSCIMUserByID: %w", err)
	}
	location := userLocation(baseURL, row.UserID)
	return mapUserRowToSCIM(row, roles, primaryTeam, location, includeExtension), row.Version, nil
}

func getSCIMGroupByID(ctx context.Context, db *sqlx.DB, tenantID, teamID string, baseURL string) (SCIMGroup, int, error) {
	rows := make([]scimGroupMemberRow, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			t.team_id::text AS team_id,
			t.tenant_id::text AS tenant_id,
			t.external_id,
			t.display_name,
			t.created_at,
			t.updated_at,
			t.version,
			tm.user_id::text AS member_id,
			u.display_name AS member_name
		FROM teams t
		LEFT JOIN team_members tm
		  ON tm.tenant_id = t.tenant_id
		 AND tm.team_id = t.team_id
		LEFT JOIN users u
		  ON u.tenant_id = tm.tenant_id
		 AND u.user_id = tm.user_id
		WHERE t.tenant_id = $1::uuid
		  AND t.team_id = $2::uuid
		ORDER BY tm.joined_at ASC, u.display_name ASC
	`, tenantID, teamID); err != nil {
		return SCIMGroup{}, 0, fmt.Errorf("getSCIMGroupByID: query group: %w", err)
	}
	group, ok := mapGroupRowsToSCIM(rows, groupLocation(baseURL, teamID))
	if !ok {
		return SCIMGroup{}, 0, fmt.Errorf("getSCIMGroupByID: %w", mt.ErrTeamNotFound)
	}
	return group, rows[0].Version, nil
}

func tenantFromClaims(ctx context.Context) (SCIMTokenClaims, string, error) {
	claims, ok := SCIMClaimsFromContext(ctx)
	if !ok {
		return SCIMTokenClaims{}, "", fmt.Errorf("tenantFromClaims: missing claims")
	}
	tenantID, err := mt.TenantFromContext(ctx)
	if err != nil {
		return SCIMTokenClaims{}, "", fmt.Errorf("tenantFromClaims: %w", err)
	}
	if tenantID != claims.TenantID {
		return SCIMTokenClaims{}, "", fmt.Errorf("tenantFromClaims: tenant mismatch claims=%s context=%s", claims.TenantID, tenantID)
	}
	return claims, tenantID, nil
}
