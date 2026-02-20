package reporting

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestMetricsSnapshotBackgroundJob_HappyPath(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(metricsRefreshAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO case_throughput_snapshots")).WillReturnResult(sqlmock.NewResult(0, 50))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO case_throughput_snapshots")).WillReturnResult(sqlmock.NewResult(0, 120))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO task_metrics_snapshots")).WillReturnResult(sqlmock.NewResult(0, 75))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO regression_metrics_snapshots")).WillReturnResult(sqlmock.NewResult(0, 20))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO service_health_snapshots")).WillReturnResult(sqlmock.NewResult(0, 15))
	mock.ExpectCommit()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(metricsRefreshAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

	job := NewMetricsRefreshJob(db, time.Minute, 3, nil)
	err = job.Run(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMetricsSnapshotBackgroundJob_ConcurrentExecution(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(metricsRefreshAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

	job := NewMetricsRefreshJob(db, time.Minute, 3, nil)
	err = job.Run(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMetricsSnapshotBackgroundJob_FailureMode(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_try_advisory_lock($1)")).
		WithArgs(metricsRefreshAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO case_throughput_snapshots")).WillReturnError(errors.New("upsert failed"))
	mock.ExpectRollback()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(metricsRefreshAdvisoryLockKey).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

	job := NewMetricsRefreshJob(db, time.Minute, 3, nil)
	err = job.Run(context.Background())
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestReportingEventHintObserver_HappyPath(t *testing.T) {
	sqlDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	job := NewMetricsRefreshJob(db, time.Minute, 3, nil)
	observer := NewEventHintObserver(job, nil)

	err = observer.HandleEvent(context.Background(), model.Event{EventType: model.EventTaskCompleted})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(job.triggerCh))
}

func TestReportingEventHintObserver_EdgeCase(t *testing.T) {
	sqlDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	job := NewMetricsRefreshJob(db, time.Minute, 3, nil)
	observer := NewEventHintObserver(job, nil)

	err = observer.HandleEvent(context.Background(), model.Event{EventType: model.EventNotificationQueued})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(job.triggerCh))
}

func TestReportingEventHintObserver_FailureMode(t *testing.T) {
	observer := NewEventHintObserver(nil, nil)
	err := observer.HandleEvent(context.Background(), model.Event{EventType: model.EventTaskCompleted})
	assert.NoError(t, err)
}
