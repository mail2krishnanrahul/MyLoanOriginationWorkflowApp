package multitenancy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

var (
	emailBasicPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	localePattern     = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

func resolveTenantIDForOperation(ctx context.Context, tenantID string, fn string) (string, error) {
	fromCtx, ctxErr := TenantFromContext(ctx)
	tenantID = strings.TrimSpace(tenantID)
	if ctxErr == nil {
		if tenantID == "" {
			tenantID = fromCtx
		} else if tenantID != fromCtx {
			return "", fmt.Errorf("%s: tenant mismatch context=%s input=%s", fn, fromCtx, tenantID)
		}
	}
	if tenantID == "" {
		if ctxErr != nil {
			return "", fmt.Errorf("%s: %w", fn, ctxErr)
		}
		return "", fmt.Errorf("%s: %w", fn, ErrTenantNotFound)
	}
	return tenantID, nil
}

func normalizeUserStatus(status UserStatus) UserStatus {
	s := UserStatus(strings.ToUpper(strings.TrimSpace(string(status))))
	switch s {
	case UserStatusActive, UserStatusSuspended, UserStatusDeactivated:
		return s
	default:
		return ""
	}
}

func normalizeAuthProvider(provider AuthProvider) AuthProvider {
	p := AuthProvider(strings.ToUpper(strings.TrimSpace(string(provider))))
	switch p {
	case AuthProviderLocal, AuthProviderOIDC, AuthProviderSAML:
		return p
	default:
		return ""
	}
}

func normalizeTeamStatus(status TeamStatus) TeamStatus {
	s := TeamStatus(strings.ToUpper(strings.TrimSpace(string(status))))
	switch s {
	case TeamStatusActive, TeamStatusDisbanded:
		return s
	default:
		return ""
	}
}

func normalizeTeamType(teamType TeamType) TeamType {
	t := TeamType(strings.ToUpper(strings.TrimSpace(string(teamType))))
	switch t {
	case TeamTypeProcessing, TeamTypeUnderwriting, TeamTypeApproval, TeamTypeOperations, TeamTypeMixed:
		return t
	default:
		return ""
	}
}

func normalizeTeamMemberRole(role TeamMemberRole) TeamMemberRole {
	r := TeamMemberRole(strings.ToUpper(strings.TrimSpace(string(role))))
	switch r {
	case TeamMemberRoleMember, TeamMemberRoleLead, TeamMemberRoleManager:
		return r
	default:
		return ""
	}
}

func normalizeUsername(username string) string {
	return strings.TrimSpace(username)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeDisplayName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeExternalID(externalID *string) *string {
	if externalID == nil {
		return nil
	}
	v := strings.TrimSpace(*externalID)
	if v == "" {
		return nil
	}
	return &v
}

func normalizeTimezone(timezone string) string {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return "UTC"
	}
	return timezone
}

func normalizeLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "en-US"
	}
	return locale
}

func sanitizePagination(page int, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}

func validateUserCreateInput(input *CreateUserInput) error {
	if input == nil {
		return fmt.Errorf("validateUserCreateInput: input is nil")
	}
	input.Username = normalizeUsername(input.Username)
	input.Email = normalizeEmail(input.Email)
	input.DisplayName = normalizeDisplayName(input.DisplayName)
	input.Status = normalizeUserStatus(input.Status)
	if input.Status == "" {
		input.Status = UserStatusActive
	}
	input.AuthProvider = normalizeAuthProvider(input.AuthProvider)
	if input.AuthProvider == "" {
		input.AuthProvider = AuthProviderLocal
	}
	input.ExternalID = normalizeExternalID(input.ExternalID)
	input.Timezone = normalizeTimezone(input.Timezone)
	input.Locale = normalizeLocale(input.Locale)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.CreatedBy == "" {
		input.CreatedBy = "system"
	}

	if input.Username == "" {
		return fmt.Errorf("validateUserCreateInput: username is required")
	}
	if input.Email == "" || !emailBasicPattern.MatchString(input.Email) {
		return fmt.Errorf("validateUserCreateInput: email is invalid")
	}
	if input.DisplayName == "" {
		return fmt.Errorf("validateUserCreateInput: display_name is required")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return fmt.Errorf("validateUserCreateInput: timezone is invalid: %w", err)
	}
	if !localePattern.MatchString(input.Locale) {
		return fmt.Errorf("validateUserCreateInput: locale is invalid")
	}
	if len(input.Metadata) == 0 {
		input.Metadata = json.RawMessage(`{}`)
	}
	return nil
}

func validateUserProfileInput(input *UpdateUserProfileInput) error {
	if input == nil {
		return fmt.Errorf("validateUserProfileInput: input is nil")
	}
	input.DisplayName = normalizeDisplayName(input.DisplayName)
	input.Timezone = normalizeTimezone(input.Timezone)
	input.Locale = normalizeLocale(input.Locale)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if input.UpdatedBy == "" {
		input.UpdatedBy = "system"
	}

	if input.DisplayName == "" {
		return fmt.Errorf("validateUserProfileInput: display_name is required")
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return fmt.Errorf("validateUserProfileInput: timezone is invalid: %w", err)
	}
	if !localePattern.MatchString(input.Locale) {
		return fmt.Errorf("validateUserProfileInput: locale is invalid")
	}
	if len(input.Metadata) == 0 {
		input.Metadata = json.RawMessage(`{}`)
	}
	return nil
}

func validateCreateTeamInput(input *CreateTeamInput) error {
	if input == nil {
		return fmt.Errorf("validateCreateTeamInput: input is nil")
	}
	input.TeamCode = strings.ToUpper(strings.TrimSpace(input.TeamCode))
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.TeamType = normalizeTeamType(input.TeamType)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.CreatedBy == "" {
		input.CreatedBy = "system"
	}
	if input.TeamCode == "" {
		return fmt.Errorf("validateCreateTeamInput: team_code is required")
	}
	if input.DisplayName == "" {
		return fmt.Errorf("validateCreateTeamInput: display_name is required")
	}
	if input.TeamType == "" {
		return fmt.Errorf("validateCreateTeamInput: team_type is required")
	}
	if len(input.Metadata) == 0 {
		input.Metadata = json.RawMessage(`{}`)
	}
	return nil
}

func beginSQLXTx(ctx context.Context, db *sqlx.DB, fn string) (*sqlx.Tx, error) {
	if db == nil {
		return nil, fmt.Errorf("%s: db is nil", fn)
	}
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", fn, err)
	}
	return tx, nil
}

func publishUserTeamEventTx(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	caseID *string,
	taskID *string,
	eventType model.EventType,
	payload map[string]interface{},
) error {
	if tx == nil {
		return fmt.Errorf("publishUserTeamEventTx: tx is nil")
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["tenant_id"] = tenantID
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publishUserTeamEventTx: marshal payload: %w", err)
	}
	return PublishEvent(ctx, tx, model.Event{
		TenantID:      tenantID,
		CaseID:        caseID,
		TaskID:        taskID,
		EventType:     eventType,
		Payload:       raw,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
		MaxAttempts:   5,
	})
}

func isUniqueViolation(err error) (*pgconn.PgError, bool) {
	if err == nil {
		return nil, false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr, true
	}
	return nil, false
}

func mapUserInsertError(err error) error {
	if pgErr, ok := isUniqueViolation(err); ok {
		switch strings.TrimSpace(pgErr.ConstraintName) {
		case "uq_users_tenant_lower_username_000030":
			return ErrUsernameTaken
		case "uq_users_tenant_lower_email_000030":
			return ErrEmailTaken
		case "uq_users_tenant_external_id_000030":
			return ErrExternalIDConflict
		}
	}
	return err
}

func mapRoleInsertError(err error) error {
	if pgErr, ok := isUniqueViolation(err); ok {
		switch strings.TrimSpace(pgErr.ConstraintName) {
		case "uq_roles_tenant_code_000030":
			return ErrRoleTenantMismatch
		}
	}
	return err
}

func actorUserIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	for _, key := range []string{"actor_user_id", "user_id", "userID", "x_user_id"} {
		if v := ctx.Value(key); v != nil {
			if userID, ok := v.(string); ok {
				userID = strings.TrimSpace(userID)
				if userID != "" {
					return userID
				}
			}
		}
	}
	return ""
}

func logUserTeamInfo(msg string, attrs ...any) {
	slog.Info(msg, attrs...)
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}
