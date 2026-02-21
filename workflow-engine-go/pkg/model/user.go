package model

import (
	"errors"
	"time"
)

// UserStatus represents the lifecycle state of a user.
type UserStatus string

const (
	UserStatusActive      UserStatus = "ACTIVE"
	UserStatusSuspended   UserStatus = "SUSPENDED"
	UserStatusDeactivated UserStatus = "DEACTIVATED"
)

// AuthProvider identifies the identity system managing credentials.
type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "LOCAL"
	AuthProviderOIDC  AuthProvider = "OIDC"
	AuthProviderSAML  AuthProvider = "SAML"
)

// User represents a human actor mapped to the workflow system.
type User struct {
	UserID       string       `db:"user_id" json:"user_id"`
	TenantID     string       `db:"tenant_id" json:"tenant_id"`
	Username     string       `db:"username" json:"username"`
	Email        string       `db:"email" json:"email"`
	DisplayName  string       `db:"display_name" json:"display_name"`
	Status       UserStatus   `db:"status" json:"status"`
	AuthProvider AuthProvider `db:"auth_provider" json:"auth_provider"`
	ExternalID   *string      `db:"external_id" json:"external_id"`
	Timezone     string       `db:"timezone" json:"timezone"`
	Locale       string       `db:"locale" json:"locale"`
	LastLoginAt  *time.Time   `db:"last_login_at" json:"last_login_at"`
	Metadata     []byte       `db:"metadata" json:"metadata"` // JSONB
	CreatedAt    time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time    `db:"updated_at" json:"updated_at"`
}

// CreateUserInput shapes the payload for onboarding new identities.
type CreateUserInput struct {
	TenantID     string
	Username     string
	Email        string
	DisplayName  string
	AuthProvider AuthProvider
	ExternalID   *string
	Timezone     string
	Locale       string
	Metadata     []byte
}

// UpdateUserProfileInput is the payload for user detail mutation.
type UpdateUserProfileInput struct {
	DisplayName *string
	Timezone    *string
	Locale      *string
	Metadata    []byte
}

// Role represents a named group of operational capabilities.
type Role struct {
	RoleID       string    `db:"role_id" json:"role_id"`
	TenantID     string    `db:"tenant_id" json:"tenant_id"`
	RoleCode     string    `db:"role_code" json:"role_code"`
	DisplayName  string    `db:"display_name" json:"display_name"`
	Description  *string   `db:"description" json:"description"`
	IsSystemRole bool      `db:"is_system_role" json:"is_system_role"`
	Permissions  []string  `db:"permissions" json:"permissions"` // text[] equivalent in Go/sqlx needs mapping handling (usually pq.StringArray)
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// UserRole links users to capability domains.
type UserRole struct {
	UserID     string    `db:"user_id" json:"user_id"`
	RoleID     string    `db:"role_id" json:"role_id"`
	TenantID   string    `db:"tenant_id" json:"tenant_id"`
	AssignedBy string    `db:"assigned_by" json:"assigned_by"`
	AssignedAt time.Time `db:"assigned_at" json:"assigned_at"`
}

// TeamStatus represents the lifecycle state of a task-assignment pool.
type TeamStatus string

const (
	TeamStatusActive    TeamStatus = "ACTIVE"
	TeamStatusDisbanded TeamStatus = "DISBANDED"
)

// TeamType identifies the operational nature of a group.
type TeamType string

const (
	TeamTypeProcessing   TeamType = "PROCESSING"
	TeamTypeUnderwriting TeamType = "UNDERWRITING"
	TeamTypeApproval     TeamType = "APPROVAL"
	TeamTypeOperations   TeamType = "OPERATIONS"
	TeamTypeMixed        TeamType = "MIXED"
)

// TeamMemberRole denotes hierarchy within a specific team.
type TeamMemberRole string

const (
	TeamMemberRoleMember  TeamMemberRole = "MEMBER"
	TeamMemberRoleLead    TeamMemberRole = "LEAD"
	TeamMemberRoleManager TeamMemberRole = "MANAGER"
)

// Team maps an operational group.
type Team struct {
	TeamID        string     `db:"team_id" json:"team_id"`
	TenantID      string     `db:"tenant_id" json:"tenant_id"`
	TeamCode      string     `db:"team_code" json:"team_code"`
	DisplayName   string     `db:"display_name" json:"display_name"`
	TeamType      TeamType   `db:"team_type" json:"team_type"`
	ParentTeamID  *string    `db:"parent_team_id" json:"parent_team_id"`
	ManagerUserID *string    `db:"manager_user_id" json:"manager_user_id"`
	Status        TeamStatus `db:"status" json:"status"`
	Metadata      []byte     `db:"metadata" json:"metadata"` // JSONB
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

// TeamMember stores the linkage matrix of users to teams.
type TeamMember struct {
	TeamID     string         `db:"team_id" json:"team_id"`
	UserID     string         `db:"user_id" json:"user_id"`
	TenantID   string         `db:"tenant_id" json:"tenant_id"`
	RoleInTeam TeamMemberRole `db:"role_in_team" json:"role_in_team"`
	JoinedAt   time.Time      `db:"joined_at" json:"joined_at"`
	AddedBy    string         `db:"added_by" json:"added_by"`
}

// CreateTeamInput dictates the creation of a new team registry.
type CreateTeamInput struct {
	TenantID      string
	TeamCode      string
	DisplayName   string
	TeamType      TeamType
	ParentTeamID  *string
	ManagerUserID *string
	Metadata      []byte
}

// ---------------------------------------------------------------------------
// Typed Permission Codes
// ---------------------------------------------------------------------------

type PermissionCode string

const (
	// Case
	PermissionCaseCreate   PermissionCode = "CASE_CREATE"
	PermissionCaseViewOwn  PermissionCode = "CASE_VIEW_OWN"
	PermissionCaseViewAll  PermissionCode = "CASE_VIEW_ALL"
	PermissionCaseCancel   PermissionCode = "CASE_CANCEL"
	PermissionCaseReassign PermissionCode = "CASE_REASSIGN"

	// Task
	PermissionTaskClaim    PermissionCode = "TASK_CLAIM"
	PermissionTaskComplete PermissionCode = "TASK_COMPLETE"
	PermissionTaskReject   PermissionCode = "TASK_REJECT"
	PermissionTaskReassign PermissionCode = "TASK_REASSIGN"
	PermissionTaskViewOwn  PermissionCode = "TASK_VIEW_OWN"
	PermissionTaskViewAll  PermissionCode = "TASK_VIEW_ALL"

	// Approval
	PermissionApprovalApprove  PermissionCode = "APPROVAL_APPROVE"
	PermissionApprovalReject   PermissionCode = "APPROVAL_REJECT"
	PermissionApprovalEscalate PermissionCode = "APPROVAL_ESCALATE"
	PermissionApprovalViewAll  PermissionCode = "APPROVAL_VIEW_ALL"

	// Reporting
	PermissionReportView        PermissionCode = "REPORT_VIEW"
	PermissionReportExport      PermissionCode = "REPORT_EXPORT"
	PermissionReportOperational PermissionCode = "REPORT_OPERATIONAL"

	// Administration
	PermissionUserCreate         PermissionCode = "USER_CREATE"
	PermissionUserSuspend        PermissionCode = "USER_SUSPEND"
	PermissionUserDeactivate     PermissionCode = "USER_DEACTIVATE"
	PermissionUserView           PermissionCode = "USER_VIEW"
	PermissionRoleManage         PermissionCode = "ROLE_MANAGE"
	PermissionTeamManage         PermissionCode = "TEAM_MANAGE"
	PermissionCaseTypeManage     PermissionCode = "CASETYPE_MANAGE"
	PermissionTenantConfigManage PermissionCode = "TENANT_CONFIG_MANAGE"
)

// ---------------------------------------------------------------------------
// Visibility Queries & Models
// ---------------------------------------------------------------------------

type UserWorkload struct {
	PendingTaskCount    int     `json:"pending_task_count"`
	InProgressTaskCount int     `json:"in_progress_task_count"`
	CompletedTodayCount int     `json:"completed_today_count"`
	OldestPendingAge    *string `json:"oldest_pending_age,omitempty"` // Example: "4h30m"
	SlaAtRiskCount      int     `json:"sla_at_risk_count"`
}

type TeamWorkload struct {
	MemberTaskCounts     map[string]int `json:"member_task_counts"` // map[user_id]count
	UnassignedTaskCount  int            `json:"unassigned_task_count"`
	TeamQueueDepth       int            `json:"team_queue_depth"`
	OldestUnassignedTask *string        `json:"oldest_unassigned_task,omitempty"`
}

type UserWorkloadRow struct {
	User
	PendingTaskCount    int `db:"pending_task_count" json:"pending_task_count"`
	InProgressTaskCount int `db:"in_progress_task_count" json:"in_progress_task_count"`
	CompletedTodayCount int `db:"completed_today_count" json:"completed_today_count"`
}

type ListUsersFilters struct {
	Status   *UserStatus `json:"status,omitempty"`
	TeamID   *string     `json:"team_id,omitempty"`
	RoleCode *string     `json:"role_code,omitempty"`
	Search   *string     `json:"search,omitempty"`
}

type ListTeamsFilters struct {
	Status       *TeamStatus `json:"status,omitempty"`
	TeamType     *TeamType   `json:"team_type,omitempty"`
	ParentTeamID *string     `json:"parent_team_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Sentinel Errors
// ---------------------------------------------------------------------------

var (
	ErrUserNotFound          = errors.New("identity: user not found")
	ErrUsernameTaken         = errors.New("identity: username already taken within tenant")
	ErrEmailTaken            = errors.New("identity: email address already taken within tenant")
	ErrUserSuspended         = errors.New("identity: user account is suspended")
	ErrUserDeactivated       = errors.New("identity: user account is permanently deactivated")
	ErrExternalIDConflict    = errors.New("identity: external identity provdier ID is already linked to another user")
	ErrLastRoleRevocation    = errors.New("identity: user must retain at least one role; cannot revoke last role")
	ErrRoleNotFound          = errors.New("identity: role not found")
	ErrRoleTenantMismatch    = errors.New("identity: role belongs to a different tenant context")
	ErrTeamNotFound          = errors.New("identity: team not found")
	ErrTeamDisbanded         = errors.New("identity: team is disbanded")
	ErrTeamHierarchyTooDeep  = errors.New("identity: maximum team hierarchy depth exceeded (max 3)")
	ErrTeamWouldBeEmpty      = errors.New("identity: cannot remove user, team must retain at least one member or lead")
	ErrTeamTenantMismatch    = errors.New("identity: team belongs to a different tenant context")
	ErrTaskAlreadyClaimed    = errors.New("identity: task has already been claimed or modified by another transaction")
	ErrTaskNotAssignable     = errors.New("identity: task status is not assignable")
	ErrUserNotTeamMember     = errors.New("identity: user is not a member of the designated team pool")
	ErrTaskReassignForbidden = errors.New("identity: caller lacks reassignment override permissions")
)

// ErrPermissionDenied encapsulates missing operational rights.
type ErrPermissionDenied struct {
	Permission PermissionCode
}

func (e *ErrPermissionDenied) Error() string {
	return "permission denied: user lacks required " + string(e.Permission) + " operational right"
}

// ErrTeamHasOpenTasks ensures disbanded teams are cleanly flushed
type ErrTeamHasOpenTasks struct {
	Count int
}

func (e *ErrTeamHasOpenTasks) Error() string {
	return "team cannot be disbanded; there are currently " + itoa(e.Count) + " open or ongoing tasks assigned"
}

// basic int-to-string for avoiding package cycles or extra dependencies
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte(i%10) + '0'}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
