package reporting

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func newSQLXMock(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	return sqlx.NewDb(sqlDB, "sqlmock"), mock
}

func TestCaseThroughputMetrics_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{"bucket_start", "created_count", "completed_count", "cancelled_count", "inflight_count"}).
		AddRow(from, int64(20), int64(10), int64(2), int64(8))
	mock.ExpectQuery(regexp.QuoteMeta("FROM case_throughput_snapshots")).
		WithArgs("HOME_LOAN", "DAILY", from, to).
		WillReturnRows(rows)

	got, err := GetCaseThroughput(context.Background(), db, "home_loan", from, to, MetricBucketDaily)
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, int64(20), got[0].CreatedCount)
	assert.Equal(t, int64(10), got[0].CompletedCount)
	assert.Equal(t, int64(2), got[0].CancelledCount)
	assert.Equal(t, int64(8), got[0].InFlightCount)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCaseThroughputMetrics_NoDataInRange(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM case_throughput_snapshots")).
		WithArgs("HOME_LOAN", "DAILY", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"bucket_start", "created_count", "completed_count", "cancelled_count", "inflight_count"}))

	got, err := GetCaseThroughput(context.Background(), db, "HOME_LOAN", from, to, MetricBucketDaily)
	assert.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestCaseThroughputMetrics_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM case_throughput_snapshots")).
		WithArgs("HOME_LOAN", "DAILY", from, to).
		WillReturnError(errors.New("db unavailable"))

	_, err := GetCaseThroughput(context.Background(), db, "HOME_LOAN", from, to, MetricBucketDaily)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStageFunnelAnalysis_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{
		"stage_code",
		"stage_ordinal",
		"in_flight_count",
		"avg_forward_dwell_seconds",
		"p95_forward_dwell_seconds",
		"forward_transition_count",
		"regression_count",
		"regression_rate_percent",
		"sla_threshold_seconds",
		"is_abnormal_dwell",
	}).AddRow("INITIAL_REVIEW", 1, int64(4), 1800.0, 2400.0, int64(50), int64(5), 10.0, 3600.0, false)

	mock.ExpectQuery(regexp.QuoteMeta("WITH selected_case_type AS")).
		WithArgs("HOME_LOAN").
		WillReturnRows(rows)

	got, err := GetStageFunnel(context.Background(), db, "HOME_LOAN")
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "INITIAL_REVIEW", got[0].StageCode)
	assert.Equal(t, int64(4), got[0].InFlightCount)
	assert.Equal(t, 10.0, got[0].RegressionRatePercent)
	assert.False(t, got[0].AbnormalDwellTime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStageFunnelAnalysis_RegressionOnlyTransitions(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	rows := sqlmock.NewRows([]string{
		"stage_code",
		"stage_ordinal",
		"in_flight_count",
		"avg_forward_dwell_seconds",
		"p95_forward_dwell_seconds",
		"forward_transition_count",
		"regression_count",
		"regression_rate_percent",
		"sla_threshold_seconds",
		"is_abnormal_dwell",
	}).AddRow("UNDERWRITING", 3, int64(2), 0.0, 0.0, int64(0), int64(3), 0.0, nil, false)

	mock.ExpectQuery(regexp.QuoteMeta("WITH selected_case_type AS")).
		WithArgs("HOME_LOAN").
		WillReturnRows(rows)

	got, err := GetStageFunnel(context.Background(), db, "HOME_LOAN")
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, int64(0), got[0].ForwardTransitionCount)
	assert.Equal(t, int64(3), got[0].RegressionCount)
	assert.Equal(t, 0.0, got[0].AvgForwardDwellSeconds)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStageFunnelAnalysis_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("WITH selected_case_type AS")).
		WithArgs("HOME_LOAN").
		WillReturnError(errors.New("query failed"))

	_, err := GetStageFunnel(context.Background(), db, "HOME_LOAN")
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskExecutionMetrics_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM task_metrics_snapshots")).
		WithArgs("CREDIT_CHECK", from, to, "DOCUMENT_SERVICE").
		WillReturnRows(sqlmock.NewRows([]string{
			"total_tasks",
			"completed_tasks",
			"failed_tasks",
			"retried_tasks",
			"dlq_tasks",
			"avg_execution_seconds",
			"p50_execution_seconds",
			"p95_execution_seconds",
			"p99_execution_seconds",
			"retry_rate_percent",
			"failure_rate_percent",
			"dlq_rate_percent",
		}).AddRow(int64(100), int64(96), int64(4), int64(8), int64(1), 13.5, 9.0, 25.0, 30.0, 8.0, 4.0, 1.0))

	got, err := GetTaskMetrics(context.Background(), db, "CREDIT_CHECK", "DOCUMENT_SERVICE", from, to)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), got.TotalTasks)
	assert.Equal(t, 25.0, got.P95ExecutionSeconds)
	assert.Equal(t, 4.0, got.FailureRatePercent)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskExecutionMetrics_EdgeCase(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 1, 23, 59, 59, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM task_metrics_snapshots")).
		WithArgs("CREDIT_CHECK", from, to, "").
		WillReturnRows(sqlmock.NewRows([]string{
			"total_tasks",
			"completed_tasks",
			"failed_tasks",
			"retried_tasks",
			"dlq_tasks",
			"avg_execution_seconds",
			"p50_execution_seconds",
			"p95_execution_seconds",
			"p99_execution_seconds",
			"retry_rate_percent",
			"failure_rate_percent",
			"dlq_rate_percent",
		}).AddRow(int64(0), int64(0), int64(0), int64(0), int64(0), 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0))

	got, err := GetTaskMetrics(context.Background(), db, "CREDIT_CHECK", "", from, to)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), got.TotalTasks)
	assert.Equal(t, 0.0, got.AvgExecutionSeconds)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskExecutionMetrics_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM task_metrics_snapshots")).
		WithArgs("CREDIT_CHECK", from, to, "").
		WillReturnError(errors.New("metrics table unavailable"))

	_, err := GetTaskMetrics(context.Background(), db, "CREDIT_CHECK", "", from, to)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSLAComplianceReporting_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 2, 20, 1, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)date_trunc\('day', COALESCE\(c.completed_at, c.created_at\)\)`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"metric_day", "completed_cases", "compliant_cases", "breached_cases"}).
			AddRow(time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC), int64(10), int64(8), int64(2)))

	mock.ExpectQuery(`(?s)c.case_due_at <= now\(\) \+ interval '4 hours'`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "reference_number", "status", "current_stage_code", "sla_deadline", "hours_to_deadline"}).
			AddRow("case-1", "LOAN-1", "IN_PROGRESS", "UNDERWRITING", now.Add(2*time.Hour), 2.0))

	mock.ExpectQuery(`(?s)c.completed_at > c.case_due_at`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "reference_number", "status", "current_stage_code", "sla_deadline", "completed_at", "breach_hours"}).
			AddRow("case-2", "LOAN-2", "COMPLETED", "APPROVAL", now.Add(-3*time.Hour), now, 3.0))

	got, err := GetSLAComplianceReport(context.Background(), db, "HOME_LOAN", from, to)
	assert.NoError(t, err)
	assert.Equal(t, int64(10), got.TotalCompletedCases)
	assert.Equal(t, int64(8), got.TotalCompliantCases)
	assert.Equal(t, 80.0, got.ComplianceRatePercent)
	assert.Len(t, got.AtRiskCases, 1)
	assert.Len(t, got.BreachedCases, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSLAComplianceReporting_AllInFlightCases(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)date_trunc\('day', COALESCE\(c.completed_at, c.created_at\)\)`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"metric_day", "completed_cases", "compliant_cases", "breached_cases"}))

	mock.ExpectQuery(`(?s)c.case_due_at <= now\(\) \+ interval '4 hours'`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "reference_number", "status", "current_stage_code", "sla_deadline", "hours_to_deadline"}).
			AddRow("case-1", "LOAN-1", "IN_PROGRESS", "INITIAL_REVIEW", time.Date(2026, 2, 19, 2, 0, 0, 0, time.UTC), 1.5))

	mock.ExpectQuery(`(?s)c.completed_at > c.case_due_at`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "reference_number", "status", "current_stage_code", "sla_deadline", "completed_at", "breach_hours"}))

	got, err := GetSLAComplianceReport(context.Background(), db, "HOME_LOAN", from, to)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), got.TotalCompletedCases)
	assert.Equal(t, 0.0, got.ComplianceRatePercent)
	assert.Len(t, got.AtRiskCases, 1)
	assert.Empty(t, got.BreachedCases)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSLAComplianceReporting_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)date_trunc\('day', COALESCE\(c.completed_at, c.created_at\)\)`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnError(errors.New("daily query failed"))

	_, err := GetSLAComplianceReport(context.Background(), db, "HOME_LOAN", from, to)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOperationalQueueDepth_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM tasks\s+WHERE status = 'PENDING'`).
		WillReturnRows(sqlmock.NewRows([]string{"assigned_service", "priority", "pending_count", "oldest_age_seconds"}).
			AddRow("DOCUMENT_SERVICE", 3, int64(7), int64(1200)))

	mock.ExpectQuery(`(?s)status IN \('PENDING', 'FAILED'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"assigned_service", "next_attempt_at", "retry_count"}).
			AddRow("DOCUMENT_SERVICE", time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC), int64(2)))

	mock.ExpectQuery(`(?s)FROM task_dlq d`).
		WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "depth"}).AddRow("HOME_LOAN", int64(1)))

	mock.ExpectQuery(`(?s)MAX\(EXTRACT\(EPOCH FROM \(now\(\) - created_at\)\)\)`).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(1200)))

	got, err := GetQueueDepth(context.Background(), db)
	assert.NoError(t, err)
	assert.Equal(t, int64(7), got.TotalPending)
	assert.Equal(t, int64(2), got.TotalRetry)
	assert.Equal(t, int64(1), got.TotalDLQ)
	assert.Equal(t, int64(1200), got.OldestPendingAgeSeconds)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOperationalQueueDepth_ZeroTasks(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM tasks\s+WHERE status = 'PENDING'`).
		WillReturnRows(sqlmock.NewRows([]string{"assigned_service", "priority", "pending_count", "oldest_age_seconds"}))
	mock.ExpectQuery(`(?s)status IN \('PENDING', 'FAILED'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"assigned_service", "next_attempt_at", "retry_count"}))
	mock.ExpectQuery(`(?s)FROM task_dlq d`).
		WillReturnRows(sqlmock.NewRows([]string{"case_type_code", "depth"}))
	mock.ExpectQuery(`(?s)MAX\(EXTRACT\(EPOCH FROM \(now\(\) - created_at\)\)\)`).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(int64(0)))

	got, err := GetQueueDepth(context.Background(), db)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), got.TotalPending)
	assert.Equal(t, int64(0), got.TotalRetry)
	assert.Equal(t, int64(0), got.TotalDLQ)
	assert.Equal(t, int64(0), got.OldestPendingAgeSeconds)
	assert.NotNil(t, got.PendingByServicePriority)
	assert.NotNil(t, got.RetryQueue)
	assert.NotNil(t, got.DLQDepthByCaseType)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOperationalQueueDepth_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM tasks\s+WHERE status = 'PENDING'`).
		WillReturnError(errors.New("pending query failed"))

	_, err := GetQueueDepth(context.Background(), db)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventStreamAuditLog_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM events_outbox`).
		WithArgs("case-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	mock.ExpectQuery(`(?s)FROM events_outbox\s+WHERE case_id = \$1::uuid`).
		WithArgs("case-1", 50, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "case_id", "task_id", "event_type", "payload", "status", "target_service", "created_at", "delivered_at"}).
			AddRow("evt-1", "case-1", nil, "CASE_CREATED", []byte(`{"case_id":"case-1"}`), "PENDING", "case-orchestrator", time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), nil).
			AddRow("evt-2", "case-1", "task-1", "TASK_COMPLETED", []byte(`{"task_id":"task-1"}`), "DELIVERED", "case-orchestrator", time.Date(2026, 2, 20, 1, 0, 0, 0, time.UTC), time.Date(2026, 2, 20, 1, 1, 0, 0, time.UTC)))

	rows, total, err := GetCaseEventTimeline(context.Background(), db, "case-1", 1, 0)
	assert.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)
	assert.Equal(t, "evt-1", rows[0].ID)
	assert.Equal(t, "evt-2", rows[1].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventStreamAuditLog_PageBeyondTotalCount(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM events_outbox`).
		WithArgs("case-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))

	rows, total, err := GetCaseEventTimeline(context.Background(), db, "case-1", 5, 50)
	assert.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Empty(t, rows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventStreamAuditLog_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM events_outbox`).
		WithArgs("case-1").
		WillReturnError(errors.New("count failed"))

	_, _, err := GetCaseEventTimeline(context.Background(), db, "case-1", 1, 50)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegressionAndReworkTracking_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)FROM regression_metrics_snapshots`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"forward_transition_count", "regression_count", "regression_rate_percent", "regression_threshold"}).
			AddRow(int64(100), int64(12), 12.0, 3))

	mock.ExpectQuery(`(?s)WITH paths AS`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"from_stage_code", "to_stage_code", "path_count"}).
			AddRow("UNDERWRITING", "INITIAL_REVIEW", int64(7)))

	mock.ExpectQuery(`(?s)WITH flagged AS`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "regression_count"}).
			AddRow("case-123", int64(4)))

	got, err := GetRegressionReport(context.Background(), db, "HOME_LOAN", from, to)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), got.ForwardTransitionCount)
	assert.Equal(t, int64(12), got.RegressionCount)
	assert.Equal(t, 12.0, got.RegressionRatePercent)
	assert.Len(t, got.MostCommonRegressionPath, 1)
	assert.Len(t, got.FlaggedCases, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegressionAndReworkTracking_EdgeCase(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)FROM regression_metrics_snapshots`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"forward_transition_count", "regression_count", "regression_rate_percent", "regression_threshold"}).
			AddRow(int64(0), int64(0), 0.0, 5))

	mock.ExpectQuery(`(?s)WITH paths AS`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"from_stage_code", "to_stage_code", "path_count"}))

	mock.ExpectQuery(`(?s)WITH flagged AS`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "regression_count"}))

	got, err := GetRegressionReport(context.Background(), db, "HOME_LOAN", from, to)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), got.ForwardTransitionCount)
	assert.Equal(t, 5, got.RegressionThreshold)
	assert.Empty(t, got.MostCommonRegressionPath)
	assert.Empty(t, got.FlaggedCases)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRegressionAndReworkTracking_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)FROM regression_metrics_snapshots`).
		WithArgs("HOME_LOAN", from, to).
		WillReturnError(errors.New("summary unavailable"))

	_, err := GetRegressionReport(context.Background(), db, "HOME_LOAN", from, to)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceHealthLeaderboard_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	tm := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)WITH latest AS`).WillReturnRows(sqlmock.NewRows([]string{
		"bucket_start",
		"assigned_service",
		"total_tasks",
		"failed_tasks",
		"retried_tasks",
		"dlq_tasks",
		"failure_rate_percent",
		"avg_execution_seconds",
		"retry_rate_percent",
		"dlq_rate_percent",
		"dlq_contribution_rate_percent",
	}).AddRow(tm, "DOCUMENT_SERVICE", int64(100), int64(12), int64(20), int64(4), 12.0, 21.0, 20.0, 4.0, 25.0))

	got, err := GetServiceHealthLeaderboard(context.Background(), db)
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "DOCUMENT_SERVICE", got[0].AssignedService)
	assert.Equal(t, 12.0, got[0].FailureRatePercent)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceHealthLeaderboard_EdgeCase(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH latest AS`).
		WillReturnRows(sqlmock.NewRows([]string{
			"bucket_start",
			"assigned_service",
			"total_tasks",
			"failed_tasks",
			"retried_tasks",
			"dlq_tasks",
			"failure_rate_percent",
			"avg_execution_seconds",
			"retry_rate_percent",
			"dlq_rate_percent",
			"dlq_contribution_rate_percent",
		}))

	got, err := GetServiceHealthLeaderboard(context.Background(), db)
	assert.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestServiceHealthLeaderboard_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)WITH latest AS`).WillReturnError(errors.New("leaderboard unavailable"))

	_, err := GetServiceHealthLeaderboard(context.Background(), db)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventStreamAuditLogFilter_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM events_outbox`).
		WithArgs("TASK_COMPLETED", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(`(?s)FROM events_outbox\s+WHERE event_type = \$1`).
		WithArgs("TASK_COMPLETED", from, to, 50, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "case_id", "task_id", "event_type", "payload", "status", "target_service", "created_at", "delivered_at"}).
			AddRow("evt-1", "case-1", "task-1", "TASK_COMPLETED", []byte(`{"task_id":"task-1"}`), "DELIVERED", "case-orchestrator", from.Add(time.Hour), from.Add(2*time.Hour)))

	rows, total, err := GetEventsByTypeInRange(context.Background(), db, "TASK_COMPLETED", from, to, 1, 0)
	assert.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, rows, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventStreamAuditLogFilter_EdgeCase(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM events_outbox`).
		WithArgs("TASK_COMPLETED", from, to).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	rows, total, err := GetEventsByTypeInRange(context.Background(), db, "TASK_COMPLETED", from, to, 3, 50)
	assert.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Empty(t, rows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventStreamAuditLogFilter_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::int\s+FROM events_outbox`).
		WithArgs("TASK_COMPLETED", from, to).
		WillReturnError(errors.New("count failed"))

	_, _, err := GetEventsByTypeInRange(context.Background(), db, "TASK_COMPLETED", from, to, 1, 50)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventVolumeByTypePerHour_HappyPath(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)date_trunc\('hour', created_at\)`).
		WithArgs(from, to).
		WillReturnRows(sqlmock.NewRows([]string{"bucket_hour", "event_type", "volume"}).
			AddRow(time.Date(2026, 2, 1, 1, 0, 0, 0, time.UTC), "TASK_COMPLETED", int64(15)))

	rows, err := CountEventVolumeByTypePerHour(context.Background(), db, from, to)
	assert.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, int64(15), rows[0].Volume)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventVolumeByTypePerHour_EdgeCase(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)date_trunc\('hour', created_at\)`).
		WithArgs(from, to).
		WillReturnRows(sqlmock.NewRows([]string{"bucket_hour", "event_type", "volume"}))

	rows, err := CountEventVolumeByTypePerHour(context.Background(), db, from, to)
	assert.NoError(t, err)
	assert.Empty(t, rows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEventVolumeByTypePerHour_FailureMode(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)date_trunc\('hour', created_at\)`).
		WithArgs(from, to).
		WillReturnError(errors.New("volume query failed"))

	_, err := CountEventVolumeByTypePerHour(context.Background(), db, from, to)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
