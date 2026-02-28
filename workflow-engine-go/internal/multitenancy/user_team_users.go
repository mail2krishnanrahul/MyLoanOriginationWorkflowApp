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

func postgresUUIDArray(ids []string) string {
	if len(ids) == 0 {
		return "{}"
	}
	return "{" + strings.Join(ids, ",") + "}"
}

// CreateUser creates a tenant-scoped user and emits USER_CREATED in the same transaction.
func CreateUser(
	ctx context.Context,
	db *sqlx.DB,
	input CreateUserInput,
) (User, error) {
	if err := validateUserCreateInput(&input); err != nil {
		return User{}, fmt.Errorf("CreateUser: %w", err)
	}
	tenantID, err := resolveTenantIDForOperation(ctx, input.TenantID, "CreateUser")
	if err != nil {
		return User{}, fmt.Errorf("CreateUser: %w", err)
	}
	input.TenantID = tenantID

	tx, err := beginSQLXTx(ctx, db, "CreateUser")
	if err != nil {
		return User{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var user User
	err = tx.GetContext(ctx, &user, `
		INSERT INTO users (
			tenant_id,
			username,
			email,
			display_name,
			full_name,
			role_code,
			status,
			auth_provider,
			external_id,
			timezone,
			locale,
			metadata
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12::jsonb
		)
		RETURNING
			user_id::text AS user_id,
			tenant_id::text AS tenant_id,
			username,
			email,
			display_name,
			status,
			auth_provider,
			external_id,
			timezone,
			locale,
			last_login_at,
			metadata,
			created_at,
			updated_at
	`,
		input.TenantID,
		input.Username,
		input.Email,
		input.DisplayName,
		input.DisplayName,
		"USER",
		string(input.Status),
		string(input.AuthProvider),
		input.ExternalID,
		input.Timezone,
		input.Locale,
		normalizeJSON(input.Metadata),
	)
	if err != nil {
		mapped := mapUserInsertError(err)
		if errors.Is(mapped, ErrUsernameTaken) || errors.Is(mapped, ErrEmailTaken) || errors.Is(mapped, ErrExternalIDConflict) {
			return User{}, fmt.Errorf("CreateUser: %w", mapped)
		}
		return User{}, fmt.Errorf("CreateUser: insert user: %w", mapped)
	}

	if err := publishUserTeamEventTx(ctx, tx, input.TenantID, nil, nil, model.EventUserCreated, map[string]interface{}{
		"user_id":       user.UserID,
		"tenant_id":     input.TenantID,
		"username":      user.Username,
		"email":         user.Email,
		"auth_provider": user.AuthProvider,
		"created_by":    input.CreatedBy,
		"occurred_at":   time.Now().UTC(),
	}); err != nil {
		return User{}, fmt.Errorf("CreateUser: publish USER_CREATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("CreateUser: commit: %w", err)
	}

	logUserTeamInfo("user created", "tenant_id", input.TenantID, "user_id", user.UserID, "username", user.Username)
	return user, nil
}

// SuspendUser changes an ACTIVE user to SUSPENDED and emits USER_SUSPENDED.
func SuspendUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	suspendedBy string,
	reason string,
) error {
	userID = strings.TrimSpace(userID)
	tenantID = strings.TrimSpace(tenantID)
	suspendedBy = strings.TrimSpace(suspendedBy)
	reason = strings.TrimSpace(reason)
	if userID == "" {
		return fmt.Errorf("SuspendUser: userID is required")
	}
	if suspendedBy == "" {
		suspendedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "SuspendUser")
	if err != nil {
		return fmt.Errorf("SuspendUser: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "SuspendUser")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var status string
	if err := tx.GetContext(ctx, &status, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
		FOR UPDATE
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("SuspendUser: %w", ErrUserNotFound)
		}
		return fmt.Errorf("SuspendUser: lock user: %w", err)
	}

	s := normalizeUserStatus(UserStatus(status))
	if s == UserStatusDeactivated {
		return fmt.Errorf("SuspendUser: %w", ErrUserDeactivated)
	}
	if s != UserStatusSuspended {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET status = 'SUSPENDED',
			    updated_at = now()
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
		`, resolvedTenantID, userID); err != nil {
			return fmt.Errorf("SuspendUser: update status: %w", err)
		}
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventUserSuspended, map[string]interface{}{
		"user_id":      userID,
		"tenant_id":    resolvedTenantID,
		"suspended_by": suspendedBy,
		"reason":       reason,
		"occurred_at":  time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("SuspendUser: publish USER_SUSPENDED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("SuspendUser: commit: %w", err)
	}

	logUserTeamInfo("user suspended", "tenant_id", resolvedTenantID, "user_id", userID, "suspended_by", suspendedBy)
	return nil
}

// DeactivateUser marks user terminal and atomically unassigns open tasks.
func DeactivateUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	deactivatedBy string,
	reason string,
) error {
	userID = strings.TrimSpace(userID)
	deactivatedBy = strings.TrimSpace(deactivatedBy)
	reason = strings.TrimSpace(reason)
	if userID == "" {
		return fmt.Errorf("DeactivateUser: userID is required")
	}
	if deactivatedBy == "" {
		deactivatedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "DeactivateUser")
	if err != nil {
		return fmt.Errorf("DeactivateUser: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "DeactivateUser")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var status string
	if err := tx.GetContext(ctx, &status, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
		FOR UPDATE
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("DeactivateUser: %w", ErrUserNotFound)
		}
		return fmt.Errorf("DeactivateUser: lock user: %w", err)
	}

	if normalizeUserStatus(UserStatus(status)) != UserStatusDeactivated {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET status = 'DEACTIVATED',
			    updated_at = now()
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
		`, resolvedTenantID, userID); err != nil {
			return fmt.Errorf("DeactivateUser: update status: %w", err)
		}
	}

	type taskRef struct {
		TaskID string         `db:"task_id"`
		CaseID sql.NullString `db:"case_id"`
	}
	tasks := make([]taskRef, 0)
	if err := tx.SelectContext(ctx, &tasks, `
		SELECT id::text AS task_id, case_id::text AS case_id
		FROM tasks
		WHERE tenant_id = $1::uuid
		  AND assigned_user_id = $2::uuid
		  AND status IN ('IN_PROGRESS', 'PENDING')
		FOR UPDATE
	`, resolvedTenantID, userID); err != nil {
		return fmt.Errorf("DeactivateUser: select open assigned tasks: %w", err)
	}

	if len(tasks) > 0 {
		taskIDs := make([]string, 0, len(tasks))
		for i := range tasks {
			taskIDs = append(taskIDs, tasks[i].TaskID)
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET assigned_user_id = NULL,
			    status = 'PENDING',
			    assigned_at = NULL,
			    updated_at = now(),
			    version = version + 1
			WHERE tenant_id = $1::uuid
			  AND id = ANY($2::uuid[])
		`, resolvedTenantID, postgresUUIDArray(taskIDs)); err != nil {
			return fmt.Errorf("DeactivateUser: unassign open tasks: %w", err)
		}

		for i := range tasks {
			var caseID *string
			if tasks[i].CaseID.Valid {
				value := strings.TrimSpace(tasks[i].CaseID.String)
				if value != "" {
					caseID = &value
				}
			}
			taskID := tasks[i].TaskID
			if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, caseID, &taskID, model.EventTaskUnassigned, map[string]interface{}{
				"task_id":       taskID,
				"user_id":       userID,
				"tenant_id":     resolvedTenantID,
				"unassigned_by": deactivatedBy,
				"reason":        reason,
				"source":        "user_deactivation",
				"occurred_at":   time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("DeactivateUser: publish TASK_UNASSIGNED for task %s: %w", taskID, err)
			}
		}
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventUserDeactivated, map[string]interface{}{
		"user_id":          userID,
		"tenant_id":        resolvedTenantID,
		"deactivated_by":   deactivatedBy,
		"reason":           reason,
		"unassigned_tasks": len(tasks),
		"occurred_at":      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("DeactivateUser: publish USER_DEACTIVATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DeactivateUser: commit: %w", err)
	}

	logUserTeamInfo("user deactivated", "tenant_id", resolvedTenantID, "user_id", userID, "open_tasks_unassigned", len(tasks), "deactivated_by", deactivatedBy)
	return nil
}

// ReactivateUser transitions SUSPENDED -> ACTIVE, never DEACTIVATED -> ACTIVE.
func ReactivateUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	reactivatedBy string,
) error {
	userID = strings.TrimSpace(userID)
	reactivatedBy = strings.TrimSpace(reactivatedBy)
	if userID == "" {
		return fmt.Errorf("ReactivateUser: userID is required")
	}
	if reactivatedBy == "" {
		reactivatedBy = "system"
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "ReactivateUser")
	if err != nil {
		return fmt.Errorf("ReactivateUser: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "ReactivateUser")
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var status string
	if err := tx.GetContext(ctx, &status, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
		FOR UPDATE
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("ReactivateUser: %w", ErrUserNotFound)
		}
		return fmt.Errorf("ReactivateUser: lock user: %w", err)
	}

	s := normalizeUserStatus(UserStatus(status))
	if s == UserStatusDeactivated {
		return fmt.Errorf("ReactivateUser: %w", ErrUserDeactivated)
	}
	if s == UserStatusSuspended {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET status = 'ACTIVE',
			    updated_at = now()
			WHERE tenant_id = $1::uuid
			  AND user_id = $2::uuid
		`, resolvedTenantID, userID); err != nil {
			return fmt.Errorf("ReactivateUser: update status: %w", err)
		}
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventUserReactivated, map[string]interface{}{
		"user_id":        userID,
		"tenant_id":      resolvedTenantID,
		"reactivated_by": reactivatedBy,
		"occurred_at":    time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("ReactivateUser: publish USER_REACTIVATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ReactivateUser: commit: %w", err)
	}

	logUserTeamInfo("user reactivated", "tenant_id", resolvedTenantID, "user_id", userID, "reactivated_by", reactivatedBy)
	return nil
}

// UpdateUserProfile updates profile-only fields and emits USER_PROFILE_UPDATED.
func UpdateUserProfile(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	input UpdateUserProfileInput,
) (User, error) {
	if err := validateUserProfileInput(&input); err != nil {
		return User{}, fmt.Errorf("UpdateUserProfile: %w", err)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return User{}, fmt.Errorf("UpdateUserProfile: userID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "UpdateUserProfile")
	if err != nil {
		return User{}, fmt.Errorf("UpdateUserProfile: %w", err)
	}

	tx, err := beginSQLXTx(ctx, db, "UpdateUserProfile")
	if err != nil {
		return User{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var user User
	if err := tx.GetContext(ctx, &user, `
		UPDATE users
		SET display_name = $1,
		    full_name = $1,
		    timezone = $2,
		    locale = $3,
		    metadata = $4::jsonb,
		    updated_at = now()
		WHERE tenant_id = $5::uuid
		  AND user_id = $6::uuid
		RETURNING
			user_id::text AS user_id,
			tenant_id::text AS tenant_id,
			username,
			email,
			display_name,
			status,
			auth_provider,
			external_id,
			timezone,
			locale,
			last_login_at,
			metadata,
			created_at,
			updated_at
	`, input.DisplayName, input.Timezone, input.Locale, normalizeJSON(input.Metadata), resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, fmt.Errorf("UpdateUserProfile: %w", ErrUserNotFound)
		}
		return User{}, fmt.Errorf("UpdateUserProfile: update profile: %w", err)
	}

	if err := publishUserTeamEventTx(ctx, tx, resolvedTenantID, nil, nil, model.EventUserProfileUpdated, map[string]interface{}{
		"user_id":     userID,
		"tenant_id":   resolvedTenantID,
		"updated_by":  input.UpdatedBy,
		"occurred_at": time.Now().UTC(),
	}); err != nil {
		return User{}, fmt.Errorf("UpdateUserProfile: publish USER_PROFILE_UPDATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("UpdateUserProfile: commit: %w", err)
	}

	logUserTeamInfo("user profile updated", "tenant_id", resolvedTenantID, "user_id", userID, "updated_by", input.UpdatedBy)
	return user, nil
}

// RecordUserLogin validates status and delegates write buffering to LoginTracker.
func RecordUserLogin(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) error {
	if db == nil {
		return fmt.Errorf("RecordUserLogin: db is nil")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("RecordUserLogin: userID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "RecordUserLogin")
	if err != nil {
		return fmt.Errorf("RecordUserLogin: %w", err)
	}

	var status string
	if err := db.GetContext(ctx, &status, `
		SELECT status
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("RecordUserLogin: %w", ErrUserNotFound)
		}
		return fmt.Errorf("RecordUserLogin: load status: %w", err)
	}

	s := normalizeUserStatus(UserStatus(status))
	switch s {
	case UserStatusActive:
		// continue
	case UserStatusSuspended:
		return fmt.Errorf("RecordUserLogin: %w", ErrUserSuspended)
	case UserStatusDeactivated:
		return fmt.Errorf("RecordUserLogin: %w", ErrUserDeactivated)
	default:
		return fmt.Errorf("RecordUserLogin: unknown status %s", status)
	}

	if tracker := getLoginTracker(); tracker != nil {
		tracker.Record(ctx, userID)
	} else {
		logUserTeamInfo("login tracker not configured; login write discarded", "tenant_id", resolvedTenantID, "user_id", userID)
	}
	return nil
}

// GetUser fetches a tenant-scoped user.
func GetUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
) (User, error) {
	if db == nil {
		return User{}, fmt.Errorf("GetUser: db is nil")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return User{}, fmt.Errorf("GetUser: userID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetUser")
	if err != nil {
		return User{}, fmt.Errorf("GetUser: %w", err)
	}

	var user User
	if err := db.GetContext(ctx, &user, `
		SELECT
			user_id::text AS user_id,
			tenant_id::text AS tenant_id,
			username,
			email,
			display_name,
			status,
			auth_provider,
			external_id,
			timezone,
			locale,
			last_login_at,
			metadata,
			created_at,
			updated_at
		FROM users
		WHERE tenant_id = $1::uuid
		  AND user_id = $2::uuid
	`, resolvedTenantID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, fmt.Errorf("GetUser: %w", ErrUserNotFound)
		}
		return User{}, fmt.Errorf("GetUser: query user: %w", err)
	}
	logUserTeamInfo("user fetched", "tenant_id", resolvedTenantID, "user_id", userID)
	return user, nil
}

// GetUserByExternalID resolves OIDC/SAML principal correlation.
func GetUserByExternalID(
	ctx context.Context,
	db *sqlx.DB,
	externalID string,
	tenantID string,
) (User, error) {
	if db == nil {
		return User{}, fmt.Errorf("GetUserByExternalID: db is nil")
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return User{}, fmt.Errorf("GetUserByExternalID: externalID is required")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "GetUserByExternalID")
	if err != nil {
		return User{}, fmt.Errorf("GetUserByExternalID: %w", err)
	}

	var user User
	if err := db.GetContext(ctx, &user, `
		SELECT
			user_id::text AS user_id,
			tenant_id::text AS tenant_id,
			username,
			email,
			display_name,
			status,
			auth_provider,
			external_id,
			timezone,
			locale,
			last_login_at,
			metadata,
			created_at,
			updated_at
		FROM users
		WHERE tenant_id = $1::uuid
		  AND external_id = $2
	`, resolvedTenantID, externalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, fmt.Errorf("GetUserByExternalID: %w", ErrUserNotFound)
		}
		return User{}, fmt.Errorf("GetUserByExternalID: query user: %w", err)
	}
	logUserTeamInfo("user fetched by external id", "tenant_id", resolvedTenantID, "user_id", user.UserID)
	return user, nil
}

// ListUsers returns paginated user list with optional filters.
func ListUsers(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filters ListUsersFilters,
	page, size int,
) ([]User, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListUsers: db is nil")
	}
	resolvedTenantID, err := resolveTenantIDForOperation(ctx, tenantID, "ListUsers")
	if err != nil {
		return nil, 0, fmt.Errorf("ListUsers: %w", err)
	}
	page, size = sanitizePagination(page, size)
	offset := (page - 1) * size

	args := make([]interface{}, 0, 8)
	args = append(args, resolvedTenantID)
	where := []string{"u.tenant_id = $1::uuid"}

	if filters.Status != nil && normalizeUserStatus(*filters.Status) != "" {
		args = append(args, string(normalizeUserStatus(*filters.Status)))
		where = append(where, fmt.Sprintf("u.status = $%d", len(args)))
	}
	if strings.TrimSpace(filters.TeamID) != "" {
		args = append(args, strings.TrimSpace(filters.TeamID))
		where = append(where, fmt.Sprintf("tm.team_id = $%d::uuid", len(args)))
	}
	if strings.TrimSpace(filters.RoleCode) != "" {
		args = append(args, strings.ToUpper(strings.TrimSpace(filters.RoleCode)))
		where = append(where, fmt.Sprintf("r.role_code = $%d", len(args)))
	}
	if strings.TrimSpace(filters.Search) != "" {
		args = append(args, "%"+strings.TrimSpace(filters.Search)+"%")
		where = append(where, fmt.Sprintf("(u.display_name ILIKE $%d OR u.email ILIKE $%d)", len(args), len(args)))
	}

	args = append(args, size, offset)
	query := fmt.Sprintf(`
		WITH filtered AS (
			SELECT DISTINCT u.user_id
			FROM users u
			LEFT JOIN team_members tm
			  ON tm.tenant_id = u.tenant_id
			 AND tm.user_id = u.user_id
			LEFT JOIN user_roles ur
			  ON ur.tenant_id = u.tenant_id
			 AND ur.user_id = u.user_id
			LEFT JOIN roles r
			  ON r.tenant_id = ur.tenant_id
			 AND r.role_id = ur.role_id
			WHERE %s
		)
		SELECT
			u.user_id::text AS user_id,
			u.tenant_id::text AS tenant_id,
			u.username,
			u.email,
			u.display_name,
			u.status,
			u.auth_provider,
			u.external_id,
			u.timezone,
			u.locale,
			u.last_login_at,
			u.metadata,
			u.created_at,
			u.updated_at,
			COUNT(*) OVER() AS total_count
		FROM filtered f
		JOIN users u
		  ON u.user_id = f.user_id
		ORDER BY u.display_name ASC, u.created_at DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), len(args)-1, len(args))

	type row struct {
		User
		TotalCount int `db:"total_count"`
	}
	result := make([]row, 0)
	if err := db.SelectContext(ctx, &result, query, args...); err != nil {
		return nil, 0, fmt.Errorf("ListUsers: query users: %w", err)
	}

	users := make([]User, 0, len(result))
	total := 0
	for i := range result {
		if i == 0 {
			total = result[i].TotalCount
		}
		users = append(users, result[i].User)
	}

	logUserTeamInfo("users listed", "tenant_id", resolvedTenantID, "count", len(users), "total", total, "page", page, "size", size)
	return users, total, nil
}
