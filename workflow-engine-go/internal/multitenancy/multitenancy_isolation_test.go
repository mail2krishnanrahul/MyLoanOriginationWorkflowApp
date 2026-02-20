package multitenancy

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantIsolation_CaseQuery(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := WithTenant(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mock.ExpectQuery(`(?s)FROM cases`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("case-a"),
	)

	rows, err := ListTenantCaseIDs(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{"case-a"}, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantIsolation_TaskQuery(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := WithTenant(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mock.ExpectQuery(`(?s)FROM tasks`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("task-a"),
	)

	rows, err := ListTenantTaskIDs(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{"task-a"}, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantIsolation_EventQuery(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := WithTenant(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mock.ExpectQuery(`(?s)FROM events_outbox`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("event-a"),
	)

	rows, err := ListTenantEventIDs(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{"event-a"}, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantIsolation_NotificationQueue(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := WithTenant(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mock.ExpectQuery(`(?s)FROM notification_queue`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"id"}).AddRow("notif-a"),
	)

	rows, err := ListTenantNotificationIDs(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{"notif-a"}, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantIsolation_DLQQuery(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	ctx := WithTenant(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mock.ExpectQuery(`(?s)FROM task_dlq`).WithArgs("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(
		sqlmock.NewRows([]string{"dlq_id"}).AddRow("dlq-a"),
	)

	rows, err := ListTenantDLQIDs(ctx, db)
	require.NoError(t, err)
	assert.Equal(t, []string{"dlq-a"}, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantIsolation_WorkerPollDoesNotCrossLeak(t *testing.T) {
	ctx := WithTenant(context.Background(), "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	query := `
		SELECT id
		FROM tasks
		WHERE status = 'PENDING'
		  AND assigned_service = $1
		ORDER BY priority DESC
		LIMIT $2
		FOR UPDATE SKIP LOCKED`

	scoped, args, err := AssertTenantScope(ctx, query, []interface{}{"svc-a", 10})
	require.NoError(t, err)
	assert.Contains(t, scoped, "tenant_id = $3")
	require.Len(t, args, 3)
	assert.Equal(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", args[2])
}

func TestTenantIsolation_CaseTypeVisibility_Global(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT EXISTS (
			SELECT 1
			FROM case_types
			WHERE code = $1
			  AND status = 'ACTIVE'
			  AND (tenant_id IS NULL OR tenant_id = $2::uuid)
		)
	`)).WithArgs("HOME_LOAN", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	visible, err := IsCaseTypeVisibleToTenant(context.Background(), db, "HOME_LOAN", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	require.NoError(t, err)
	assert.True(t, visible)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTenantIsolation_CaseTypeVisibility_TenantSpecific(t *testing.T) {
	db, mock, cleanup := newSQLXMock(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT EXISTS (
			SELECT 1
			FROM case_types
			WHERE code = $1
			  AND status = 'ACTIVE'
			  AND (tenant_id IS NULL OR tenant_id = $2::uuid)
		)
	`)).WithArgs("HOME_LOAN", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	visible, err := IsCaseTypeVisibleToTenant(context.Background(), db, "HOME_LOAN", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	require.NoError(t, err)
	assert.False(t, visible)
	require.NoError(t, mock.ExpectationsWereMet())
}
