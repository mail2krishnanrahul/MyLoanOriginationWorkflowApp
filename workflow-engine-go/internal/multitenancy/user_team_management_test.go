package multitenancy

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearPermissionRequestCache() {
	permissionRequestCache.Range(func(key interface{}, _ interface{}) bool {
		permissionRequestCache.Delete(key)
		return true
	})
}

func baseCreateUserInput(tenantID string) CreateUserInput {
	return CreateUserInput{
		TenantID:     tenantID,
		Username:     "jdoe",
		Email:        "jdoe@example.com",
		DisplayName:  "John Doe",
		Status:       UserStatusActive,
		AuthProvider: AuthProviderOIDC,
		Timezone:     "Australia/Sydney",
		Locale:       "en-AU",
		Metadata:     []byte(`{"employee_id":"E-1"}`),
		CreatedBy:    "admin-user",
	}
}

func createUserReturnRows() *sqlmock.Rows {
	now := time.Now().UTC()
	return sqlmock.NewRows([]string{
		"user_id",
		"tenant_id",
		"username",
		"email",
		"display_name",
		"status",
		"auth_provider",
		"external_id",
		"timezone",
		"locale",
		"last_login_at",
		"metadata",
		"created_at",
		"updated_at",
	}).AddRow(
		"11111111-1111-1111-1111-111111111111",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"jdoe",
		"jdoe@example.com",
		"John Doe",
		"ACTIVE",
		"OIDC",
		nil,
		"Australia/Sydney",
		"en-AU",
		nil,
		[]byte(`{"employee_id":"E-1"}`),
		now,
		now,
	)
}

func TestCreateUser_DuplicateUsernameSameTenant(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO users`).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "uq_users_tenant_lower_username_000030"})
	mock.ExpectRollback()

	_, err := CreateUser(context.Background(), db, baseCreateUserInput("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUsernameTaken))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateUser_DuplicateUsernameDifferentTenantAllowed(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	input := baseCreateUserInput("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)INSERT INTO users`).
		WithArgs(
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
			input.Metadata,
		).
		WillReturnRows(createUserReturnRows())
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	user, err := CreateUser(context.Background(), db, input)
	require.NoError(t, err)
	assert.Equal(t, "jdoe", user.Username)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeactivateUser_ZeroOpenTasks(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status\s+FROM users`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "11111111-1111-1111-1111-111111111111").
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("ACTIVE"))
	mock.ExpectExec(`(?s)UPDATE users\s+SET status = 'DEACTIVATED'`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "11111111-1111-1111-1111-111111111111").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT id::text AS task_id`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "11111111-1111-1111-1111-111111111111").
		WillReturnRows(sqlmock.NewRows([]string{"task_id", "case_id"}))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1)) // USER_DEACTIVATED only
	mock.ExpectCommit()

	err := DeactivateUser(
		context.Background(),
		db,
		"11111111-1111-1111-1111-111111111111",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"admin",
		"left company",
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeactivateUser_FiveOpenTasksPublishesFiveUnassignedEvents(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"task_id", "case_id"})
	for _, taskID := range []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
		"00000000-0000-0000-0000-000000000004",
		"00000000-0000-0000-0000-000000000005",
	} {
		rows.AddRow(
			taskID,
			"22222222-2222-2222-2222-222222222222",
		)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status\s+FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("ACTIVE"))
	mock.ExpectExec(`(?s)UPDATE users\s+SET status = 'DEACTIVATED'`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT id::text AS task_id`).WillReturnRows(rows)
	mock.ExpectExec(`(?s)UPDATE tasks\s+SET assigned_user_id = NULL`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 5))
	for i := 0; i < 5; i++ {
		mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1)) // TASK_UNASSIGNED x5
	}
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1)) // USER_DEACTIVATED
	mock.ExpectCommit()

	err := DeactivateUser(
		context.Background(),
		db,
		"11111111-1111-1111-1111-111111111111",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"admin",
		"terminal",
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReactivateUser_DeactivatedNotAllowed(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status\s+FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("DEACTIVATED"))
	mock.ExpectRollback()

	err := ReactivateUser(
		context.Background(),
		db,
		"11111111-1111-1111-1111-111111111111",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"admin",
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserDeactivated))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRevokeRoleFromUser_LastRoleRevocationRejected(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT tenant_id::text\s+FROM roles`).
		WithArgs("33333333-3333-3333-3333-333333333333").
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	mock.ExpectQuery(`(?s)SELECT EXISTS`).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM user_roles`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()

	err := RevokeRoleFromUser(
		context.Background(),
		db,
		"11111111-1111-1111-1111-111111111111",
		"33333333-3333-3333-3333-333333333333",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"admin",
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrLastRoleRevocation))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAssignRoleToUser_IdempotentSecondAssignmentNoError(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectQuery(`(?s)SELECT EXISTS`).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
		mock.ExpectQuery(`(?s)SELECT tenant_id::text\s+FROM roles`).
			WillReturnRows(sqlmock.NewRows([]string{"tenant_id"}).AddRow("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
		if i == 0 {
			mock.ExpectExec(`(?s)INSERT INTO user_roles`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1))
		} else {
			mock.ExpectExec(`(?s)INSERT INTO user_roles`).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		mock.ExpectCommit()
	}

	err := AssignRoleToUser(
		context.Background(),
		db,
		"11111111-1111-1111-1111-111111111111",
		"33333333-3333-3333-3333-333333333333",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"99999999-9999-9999-9999-999999999999",
	)
	require.NoError(t, err)

	err = AssignRoleToUser(
		context.Background(),
		db,
		"11111111-1111-1111-1111-111111111111",
		"33333333-3333-3333-3333-333333333333",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"99999999-9999-9999-9999-999999999999",
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateTeam_ParentDepthThreeRejected(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	parentID := "44444444-4444-4444-4444-444444444444"
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)WITH RECURSIVE lineage`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", parentID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(3))
	mock.ExpectRollback()

	_, err := CreateTeam(context.Background(), db, CreateTeamInput{
		TenantID:     "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		TeamCode:     "SYDNEY_LOANS",
		DisplayName:  "Sydney Loans",
		TeamType:     TeamTypeProcessing,
		ParentTeamID: &parentID,
		CreatedBy:    "admin",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTeamHierarchyTooDeep))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDisbandTeam_OpenTasksRejectedWithCount(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT status, tenant_id::text AS tenant_id\s+FROM teams`).
		WillReturnRows(sqlmock.NewRows([]string{"status", "tenant_id"}).AddRow("ACTIVE", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM tasks`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
	mock.ExpectRollback()

	err := DisbandTeam(
		context.Background(),
		db,
		"55555555-5555-5555-5555-555555555555",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"admin",
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTeamHasOpenTasks))
	assert.Contains(t, err.Error(), "open_tasks=7")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimTask_ConcurrentOneWins(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	permissionRows := sqlmock.NewRows([]string{"permission"}).AddRow("TASK_CLAIM")

	// First claimer succeeds.
	mock.ExpectQuery(`(?s)SELECT status\s+FROM users`).WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("ACTIVE"))
	mock.ExpectQuery(`(?s)SELECT DISTINCT p.permission`).WillReturnRows(permissionRows)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT\s+case_id::text AS case_id`).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "status", "version", "assigned_team_id", "assigned_user_id"}).
			AddRow("22222222-2222-2222-2222-222222222222", "PENDING", 3, "77777777-7777-7777-7777-777777777777", nil))
	mock.ExpectQuery(`(?s)SELECT EXISTS`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`(?s)UPDATE tasks\s+SET assigned_user_id =`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// Second claimer loses optimistic update.
	mock.ExpectQuery(`(?s)SELECT status\s+FROM users`).WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("ACTIVE"))
	mock.ExpectQuery(`(?s)SELECT DISTINCT p.permission`).WillReturnRows(sqlmock.NewRows([]string{"permission"}).AddRow("TASK_CLAIM"))
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT\s+case_id::text AS case_id`).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "status", "version", "assigned_team_id", "assigned_user_id"}).
			AddRow("22222222-2222-2222-2222-222222222222", "PENDING", 3, "77777777-7777-7777-7777-777777777777", nil))
	mock.ExpectQuery(`(?s)SELECT EXISTS`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`(?s)UPDATE tasks\s+SET assigned_user_id =`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := ClaimTask(context.Background(), db, "task-1", "user-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	require.NoError(t, err)

	err = ClaimTask(context.Background(), db, "task-1", "user-2", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTaskAlreadyClaimed))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUnassignTask_PreservesAssignedTeam(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(`(?s)SELECT DISTINCT p.permission`).WillReturnRows(sqlmock.NewRows([]string{"permission"}).AddRow("TASK_REASSIGN"))
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT\s+case_id::text AS case_id`).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "status", "assigned_user_id", "assigned_team_id"}).
			AddRow("22222222-2222-2222-2222-222222222222", "IN_PROGRESS", "88888888-8888-8888-8888-888888888888", "99999999-9999-9999-9999-999999999999"))
	mock.ExpectExec(`(?s)UPDATE tasks\s+SET assigned_user_id = NULL,\s+status = 'PENDING'`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ctx := context.WithValue(context.Background(), "actor_user_id", "admin-user")
	err := UnassignTask(
		ctx,
		db,
		"task-1",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"admin-user",
		"manual balancing",
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserHasPermission_SupervisorUnionIncludesSeniorApprover(t *testing.T) {
	clearPermissionRequestCache()
	defer clearPermissionRequestCache()

	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock.ExpectQuery(`(?s)SELECT DISTINCT p.permission`).
		WillReturnRows(sqlmock.NewRows([]string{"permission"}).
			AddRow("APPROVAL_APPROVE").
			AddRow("APPROVAL_REJECT").
			AddRow("APPROVAL_ESCALATE").
			AddRow("TASK_REASSIGN"))

	hasApprove, err := UserHasPermission(ctx, db, "user-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", PermissionApprovalApprove)
	require.NoError(t, err)
	assert.True(t, hasApprove)

	// Second call should hit request-scoped cache, no extra DB query.
	hasEscalate, err := UserHasPermission(ctx, db, "user-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", PermissionApprovalEscalate)
	require.NoError(t, err)
	assert.True(t, hasEscalate)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAssertPermission_DeniedIncludesPermissionCode(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(`(?s)SELECT DISTINCT p.permission`).WillReturnRows(sqlmock.NewRows([]string{"permission"}))

	err := AssertPermission(context.Background(), db, "user-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", PermissionTeamManage)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPermissionDenied))
	assert.Contains(t, err.Error(), string(PermissionTeamManage))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserByExternalID_DifferentTenantReturnsNotFound(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(`(?s)FROM users\s+WHERE tenant_id = \$1::uuid\s+AND external_id = \$2`).
		WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "oidc-sub-1").
		WillReturnError(sql.ErrNoRows)

	_, err := GetUserByExternalID(context.Background(), db, "oidc-sub-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserNotFound))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUsers_SearchFilterMatchesPartialDisplayName(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)WITH filtered AS`).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id",
			"tenant_id",
			"username",
			"email",
			"display_name",
			"status",
			"auth_provider",
			"external_id",
			"timezone",
			"locale",
			"last_login_at",
			"metadata",
			"created_at",
			"updated_at",
			"total_count",
		}).AddRow(
			"11111111-1111-1111-1111-111111111111",
			"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			"a.smith",
			"alice.smith@example.com",
			"Alice Smith",
			"ACTIVE",
			"LOCAL",
			nil,
			"UTC",
			"en-US",
			nil,
			[]byte(`{}`),
			now,
			now,
			1,
		))

	users, total, err := ListUsers(
		context.Background(),
		db,
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		ListUsersFilters{Search: "Ali"},
		1,
		20,
	)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, 1, total)
	assert.Equal(t, "Alice Smith", users[0].DisplayName)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUsersWithWorkload_SingleQueryNoNPlusOne(t *testing.T) {
	clearPermissionRequestCache()
	defer clearPermissionRequestCache()

	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = context.WithValue(ctx, "actor_user_id", "supervisor-1")
	cacheUserPermissions(ctx, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "supervisor-1", map[PermissionCode]struct{}{
		PermissionReportOperational: {},
	})

	mock.ExpectQuery(regexp.QuoteMeta("WITH filtered_users AS")).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id",
			"username",
			"display_name",
			"email",
			"team_id",
			"pending_count",
			"in_progress_count",
			"completed_today_count",
			"sla_at_risk_count",
			"total_count",
		}).AddRow(
			"11111111-1111-1111-1111-111111111111",
			"u1",
			"User One",
			"u1@example.com",
			"",
			3,
			1,
			2,
			1,
			1,
		))

	rows, total, err := ListUsersWithWorkload(
		ctx,
		db,
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"",
		1,
		50,
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, total)
	require.NoError(t, mock.ExpectationsWereMet())
}
