package multitenancy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"
)

const (
	RoleCodeLoanOfficer    = "LOAN_OFFICER"
	RoleCodeUnderwriter    = "UNDERWRITER"
	RoleCodeSeniorApprover = "SENIOR_APPROVER"
	RoleCodeSupervisor     = "SUPERVISOR"
	RoleCodeAdmin          = "ADMIN"
)

type permissionCacheEntry struct {
	mu    sync.RWMutex
	users map[string]map[PermissionCode]struct{}
}

var permissionRequestCache sync.Map // map[string]*permissionCacheEntry keyed by ctx.Done channel identity.

func cacheKeyFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	done := ctx.Done()
	if done == nil {
		return "", false
	}
	return fmt.Sprintf("%p", done), true
}

func getOrCreatePermissionCache(ctx context.Context) *permissionCacheEntry {
	key, ok := cacheKeyFromContext(ctx)
	if !ok {
		return nil
	}
	entry := &permissionCacheEntry{users: make(map[string]map[PermissionCode]struct{})}
	actual, loaded := permissionRequestCache.LoadOrStore(key, entry)
	if !loaded {
		go func() {
			<-ctx.Done()
			permissionRequestCache.Delete(key)
		}()
	}
	if cast, ok := actual.(*permissionCacheEntry); ok {
		return cast
	}
	return nil
}

func cacheUserPermissions(ctx context.Context, tenantID string, userID string, perms map[PermissionCode]struct{}) {
	entry := getOrCreatePermissionCache(ctx)
	if entry == nil {
		return
	}
	key := strings.TrimSpace(tenantID) + ":" + strings.TrimSpace(userID)
	entry.mu.Lock()
	entry.users[key] = perms
	entry.mu.Unlock()
}

func getCachedUserPermissions(ctx context.Context, tenantID string, userID string) (map[PermissionCode]struct{}, bool) {
	entry := getOrCreatePermissionCache(ctx)
	if entry == nil {
		return nil, false
	}
	key := strings.TrimSpace(tenantID) + ":" + strings.TrimSpace(userID)
	entry.mu.RLock()
	perms, ok := entry.users[key]
	entry.mu.RUnlock()
	return perms, ok
}

func systemRolePermissions() map[string][]PermissionCode {
	underwriter := []PermissionCode{
		PermissionCaseViewAll,
		PermissionTaskClaim,
		PermissionTaskComplete,
		PermissionTaskReject,
		PermissionTaskViewAll,
	}
	seniorApprover := append([]PermissionCode{}, underwriter...)
	seniorApprover = append(seniorApprover,
		PermissionApprovalApprove,
		PermissionApprovalReject,
		PermissionApprovalViewAll,
	)
	supervisor := append([]PermissionCode{}, seniorApprover...)
	supervisor = append(supervisor,
		PermissionApprovalEscalate,
		PermissionTaskReassign,
		PermissionCaseReassign,
		PermissionReportView,
		PermissionReportOperational,
	)

	all := []PermissionCode{
		PermissionCaseCreate,
		PermissionCaseViewOwn,
		PermissionCaseViewAll,
		PermissionCaseCancel,
		PermissionCaseReassign,
		PermissionTaskClaim,
		PermissionTaskComplete,
		PermissionTaskReject,
		PermissionTaskReassign,
		PermissionTaskViewOwn,
		PermissionTaskViewAll,
		PermissionApprovalApprove,
		PermissionApprovalReject,
		PermissionApprovalEscalate,
		PermissionApprovalViewAll,
		PermissionReportView,
		PermissionReportExport,
		PermissionReportOperational,
		PermissionUserCreate,
		PermissionUserSuspend,
		PermissionUserDeactivate,
		PermissionUserView,
		PermissionRoleManage,
		PermissionTeamManage,
		PermissionCaseTypeManage,
		PermissionTenantConfigManage,
	}

	return map[string][]PermissionCode{
		RoleCodeLoanOfficer: {
			PermissionCaseCreate,
			PermissionCaseViewOwn,
			PermissionTaskClaim,
			PermissionTaskComplete,
			PermissionTaskViewOwn,
		},
		RoleCodeUnderwriter:    underwriter,
		RoleCodeSeniorApprover: seniorApprover,
		RoleCodeSupervisor:     supervisor,
		RoleCodeAdmin:          all,
	}
}

func roleDisplayDescription(roleCode string) (string, string) {
	switch roleCode {
	case RoleCodeLoanOfficer:
		return "Loan Officer", "Create and view own cases; claim and complete assigned tasks"
	case RoleCodeUnderwriter:
		return "Underwriter", "View all cases; claim queue tasks; reject tasks"
	case RoleCodeSeniorApprover:
		return "Senior Approver", "Underwriter permissions plus approval gate decisions"
	case RoleCodeSupervisor:
		return "Supervisor", "Senior approver permissions plus reassignment and operational reporting"
	case RoleCodeAdmin:
		return "Admin", "All permissions including user, role, and tenant administration"
	default:
		return roleCode, "System role"
	}
}

func permissionCodesToStrings(perms []PermissionCode) []string {
	out := make([]string, 0, len(perms))
	for i := range perms {
		out = append(out, string(perms[i]))
	}
	return out
}

// SeedSystemRoles seeds mandatory system roles for a tenant inside the caller transaction.
func SeedSystemRoles(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	createdBy string,
) error {
	if tx == nil {
		return fmt.Errorf("SeedSystemRoles: tx is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	createdBy = strings.TrimSpace(createdBy)
	if createdBy == "" {
		createdBy = "system"
	}
	if tenantID == "" {
		return fmt.Errorf("SeedSystemRoles: tenantID is required")
	}

	roles := systemRolePermissions()
	orderedCodes := []string{
		RoleCodeLoanOfficer,
		RoleCodeUnderwriter,
		RoleCodeSeniorApprover,
		RoleCodeSupervisor,
		RoleCodeAdmin,
	}

	for _, roleCode := range orderedCodes {
		perms := permissionCodesToStrings(roles[roleCode])
		display, description := roleDisplayDescription(roleCode)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO roles (
				tenant_id,
				role_code,
				display_name,
				description,
				is_system_role,
				permissions,
				created_at,
				updated_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4,
				TRUE,
				$5,
				now(),
				now()
			)
			ON CONFLICT (tenant_id, role_code)
			DO UPDATE
			SET display_name = EXCLUDED.display_name,
			    description = EXCLUDED.description,
			    is_system_role = TRUE,
			    permissions = EXCLUDED.permissions,
			    updated_at = now()
		`, tenantID, roleCode, display, description, perms)
		if err != nil {
			return fmt.Errorf("SeedSystemRoles: upsert role %s: %w", roleCode, mapRoleInsertError(err))
		}
	}

	logUserTeamInfo("system roles seeded", "tenant_id", tenantID, "created_by", createdBy)
	return nil
}

// UserHasPermission resolves effective permissions from assigned roles and checks membership.
func UserHasPermission(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	permission PermissionCode,
) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("UserHasPermission: db is nil")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "UserHasPermission")
	if err != nil {
		return false, fmt.Errorf("UserHasPermission: %w", err)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, fmt.Errorf("UserHasPermission: userID is required")
	}
	permission = PermissionCode(strings.ToUpper(strings.TrimSpace(string(permission))))
	if permission == "" {
		return false, fmt.Errorf("UserHasPermission: permission is required")
	}

	if cached, ok := getCachedUserPermissions(ctx, resolvedTenantID, userID); ok {
		_, has := cached[permission]
		return has, nil
	}

	rows := make([]string, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT DISTINCT p.permission
		FROM user_roles ur
		JOIN roles r
		  ON r.role_id = ur.role_id
		 AND r.tenant_id = ur.tenant_id
		CROSS JOIN LATERAL unnest(r.permissions) AS p(permission)
		WHERE ur.tenant_id = $1::uuid
		  AND ur.user_id = $2::uuid
	`, resolvedTenantID, userID); err != nil {
		return false, fmt.Errorf("UserHasPermission: resolve permissions: %w", err)
	}

	set := make(map[PermissionCode]struct{}, len(rows))
	for i := range rows {
		code := PermissionCode(strings.ToUpper(strings.TrimSpace(rows[i])))
		if code == "" {
			continue
		}
		set[code] = struct{}{}
	}
	cacheUserPermissions(ctx, resolvedTenantID, userID, set)

	_, has := set[permission]
	return has, nil
}

// AssertPermission enforces capability checks with sentinel error semantics.
func AssertPermission(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	permission PermissionCode,
) error {
	has, err := UserHasPermission(ctx, db, userID, tenantID, permission)
	if err != nil {
		return fmt.Errorf("AssertPermission: %w", err)
	}
	if !has {
		return fmt.Errorf("AssertPermission: %w", permissionDeniedError(permission))
	}
	return nil
}
