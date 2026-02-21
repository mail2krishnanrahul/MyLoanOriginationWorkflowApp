package multitenancy

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// Task reuses the canonical workflow task model.
type Task = model.Task

// UserStatus represents the lifecycle state of a user account.
type UserStatus string

const (
	UserStatusActive      UserStatus = "ACTIVE"
	UserStatusSuspended   UserStatus = "SUSPENDED"
	UserStatusDeactivated UserStatus = "DEACTIVATED"
)

// AuthProvider defines the external identity source.
type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "LOCAL"
	AuthProviderOIDC  AuthProvider = "OIDC"
	AuthProviderSAML  AuthProvider = "SAML"
)

// PermissionCode is a discrete capability grant.
type PermissionCode string

const (
	PermissionCaseCreate       PermissionCode = "CASE_CREATE"
	PermissionCaseViewOwn      PermissionCode = "CASE_VIEW_OWN"
	PermissionCaseViewAll      PermissionCode = "CASE_VIEW_ALL"
	PermissionCaseCancel       PermissionCode = "CASE_CANCEL"
	PermissionCaseReassign     PermissionCode = "CASE_REASSIGN"
	PermissionTaskClaim        PermissionCode = "TASK_CLAIM"
	PermissionTaskComplete     PermissionCode = "TASK_COMPLETE"
	PermissionTaskReject       PermissionCode = "TASK_REJECT"
	PermissionTaskReassign     PermissionCode = "TASK_REASSIGN"
	PermissionTaskViewOwn      PermissionCode = "TASK_VIEW_OWN"
	PermissionTaskViewAll      PermissionCode = "TASK_VIEW_ALL"
	PermissionApprovalApprove  PermissionCode = "APPROVAL_APPROVE"
	PermissionApprovalReject   PermissionCode = "APPROVAL_REJECT"
	PermissionApprovalEscalate PermissionCode = "APPROVAL_ESCALATE"
	PermissionApprovalViewAll  PermissionCode = "APPROVAL_VIEW_ALL"
	PermissionReportView       PermissionCode = "REPORT_VIEW"
	PermissionReportExport     PermissionCode = "REPORT_EXPORT"
	PermissionReportOperational PermissionCode = "REPORT_OPERATIONAL"
	PermissionUserCreate       PermissionCode = "USER_CREATE"
	PermissionUserSuspend      PermissionCode = "USER_SUSPEND"
	PermissionUserDeactivate   PermissionCode = "USER_DEACTIVATE"
	PermissionUserView         PermissionCode = "USER_VIEW"
	PermissionRoleManage       PermissionCode = "ROLE_MANAGE"
	PermissionTeamManage       PermissionCode = "TEAM_MANAGE"
	PermissionCaseTypeManage   PermissionCode = "CASETYPE_MANAGE"
	PermissionTenantConfigManage PermissionCode = "TENANT_CONFIG_MANAGE"
)

// User maps to the users table.
type User struct {
	UserID      string          `json:"user_id" db:"user_id"`
	TenantID    string          `json:"tenant_id" db:"tenant_id"`
	Username    string          `json:"username" db:"username"`
	Email       string          `json:"email" db:"email"`
	DisplayName string          `json:"display_name" db:"display_name"`
	Status      UserStatus      `json:"status" db:"status"`
	AuthProvider AuthProvider   `json:"auth_provider" db:"auth_provider"`
	ExternalID  *string         `json:"external_id,omitempty" db:"external_id"`
	Timezone    string          `json:"timezone" db:"timezone"`
	Locale      string          `json:"locale" db:"locale"`
	LastLoginAt *time.Time      `json:"last_login_at,omitempty" db:"last_login_at"`
	Metadata    json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// CreateUserInput defines mandatory user-registry fields.
type CreateUserInput struct {
	TenantID     string          `json:"tenant_id"`
	Username     string          `json:"username"`
	Email        string          `json:"email"`
	DisplayName  string          `json:"display_name"`
	Status       UserStatus      `json:"status"`
	AuthProvider AuthProvider    `json:"auth_provider"`
	ExternalID   *string         `json:"external_id,omitempty"`
	Timezone     string          `json:"timezone"`
	Locale       string          `json:"locale"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedBy    string          `json:"created_by"`
}

// UpdateUserProfileInput allows only profile fields.
type UpdateUserProfileInput struct {
	DisplayName string          `json:"display_name"`
	Timezone    string          `json:"timezone"`
	Locale      string          `json:"locale"`
	Metadata    json.RawMessage `json:"metadata"`
	UpdatedBy   string          `json:"updated_by"`
}

// Role maps to roles table.
type Role struct {
	RoleID        string          `json:"role_id" db:"role_id"`
	TenantID      string          `json:"tenant_id" db:"tenant_id"`
	RoleCode      string          `json:"role_code" db:"role_code"`
	DisplayName   string          `json:"display_name" db:"display_name"`
	Description   string          `json:"description" db:"description"`
	IsSystemRole  bool            `json:"is_system_role" db:"is_system_role"`
	Permissions   []string        `json:"permissions" db:"permissions"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

// UserRole maps to user_roles table.
type UserRole struct {
	UserID     string    `json:"user_id" db:"user_id"`
	RoleID     string    `json:"role_id" db:"role_id"`
	TenantID   string    `json:"tenant_id" db:"tenant_id"`
	AssignedBy string    `json:"assigned_by" db:"assigned_by"`
	AssignedAt time.Time `json:"assigned_at" db:"assigned_at"`
}

// TeamStatus represents team lifecycle.
type TeamStatus string

const (
	TeamStatusActive    TeamStatus = "ACTIVE"
	TeamStatusDisbanded TeamStatus = "DISBANDED"
)

// TeamType categorizes teams by operational purpose.
type TeamType string

const (
	TeamTypeProcessing   TeamType = "PROCESSING"
	TeamTypeUnderwriting TeamType = "UNDERWRITING"
	TeamTypeApproval     TeamType = "APPROVAL"
	TeamTypeOperations   TeamType = "OPERATIONS"
	TeamTypeMixed        TeamType = "MIXED"
)

// TeamMemberRole captures a user's role within a team.
type TeamMemberRole string

const (
	TeamMemberRoleMember  TeamMemberRole = "MEMBER"
	TeamMemberRoleLead    TeamMemberRole = "LEAD"
	TeamMemberRoleManager TeamMemberRole = "MANAGER"
)

// Team maps to teams table.
type Team struct {
	TeamID        string          `json:"team_id" db:"team_id"`
	TenantID      string          `json:"tenant_id" db:"tenant_id"`
	TeamCode      string          `json:"team_code" db:"team_code"`
	DisplayName   string          `json:"display_name" db:"display_name"`
	TeamType      TeamType        `json:"team_type" db:"team_type"`
	ParentTeamID  *string         `json:"parent_team_id,omitempty" db:"parent_team_id"`
	ManagerUserID *string         `json:"manager_user_id,omitempty" db:"manager_user_id"`
	Status        TeamStatus      `json:"status" db:"status"`
	Metadata      json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

// TeamMember maps to team_members table.
type TeamMember struct {
	TeamID      string         `json:"team_id" db:"team_id"`
	UserID      string         `json:"user_id" db:"user_id"`
	TenantID    string         `json:"tenant_id" db:"tenant_id"`
	RoleInTeam  TeamMemberRole `json:"role_in_team" db:"role_in_team"`
	JoinedAt    time.Time      `json:"joined_at" db:"joined_at"`
	AddedBy     string         `json:"added_by" db:"added_by"`
	DisplayName string         `json:"display_name,omitempty" db:"display_name"`
	Username    string         `json:"username,omitempty" db:"username"`
	Email       string         `json:"email,omitempty" db:"email"`
}

// CreateTeamInput defines team creation fields.
type CreateTeamInput struct {
	TenantID      string          `json:"tenant_id"`
	TeamCode      string          `json:"team_code"`
	DisplayName   string          `json:"display_name"`
	TeamType      TeamType        `json:"team_type"`
	ParentTeamID  *string         `json:"parent_team_id,omitempty"`
	ManagerUserID *string         `json:"manager_user_id,omitempty"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedBy     string          `json:"created_by"`
}

// UserWorkload summarizes assignment pressure for one user.
type UserWorkload struct {
	UserID                 string `json:"user_id"`
	PendingCount           int    `json:"pending_count"`
	InProgressCount        int    `json:"in_progress_count"`
	CompletedTodayCount    int    `json:"completed_today_count"`
	OldestPendingAgeSeconds int64 `json:"oldest_pending_age_seconds"`
	SLAAtRiskCount         int    `json:"sla_at_risk_count"`
}

// TeamMemberWorkload is per-member load summary inside a team.
type TeamMemberWorkload struct {
	UserID              string `json:"user_id" db:"user_id"`
	DisplayName         string `json:"display_name" db:"display_name"`
	PendingCount        int    `json:"pending_count" db:"pending_count"`
	InProgressCount     int    `json:"in_progress_count" db:"in_progress_count"`
	CompletedTodayCount int    `json:"completed_today_count" db:"completed_today_count"`
}

// TeamWorkload summarizes queue pressure for a team.
type TeamWorkload struct {
	TeamID                    string               `json:"team_id"`
	Members                   []TeamMemberWorkload `json:"members"`
	UnassignedPoolTaskCount   int                  `json:"unassigned_pool_task_count"`
	TeamQueueDepth            int                  `json:"team_queue_depth"`
	OldestUnassignedAgeSeconds int64               `json:"oldest_unassigned_age_seconds"`
}

// UserWorkloadRow is a row-model for workload dashboards.
type UserWorkloadRow struct {
	UserID               string `json:"user_id" db:"user_id"`
	Username             string `json:"username" db:"username"`
	DisplayName          string `json:"display_name" db:"display_name"`
	Email                string `json:"email" db:"email"`
	TeamID               string `json:"team_id,omitempty" db:"team_id"`
	PendingCount         int    `json:"pending_count" db:"pending_count"`
	InProgressCount      int    `json:"in_progress_count" db:"in_progress_count"`
	CompletedTodayCount  int    `json:"completed_today_count" db:"completed_today_count"`
	SLAAtRiskCount       int    `json:"sla_at_risk_count" db:"sla_at_risk_count"`
}

// ListUsersFilters defines optional user list filters.
type ListUsersFilters struct {
	Status   *UserStatus `json:"status,omitempty"`
	TeamID   string      `json:"team_id,omitempty"`
	RoleCode string      `json:"role_code,omitempty"`
	Search   string      `json:"search,omitempty"`
}

// ListTeamsFilters defines optional team list filters.
type ListTeamsFilters struct {
	Status       *TeamStatus `json:"status,omitempty"`
	TeamType     *TeamType   `json:"team_type,omitempty"`
	ParentTeamID string      `json:"parent_team_id,omitempty"`
}

// LoginTracker buffers frequent login updates and flushes periodically in batch.
type LoginTracker struct {
	db       *sqlx.DB
	buffer   sync.Map // map[string]time.Time
	interval time.Duration
	logger   *slog.Logger
}

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrUsernameTaken         = errors.New("username already exists in tenant")
	ErrEmailTaken            = errors.New("email already exists in tenant")
	ErrUserSuspended         = errors.New("user is suspended")
	ErrUserDeactivated       = errors.New("user is deactivated")
	ErrExternalIDConflict    = errors.New("external id already exists in tenant")
	ErrPermissionDenied      = errors.New("permission denied")
	ErrLastRoleRevocation    = errors.New("cannot revoke last role from user")
	ErrRoleNotFound          = errors.New("role not found")
	ErrRoleTenantMismatch    = errors.New("role does not belong to tenant")
	ErrTeamNotFound          = errors.New("team not found")
	ErrTeamDisbanded         = errors.New("team is disbanded")
	ErrTeamHierarchyTooDeep  = errors.New("team hierarchy exceeds max depth")
	ErrTeamWouldBeEmpty      = errors.New("team would be empty")
	ErrTeamHasOpenTasks      = errors.New("team has open tasks")
	ErrTeamTenantMismatch    = errors.New("team does not belong to tenant")
	ErrTaskAlreadyClaimed    = errors.New("task already claimed")
	ErrTaskNotAssignable     = errors.New("task is not assignable")
	ErrUserNotTeamMember     = errors.New("user is not a member of team")
	ErrTaskReassignForbidden = errors.New("task reassignment forbidden")
)

func permissionDeniedError(permission PermissionCode) error {
	return fmt.Errorf("%w: %s", ErrPermissionDenied, permission)
}

func teamHasOpenTasksError(count int) error {
	return fmt.Errorf("%w: open_tasks=%d", ErrTeamHasOpenTasks, count)
}
