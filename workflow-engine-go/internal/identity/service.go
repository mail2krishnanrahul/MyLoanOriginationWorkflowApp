package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// IdentityService provides core registry operations for users, roles, and teams.
type IdentityService struct {
	db        *sqlx.DB
	logger    *slog.Logger
	publisher EventPublisher
}

// EventPublisher is a minimal interface matching the project's typical outbox publisher.
type EventPublisher interface {
	PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error
}

func NewIdentityService(db *sqlx.DB, logger *slog.Logger, publisher EventPublisher) *IdentityService {
	return &IdentityService{
		db:        db,
		logger:    logger,
		publisher: publisher,
	}
}

// ---------------------------------------------------------------------------
// 1. User Lifecycle Management
// ---------------------------------------------------------------------------

func (s *IdentityService) CreateUser(
	ctx context.Context,
	db *sqlx.DB,
	input model.CreateUserInput,
) (model.User, error) {
	if db == nil {
		return model.User{}, fmt.Errorf("CreateUser: db is nil")
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return model.User{}, fmt.Errorf("CreateUser: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Enforce default status
	status := model.UserStatusActive
	authProvider := input.AuthProvider
	if authProvider == "" {
		authProvider = model.AuthProviderLocal
	}

	timezone := input.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	locale := input.Locale
	if locale == "" {
		locale = "en-US"
	}

	meta := input.Metadata
	if meta == nil {
		meta = []byte("{}")
	}

	var user model.User
	err = tx.GetContext(ctx, &user, `
		INSERT INTO users (
			tenant_id,
			username,
			email,
			display_name,
			status,
			auth_provider,
			external_id,
			timezone,
			locale,
			metadata
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb
		)
		RETURNING
			user_id,
			tenant_id,
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
		status,
		authProvider,
		input.ExternalID,
		timezone,
		locale,
		meta,
	)

	if err != nil {
		// Detect specific unique constraint violations
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			switch pqErr.Constraint {
			case "uq_users_tenant_lower_username_000030":
				return model.User{}, model.ErrUsernameTaken
			case "uq_users_tenant_lower_email_000030":
				return model.User{}, model.ErrEmailTaken
			case "uq_users_tenant_external_id_000030":
				return model.User{}, model.ErrExternalIDConflict
			}
		}
		return model.User{}, fmt.Errorf("CreateUser: insert: %w", err)
	}

	// Publish USER_CREATED event
	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":  user.UserID,
		"username": user.Username,
		"email":    user.Email,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  user.TenantID,
		EventType: "USER_CREATED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return model.User{}, fmt.Errorf("CreateUser: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.User{}, fmt.Errorf("CreateUser: commit tx: %w", err)
	}

	s.logger.Info("user created", "tenant_id", user.TenantID, "user_id", user.UserID)
	return user, nil
}

func (s *IdentityService) SuspendUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	suspendedBy string,
	reason string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("SuspendUser: begin tx: %w", err)
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.GetContext(ctx, &currentStatus, `
		SELECT status FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE
	`, userID, tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrUserNotFound
		}
		return fmt.Errorf("SuspendUser: get status: %w", err)
	}

	if currentStatus == string(model.UserStatusDeactivated) {
		return model.ErrUserDeactivated
	}
	if currentStatus == string(model.UserStatusSuspended) {
		return nil // idempotent
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE users
		SET status = 'SUSPENDED', updated_at = now()
		WHERE user_id = $1::uuid AND tenant_id = $2::uuid
	`, userID, tenantID)
	if err != nil {
		return fmt.Errorf("SuspendUser: update status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":      userID,
		"suspended_by": suspendedBy,
		"reason":       reason,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "USER_SUSPENDED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("SuspendUser: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("SuspendUser: commit tx: %w", err)
	}

	s.logger.Info("user suspended", "tenant_id", tenantID, "user_id", userID, "suspended_by", suspendedBy)
	return nil
}

func (s *IdentityService) DeactivateUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	deactivatedBy string,
	reason string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("DeactivateUser: begin tx: %w", err)
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.GetContext(ctx, &currentStatus, `
		SELECT status FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE
	`, userID, tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrUserNotFound
		}
		return fmt.Errorf("DeactivateUser: get status: %w", err)
	}

	if currentStatus == string(model.UserStatusDeactivated) {
		return nil // idempotent
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE users
		SET status = 'DEACTIVATED', updated_at = now()
		WHERE user_id = $1::uuid AND tenant_id = $2::uuid
	`, userID, tenantID)
	if err != nil {
		return fmt.Errorf("DeactivateUser: update status: %w", err)
	}

	// Find and unassign all open tasks
	var openTaskIDs []string
	err = tx.SelectContext(ctx, &openTaskIDs, `
		SELECT id::text FROM tasks
		WHERE tenant_id = $1::uuid
		  AND assigned_user_id = $2::uuid
		  AND status IN ('PENDING', 'IN_PROGRESS', 'AWAITING_EXTERNAL')
		FOR UPDATE
	`, tenantID, userID)
	if err != nil {
		return fmt.Errorf("DeactivateUser: get open tasks: %w", err)
	}

	if len(openTaskIDs) > 0 {
		_, err = tx.ExecContext(ctx, `
			UPDATE tasks
			SET assigned_user_id = NULL,
			    status = CASE WHEN status = 'IN_PROGRESS' THEN 'PENDING' ELSE status END,
			    updated_at = now()
			WHERE id = ANY($1::uuid[])
		`, pq.Array(openTaskIDs))
		if err != nil {
			return fmt.Errorf("DeactivateUser: unassign tasks: %w", err)
		}

		for _, taskID := range openTaskIDs {
			evtPayload, _ := json.Marshal(map[string]interface{}{
				"task_id":       taskID,
				"unassigned_by": deactivatedBy,
				"reason":        "USER_DEACTIVATED",
			})
			if err := s.publisher.PublishEvent(ctx, tx, model.Event{
				TenantID:  tenantID,
				EventType: "TASK_UNASSIGNED",
				Payload:   evtPayload,
				Status:    model.EventStatusPending,
			}); err != nil {
				return fmt.Errorf("DeactivateUser: publish task_unassigned event: %w", err)
			}
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":        userID,
		"deactivated_by": deactivatedBy,
		"reason":         reason,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "USER_DEACTIVATED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("DeactivateUser: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DeactivateUser: commit tx: %w", err)
	}

	s.logger.Info("user deactivated", "tenant_id", tenantID, "user_id", userID, "open_tasks_unassigned", len(openTaskIDs))
	return nil
}

func (s *IdentityService) ReactivateUser(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	reactivatedBy string,
) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ReactivateUser: begin tx: %w", err)
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.GetContext(ctx, &currentStatus, `
		SELECT status FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE
	`, userID, tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.ErrUserNotFound
		}
		return fmt.Errorf("ReactivateUser: get status: %w", err)
	}

	if currentStatus == string(model.UserStatusDeactivated) {
		return model.ErrUserDeactivated
	}
	if currentStatus == string(model.UserStatusActive) {
		return nil // idempotent
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE users
		SET status = 'ACTIVE', updated_at = now()
		WHERE user_id = $1::uuid AND tenant_id = $2::uuid
	`, userID, tenantID)
	if err != nil {
		return fmt.Errorf("ReactivateUser: update status: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"user_id":        userID,
		"reactivated_by": reactivatedBy,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "USER_REACTIVATED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("ReactivateUser: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ReactivateUser: commit tx: %w", err)
	}

	s.logger.Info("user reactivated", "tenant_id", tenantID, "user_id", userID)
	return nil
}

func (s *IdentityService) UpdateUserProfile(
	ctx context.Context,
	db *sqlx.DB,
	userID string,
	tenantID string,
	input model.UpdateUserProfileInput,
) (model.User, error) {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return model.User{}, fmt.Errorf("UpdateUserProfile: begin tx: %w", err)
	}
	defer tx.Rollback()

	var user model.User
	err = tx.GetContext(ctx, &user, `
		SELECT * FROM users WHERE user_id = $1::uuid AND tenant_id = $2::uuid FOR UPDATE
	`, userID, tenantID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, model.ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("UpdateUserProfile: get user: %w", err)
	}

	if input.DisplayName != nil {
		user.DisplayName = *input.DisplayName
	}
	if input.Timezone != nil {
		user.Timezone = *input.Timezone
	}
	if input.Locale != nil {
		user.Locale = *input.Locale
	}
	if input.Metadata != nil {
		user.Metadata = input.Metadata
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET display_name = $1, timezone = $2, locale = $3, metadata = $4::jsonb, updated_at = now()
		WHERE user_id = $5::uuid AND tenant_id = $6::uuid
		RETURNING updated_at
	`, user.DisplayName, user.Timezone, user.Locale, user.Metadata, userID, tenantID).Scan(&user.UpdatedAt)
	if err != nil {
		return model.User{}, fmt.Errorf("UpdateUserProfile: update user: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"user_id": userID,
	})
	if err := s.publisher.PublishEvent(ctx, tx, model.Event{
		TenantID:  tenantID,
		EventType: "USER_PROFILE_UPDATED",
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return model.User{}, fmt.Errorf("UpdateUserProfile: publish event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.User{}, fmt.Errorf("UpdateUserProfile: commit tx: %w", err)
	}

	return user, nil
}
