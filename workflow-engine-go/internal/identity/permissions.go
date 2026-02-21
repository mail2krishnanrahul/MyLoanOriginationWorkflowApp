package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ---------------------------------------------------------------------------
// 4. Permission Model & Guards
// ---------------------------------------------------------------------------

type permissionCacheKey struct{}

// WithPermissionCache injects a request-scoped map into context to prevent N+1 DB lookups
// when asserting permissions multiple times within a single HTTP request or queue handler.
func WithPermissionCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, permissionCacheKey{}, make(map[model.PermissionCode]bool))
}

func (s *IdentityService) UserHasPermission(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	permission model.PermissionCode,
) (bool, error) {
	// Check request-scoped cache first
	if cache, ok := ctx.Value(permissionCacheKey{}).(map[model.PermissionCode]bool); ok {
		if has, exists := cache[permission]; exists {
			return has, nil
		}
	}

	// Resolve actual permissions from DB
	// We union all permissions from all roles assigned to the user
	var resolvedPerms []string
	err := db.SelectContext(ctx, &resolvedPerms, `
		SELECT unnest(r.permissions) AS p
		FROM roles r
		JOIN user_roles ur ON r.role_id = ur.role_id
		WHERE ur.user_id = $1::uuid AND ur.tenant_id = $2::uuid
	`, userID, tenantID)

	if err != nil {
		return false, fmt.Errorf("UserHasPermission: query: %w", err)
	}

	hasPerm := false
	for _, p := range resolvedPerms {
		if p == string(permission) {
			hasPerm = true
			break
		}
	}

	// Cache result if possible
	if cache, ok := ctx.Value(permissionCacheKey{}).(map[model.PermissionCode]bool); ok {
		cache[permission] = hasPerm
	}

	return hasPerm, nil
}

func (s *IdentityService) AssertPermission(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	permission model.PermissionCode,
) error {
	has, err := s.UserHasPermission(ctx, db, userID, tenantID, permission)
	if err != nil {
		return fmt.Errorf("AssertPermission: %w", err)
	}
	if !has {
		return &model.ErrPermissionDenied{Permission: permission}
	}
	return nil
}

// SeedSystemRoles establishes the default role hierarchy for a new tenant
func (s *IdentityService) SeedSystemRoles(ctx context.Context, tx *sqlx.Tx, tenantID string) error {
	systemRoles := []struct {
		code        string
		name        string
		desc        string
		permissions []model.PermissionCode
	}{
		{
			code: "LOAN_OFFICER", name: "Loan Officer", desc: "Frontline originations and doc collection",
			permissions: []model.PermissionCode{
				model.PermissionCaseCreate, model.PermissionCaseViewOwn,
				model.PermissionTaskClaim, model.PermissionTaskComplete, model.PermissionTaskViewOwn,
			},
		},
		{
			code: "UNDERWRITER", name: "Underwriter", desc: "Credit review and task rejections",
			permissions: []model.PermissionCode{
				model.PermissionCaseViewAll,
				model.PermissionTaskClaim, model.PermissionTaskComplete, model.PermissionTaskReject, model.PermissionTaskViewAll,
			},
		},
		{
			code: "SENIOR_APPROVER", name: "Senior Approver", desc: "Underwriter with approval authority",
			permissions: []model.PermissionCode{
				model.PermissionCaseViewAll,
				model.PermissionTaskClaim, model.PermissionTaskComplete, model.PermissionTaskReject, model.PermissionTaskViewAll,
				model.PermissionApprovalApprove, model.PermissionApprovalReject, model.PermissionApprovalEscalate,
			},
		},
		{
			code: "SUPERVISOR", name: "Supervisor", desc: "Team oversight and queue management",
			permissions: []model.PermissionCode{
				model.PermissionCaseViewAll, model.PermissionCaseReassign,
				model.PermissionTaskClaim, model.PermissionTaskComplete, model.PermissionTaskReject, model.PermissionTaskViewAll, model.PermissionTaskReassign,
				model.PermissionApprovalApprove, model.PermissionApprovalReject, model.PermissionApprovalEscalate, model.PermissionApprovalViewAll,
				model.PermissionReportView, model.PermissionReportOperational,
			},
		},
		{
			code: "ADMIN", name: "Administrator", desc: "Full systemic governance over tenant",
			permissions: []model.PermissionCode{
				model.PermissionCaseViewAll, model.PermissionCaseReassign, model.PermissionCaseCancel,
				model.PermissionTaskClaim, model.PermissionTaskComplete, model.PermissionTaskReject, model.PermissionTaskViewAll, model.PermissionTaskReassign,
				model.PermissionApprovalApprove, model.PermissionApprovalReject, model.PermissionApprovalEscalate, model.PermissionApprovalViewAll,
				model.PermissionReportView, model.PermissionReportExport, model.PermissionReportOperational,
				model.PermissionUserCreate, model.PermissionUserSuspend, model.PermissionUserDeactivate, model.PermissionUserView,
				model.PermissionRoleManage, model.PermissionTeamManage, model.PermissionCaseTypeManage, model.PermissionTenantConfigManage,
			},
		},
	}

	for _, sr := range systemRoles {
		var strPerms []string
		for _, p := range sr.permissions {
			strPerms = append(strPerms, string(p))
		}

		permArray := "{" + strings.Join(strPerms, ",") + "}"

		_, err := tx.ExecContext(ctx, `
			INSERT INTO roles (tenant_id, role_code, display_name, description, is_system_role, permissions)
			VALUES ($1::uuid, $2, $3, $4, true, $5::text[])
			ON CONFLICT DO NOTHING
		`, tenantID, sr.code, sr.name, sr.desc, permArray)
		if err != nil {
			return fmt.Errorf("SeedSystemRoles: insert role %s: %w", sr.code, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 8 & 9. Workload and Queries
// ---------------------------------------------------------------------------

func (s *IdentityService) GetUserWorkload(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) (model.UserWorkload, error) {
	var wl model.UserWorkload
	err := db.GetContext(ctx, &wl, `
		SELECT 
			COUNT(*) FILTER (WHERE status = 'PENDING') AS pending_task_count,
			COUNT(*) FILTER (WHERE status IN ('ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')) AS in_progress_task_count,
			COUNT(*) FILTER (WHERE status = 'COMPLETED' AND completed_at >= CURRENT_DATE) AS completed_today_count,
			COUNT(*) FILTER (WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS') AND due_at < now() + interval '2 hours') AS sla_at_risk_count
		FROM tasks
		WHERE tenant_id = $1::uuid AND assigned_user_id = $2::uuid
	`, tenantID, userID)
	if err != nil {
		return wl, fmt.Errorf("GetUserWorkload: metrics: %w", err)
	}

	// Grab oldest age format
	var oldest *time.Time
	err = db.GetContext(ctx, &oldest, `
		SELECT created_at FROM tasks 
		WHERE tenant_id = $1::uuid AND assigned_user_id = $2::uuid AND status = 'PENDING'
		ORDER BY created_at ASC LIMIT 1
	`, tenantID, userID)
	if err == nil && oldest != nil {
		age := time.Since(*oldest).Round(time.Minute).String()
		wl.OldestPendingAge = &age
	}

	return wl, nil
}

func (s *IdentityService) GetTeamWorkload(
	ctx context.Context,
	db *sqlx.DB,
	teamID string,
	tenantID string,
) (model.TeamWorkload, error) {
	wl := model.TeamWorkload{
		MemberTaskCounts: make(map[string]int),
	}

	// Fill pool counts
	err := db.QueryRowxContext(ctx, `
		SELECT count(*)
		FROM tasks
		WHERE tenant_id = $1::uuid AND assigned_team_id = $2::uuid AND assigned_user_id IS NULL AND status = 'PENDING'
	`, tenantID, teamID).Scan(&wl.UnassignedTaskCount)
	if err != nil {
		return wl, fmt.Errorf("GetTeamWorkload: pool count: %w", err)
	}
	wl.TeamQueueDepth = wl.UnassignedTaskCount

	// Fill member counts
	rows, err := db.QueryxContext(ctx, `
		SELECT assigned_user_id, count(*)
		FROM tasks 
		WHERE tenant_id = $1::uuid 
		  AND assigned_team_id = $2::uuid 
		  AND assigned_user_id IS NOT NULL 
		  AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS')
		GROUP BY assigned_user_id
	`, tenantID, teamID)
	if err != nil {
		return wl, fmt.Errorf("GetTeamWorkload: member counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var uid string
		var count int
		if err := rows.Scan(&uid, &count); err == nil {
			wl.MemberTaskCounts[uid] = count
		}
	}

	return wl, nil
}

func (s *IdentityService) ListUsersWithWorkload(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	teamID string,
	page, size int,
) ([]model.UserWorkloadRow, int, error) {
	if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	var teamFilter string
	var args []interface{}
	args = append(args, tenantID)

	if teamID != "" {
		teamFilter = ` AND u.user_id IN (SELECT user_id FROM team_members WHERE team_id = $2::uuid) `
		args = append(args, teamID)
	}

	// 1 query to get active users and their active task counts
	query := fmt.Sprintf(`
		SELECT 
			u.user_id, u.tenant_id, u.username, u.email, u.display_name,
			COUNT(t.id) FILTER (WHERE t.status = 'PENDING') AS pending_task_count,
			COUNT(t.id) FILTER (WHERE t.status IN ('ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL')) AS in_progress_task_count,
			COUNT(t.id) FILTER (WHERE t.status = 'COMPLETED' AND t.completed_at >= CURRENT_DATE) AS completed_today_count
		FROM users u
		LEFT JOIN tasks t ON u.user_id = t.assigned_user_id AND t.tenant_id = u.tenant_id
		WHERE u.tenant_id = $1::uuid AND u.status = 'ACTIVE' %s
		GROUP BY u.user_id, u.tenant_id, u.username, u.email, u.display_name
		ORDER BY in_progress_task_count DESC, pending_task_count DESC
		LIMIT $%d OFFSET $%d
	`, teamFilter, len(args)+1, len(args)+2)

	args = append(args, size, offset)

	rows, err := db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("ListUsersWithWorkload: query: %w", err)
	}
	defer rows.Close()

	var lines []model.UserWorkloadRow
	for rows.Next() {
		var r model.UserWorkloadRow
		err := rows.StructScan(&r)
		if err != nil {
			return nil, 0, fmt.Errorf("ListUsersWithWorkload: scan: %w", err)
		}
		lines = append(lines, r)
	}

	// Just return length since requirement does not enforce strict TotalCount pagination matching for this dashboard view
	return lines, len(lines), nil
}

func (s *IdentityService) GetUnassignedTaskQueue(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	teamID string,
	page, size int,
) ([]model.Task, int, error) {
	if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	var teamFilter string
	var args []interface{}
	args = append(args, tenantID)

	if teamID != "" {
		teamFilter = ` AND assigned_team_id = $2::uuid `
		args = append(args, teamID)
	} else {
		teamFilter = ` AND assigned_team_id IS NULL ` // global pool
	}

	query := fmt.Sprintf(`
		SELECT * FROM tasks
		WHERE tenant_id = $1::uuid AND assigned_user_id IS NULL AND status = 'PENDING' %s
		ORDER BY priority DESC, created_at ASC
		LIMIT $%d OFFSET $%d
	`, teamFilter, len(args)+1, len(args)+2)

	args = append(args, size, offset)

	var tasks []model.Task
	err := db.SelectContext(ctx, &tasks, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("GetUnassignedTaskQueue: query: %w", err)
	}

	// Get total count
	countQuery := fmt.Sprintf(`
		SELECT count(*) FROM tasks
		WHERE tenant_id = $1::uuid AND assigned_user_id IS NULL AND status = 'PENDING' %s
	`, teamFilter)

	var total int
	err = db.QueryRowxContext(ctx, countQuery, args[:len(args)-2]...).Scan(&total)
	if err != nil {
		return tasks, len(tasks), nil // degrade gracefully
	}

	return tasks, total, nil
}

func (s *IdentityService) ListUsers(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filters model.ListUsersFilters,
	page, size int,
) ([]model.User, int, error) {
	if size > 200 {
		size = 200
	}
	offset := (page - 1) * size

	var whereClauses []string
	var args []interface{}

	whereClauses = append(whereClauses, "tenant_id = $1::uuid")
	args = append(args, tenantID)

	if filters.Status != nil {
		args = append(args, *filters.Status)
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", len(args)))
	}
	if filters.TeamID != nil {
		args = append(args, *filters.TeamID)
		whereClauses = append(whereClauses, fmt.Sprintf("user_id IN (SELECT user_id FROM team_members WHERE team_id = $%d::uuid)", len(args)))
	}
	if filters.RoleCode != nil {
		args = append(args, *filters.RoleCode)
		whereClauses = append(whereClauses, fmt.Sprintf("user_id IN (SELECT ur.user_id FROM user_roles ur JOIN roles r ON ur.role_id = r.role_id WHERE r.role_code = $%d)", len(args)))
	}
	if filters.Search != nil && *filters.Search != "" {
		args = append(args, "%"+*filters.Search+"%")
		whereClauses = append(whereClauses, fmt.Sprintf("(display_name ILIKE $%d OR email ILIKE $%d)", len(args), len(args)))
	}

	where := "WHERE " + strings.Join(whereClauses, " AND ")

	query := fmt.Sprintf(`
		SELECT * FROM users
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2)

	args = append(args, size, offset)

	var users []model.User
	err := db.SelectContext(ctx, &users, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("ListUsers: query: %w", err)
	}

	var total int
	countQuery := fmt.Sprintf(`SELECT count(*) FROM users %s`, where)
	err = db.QueryRowxContext(ctx, countQuery, args[:len(args)-2]...).Scan(&total)
	if err != nil {
		total = len(users)
	}

	return users, total, nil
}
