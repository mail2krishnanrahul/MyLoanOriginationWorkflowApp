package identity

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockPublisher struct {
	mock.Mock
}

func (m *MockPublisher) PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	args := m.Called(ctx, tx, event)
	return args.Error(0)
}

func setupTestDB() (*sqlx.DB, sqlmock.Sqlmock, *MockPublisher, *IdentityService) {
	mockDB, mockSQL, _ := sqlmock.New()
	sqlxDB := sqlx.NewDb(mockDB, "sqlmock")
	pub := new(MockPublisher)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewIdentityService(sqlxDB, logger, pub) // nil logger causes panic on created
	return sqlxDB, mockSQL, pub, svc
}

func TestIdentityService_CreateUser(t *testing.T) {
	db, mockSQL, pub, svc := setupTestDB()
	defer db.Close()

	ctx := context.Background()

	input := model.CreateUserInput{
		TenantID:    "00000000-0000-0000-0000-000000000001",
		Username:    "testuser",
		Email:       "test@example.com",
		DisplayName: "Test User",
	}

	mockSQL.ExpectBegin()
	mockSQL.ExpectQuery(`INSERT INTO users`).
		WithArgs(
			input.TenantID, input.Username, input.Email, input.DisplayName,
			model.UserStatusActive, model.AuthProviderLocal, nil, "UTC", "en-US", []byte("{}"),
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "tenant_id", "username", "email", "display_name", "status",
			"auth_provider", "external_id", "timezone", "locale", "last_login_at",
			"metadata", "created_at", "updated_at",
		}).AddRow(
			"11111111-1111-1111-1111-111111111111", input.TenantID, input.Username, input.Email, input.DisplayName,
			model.UserStatusActive, model.AuthProviderLocal, nil, "UTC", "en-US", nil,
			[]byte("{}"), time.Now(), time.Now(),
		))

	pub.On("PublishEvent", mock.Anything, mock.Anything, mock.MatchedBy(func(e model.Event) bool {
		return e.EventType == "USER_CREATED" && e.TenantID == input.TenantID
	})).Return(nil)

	mockSQL.ExpectCommit()

	user, err := svc.CreateUser(ctx, db, input)
	assert.NoError(t, err)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", user.UserID)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, model.UserStatusActive, user.Status)

	assert.NoError(t, mockSQL.ExpectationsWereMet())
	pub.AssertExpectations(t)
}

func TestIdentityService_SuspendUser(t *testing.T) {
	db, mockSQL, pub, svc := setupTestDB()
	defer db.Close()

	ctx := context.Background()
	userID := "11111111-1111-1111-1111-111111111111"
	tenantID := "00000000-0000-0000-0000-000000000001"

	mockSQL.ExpectBegin()
	mockSQL.ExpectQuery(`SELECT status FROM users WHERE user_id = \$1::uuid AND tenant_id = \$2::uuid FOR UPDATE`).
		WithArgs(userID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("ACTIVE"))

	mockSQL.ExpectExec(`UPDATE users SET status = 'SUSPENDED', updated_at = now\(\) WHERE user_id = \$1::uuid AND tenant_id = \$2::uuid`).
		WithArgs(userID, tenantID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	pub.On("PublishEvent", mock.Anything, mock.Anything, mock.MatchedBy(func(e model.Event) bool {
		return e.EventType == "USER_SUSPENDED"
	})).Return(nil)

	mockSQL.ExpectCommit()

	err := svc.SuspendUser(ctx, db, userID, tenantID, "admin", "violation")
	assert.NoError(t, err)
	assert.NoError(t, mockSQL.ExpectationsWereMet())
}

func TestIdentityService_DeactivateUser(t *testing.T) {
	db, mockSQL, pub, svc := setupTestDB()
	defer db.Close()

	ctx := context.Background()
	userID := "11111111-1111-1111-1111-111111111111"
	tenantID := "00000000-0000-0000-0000-000000000001"

	mockSQL.ExpectBegin()
	mockSQL.ExpectQuery(`SELECT status FROM users WHERE user_id = \$1::uuid AND tenant_id = \$2::uuid FOR UPDATE`).
		WithArgs(userID, tenantID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("ACTIVE"))

	mockSQL.ExpectExec(`UPDATE users SET status = 'DEACTIVATED', updated_at = now\(\) WHERE user_id = \$1::uuid AND tenant_id = \$2::uuid`).
		WithArgs(userID, tenantID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Mock catching open tasks
	mockSQL.ExpectQuery(`SELECT id::text FROM tasks WHERE tenant_id = \$1::uuid AND assigned_user_id = \$2::uuid AND status IN \('PENDING', 'IN_PROGRESS', 'AWAITING_EXTERNAL'\) FOR UPDATE`).
		WithArgs(tenantID, userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("task-123").AddRow("task-456"))

	mockSQL.ExpectExec(`UPDATE tasks SET assigned_user_id = NULL, status = CASE WHEN status = 'IN_PROGRESS' THEN 'PENDING' ELSE status END, updated_at = now\(\) WHERE id = ANY\(\$1::uuid\[\]\)`).
		WillReturnResult(sqlmock.NewResult(1, 2))

	// Requires two UNASSIGNED events and one DEACTIVATED event
	pub.On("PublishEvent", mock.Anything, mock.Anything, mock.MatchedBy(func(e model.Event) bool {
		return e.EventType == "TASK_UNASSIGNED"
	})).Return(nil).Twice()

	pub.On("PublishEvent", mock.Anything, mock.Anything, mock.MatchedBy(func(e model.Event) bool {
		return e.EventType == "USER_DEACTIVATED"
	})).Return(nil).Once()

	mockSQL.ExpectCommit()

	err := svc.DeactivateUser(ctx, db, userID, tenantID, "admin", "leaving company")
	assert.NoError(t, err)
	assert.NoError(t, mockSQL.ExpectationsWereMet())
	pub.AssertExpectations(t)
}
