package exception

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestOperatorExceptionDashboardSupport_HappyPath(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	now := time.Now().UTC()
	caseRows := sqlmock.NewRows([]string{
		"case_id", "reference_number", "case_type_id", "status", "exception_at", "exception_reason", "exception_severity", "exception_task_id", "task_definition_code", "last_error_code", "created_at",
	}).AddRow("case-1", "LOAN-2026-00001", "ct-1", "EXCEPTION", now, "failed", "BLOCKING", "task-1", "CREDIT_CHECK", "DOWNSTREAM_TIMEOUT", now)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\tc.id::text AS case_id")).WithArgs(10).WillReturnRows(caseRows)

	dlqRows := sqlmock.NewRows([]string{
		"dlq_id", "task_id", "case_id", "failure_reason", "error_detail", "moved_at", "requeue_count", "last_requeue_at", "is_poison_pill", "quarantine_released_at", "soft_deleted_at", "created_at",
	}).AddRow("dlq-1", "task-1", "case-1", "failed", []byte(`{"error_code":"DOWNSTREAM_TIMEOUT"}`), now, 0, nil, false, nil, nil, now)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\tdlq_id::text AS dlq_id")).WithArgs("case-1").WillReturnRows(dlqRows)

	historyRows := sqlmock.NewRows([]string{
		"attempt_id", "task_id", "case_id", "attempt_number", "retry_count_before", "max_retries", "backoff_strategy", "base_interval_seconds", "max_interval_seconds", "computed_interval_seconds", "scheduled_at", "next_attempt_at", "error_code", "error_class", "source_service", "outcome",
	}).AddRow("att-1", "task-1", "case-1", 1, 0, 3, "EXPONENTIAL", 5, 60, 5, now, now.Add(5*time.Second), "DOWNSTREAM_TIMEOUT", "TRANSIENT", "credit-service", "RETRY_SCHEDULED")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\tattempt_id::text AS attempt_id")).WithArgs("task-1").WillReturnRows(historyRows)

	cases, err := ListExceptionCases(context.Background(), db, 10)
	assert.NoError(t, err)
	assert.Len(t, cases, 1)

	dlq, err := GetDLQEntries(context.Background(), db, "case-1")
	assert.NoError(t, err)
	assert.Len(t, dlq, 1)

	history, err := GetRetryHistory(context.Background(), db, "task-1")
	assert.NoError(t, err)
	assert.Len(t, history, 1)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOperatorExceptionDashboardSupport_EdgeCase(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\tc.id::text AS case_id")).WithArgs(100).WillReturnRows(sqlmock.NewRows([]string{
		"case_id", "reference_number", "case_type_id", "status", "exception_at", "exception_reason", "exception_severity", "exception_task_id", "task_definition_code", "last_error_code", "created_at",
	}))

	cases, err := ListExceptionCases(context.Background(), db, 0)
	assert.NoError(t, err)
	assert.Len(t, cases, 0)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOperatorExceptionDashboardSupport_FailureMode(t *testing.T) {
	_, err := ListExceptionCases(context.Background(), nil, 10)
	assert.Error(t, err)

	_, err = GetDLQEntries(context.Background(), nil, "case-1")
	assert.Error(t, err)

	_, err = GetRetryHistory(context.Background(), nil, "task-1")
	assert.Error(t, err)
}

func TestDeadLetterQueue_HappyPath(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\ttask_id::text AS task_id")).WithArgs("dlq-1").WillReturnRows(sqlmock.NewRows([]string{"task_id", "case_id", "requeue_count", "is_poison_pill"}).AddRow("task-1", "case-1", 0, false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT retry_count, max_retries, status")).WithArgs("task-1").WillReturnRows(sqlmock.NewRows([]string{"retry_count", "max_retries", "status"}).AddRow(3, 3, "FAILED"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()::text")).WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow("uuid-1"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tasks")).WithArgs("requeue:task-1:uuid-1", "task-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE task_dlq")).WithArgs("dlq-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO task_retry_history")).WithArgs("task-1", "case-1", 3, 3, sqlmock.AnyArg(), "operator-1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).
		WithArgs(sqlmock.AnyArg(), "case-1", "task-1", "TASK_REQUEUED", sqlmock.AnyArg(), "PENDING", "case-orchestrator", 5, "case-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs(sqlmock.AnyArg(), "TASK_REQUEUED", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = RequeueDLQEntry(context.Background(), tx, "dlq-1", "operator-1")
	assert.NoError(t, err)

	mock.ExpectCommit()
	assert.NoError(t, tx.Commit())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterQueue_EdgeCase(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\ttask_id::text AS task_id")).WithArgs("dlq-2").WillReturnRows(sqlmock.NewRows([]string{"task_id", "case_id", "requeue_count", "is_poison_pill"}).AddRow("task-2", "case-2", 4, true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT retry_count, max_retries, status")).WithArgs("task-2").WillReturnRows(sqlmock.NewRows([]string{"retry_count", "max_retries", "status"}).AddRow(5, 5, "FAILED"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()::text")).WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow("uuid-2"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tasks")).WithArgs("requeue:task-2:uuid-2", "task-2").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE task_dlq")).WithArgs("dlq-2").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO task_retry_history")).WithArgs("task-2", "case-2", 5, 5, sqlmock.AnyArg(), "operator-2").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).
		WithArgs(sqlmock.AnyArg(), "case-2", "task-2", "TASK_REQUEUED", sqlmock.AnyArg(), "PENDING", "case-orchestrator", 5, "case-2", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs(sqlmock.AnyArg(), "TASK_REQUEUED", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = RequeueDLQEntry(context.Background(), tx, "dlq-2", "operator-2")
	assert.NoError(t, err)
	mock.ExpectCommit()
	assert.NoError(t, tx.Commit())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeadLetterQueue_FailureMode(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT\n\t\t\ttask_id::text AS task_id")).WithArgs("missing-dlq").WillReturnError(sql.ErrNoRows)
	err = RequeueDLQEntry(context.Background(), tx, "missing-dlq", "operator-3")
	assert.Error(t, err)

	mock.ExpectRollback()
	_ = tx.Rollback()
	assert.NoError(t, mock.ExpectationsWereMet())
}
