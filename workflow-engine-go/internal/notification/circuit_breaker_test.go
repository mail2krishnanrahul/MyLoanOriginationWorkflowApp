package notification

import (
	"context"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestCircuitBreakerCheckState(t *testing.T) {
	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		setup     func(sqlmock.Sqlmock)
		wantAllow bool
		wantErr   bool
	}{
		{
			name: "happy path closed allows",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"channel", "state", "failure_count", "success_count", "last_failure_at", "opened_at", "half_open_at", "threshold_failures", "cooldown_seconds"}).
					AddRow("EMAIL", "CLOSED", 0, 0, nil, nil, nil, 10, 300)
				mock.ExpectQuery(`(?s)FROM circuit_breaker_state`).WithArgs("EMAIL").WillReturnRows(rows)
				mock.ExpectCommit()
			},
			wantAllow: true,
		},
		{
			name: "edge open to half-open after cooldown",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"channel", "state", "failure_count", "success_count", "last_failure_at", "opened_at", "half_open_at", "threshold_failures", "cooldown_seconds"}).
					AddRow("EMAIL", "OPEN", 10, 0, now.Add(-2*time.Minute), now.Add(-10*time.Minute), nil, 10, 300)
				mock.ExpectQuery(`(?s)FROM circuit_breaker_state`).WithArgs("EMAIL").WillReturnRows(rows)
				mock.ExpectExec(`(?s)UPDATE circuit_breaker_state`).
					WithArgs("EMAIL", "HALF_OPEN", 0, 0, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			wantAllow: true,
		},
		{
			name: "failure mode query error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM circuit_breaker_state`).WithArgs("EMAIL").WillReturnError(assert.AnError)
				mock.ExpectRollback()
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			cb := NewCircuitBreaker(db, 10, 5*time.Minute, 3, nil, &stubNotificationPublisher{})
			cb.nowFunc = func() time.Time { return now }
			tt.setup(mock)

			allow, err := cb.CheckState(context.Background(), "EMAIL")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantAllow, allow)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCircuitBreakerTransitions(t *testing.T) {
	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		run     func(ctx context.Context, cb *CircuitBreaker, tx *sqlx.Tx) error
		wantErr bool
	}{
		{
			name: "closed to open on threshold failure",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"channel", "state", "failure_count", "success_count", "last_failure_at", "opened_at", "half_open_at", "threshold_failures", "cooldown_seconds"}).
					AddRow("EMAIL", "CLOSED", 9, 0, now.Add(-30*time.Second), nil, nil, 10, 300)
				mock.ExpectQuery(`(?s)FROM circuit_breaker_state`).WithArgs("EMAIL").WillReturnRows(rows)
				mock.ExpectExec(`(?s)UPDATE circuit_breaker_state`).
					WithArgs("EMAIL", "OPEN", 10, 0, sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			run: func(ctx context.Context, cb *CircuitBreaker, tx *sqlx.Tx) error {
				return cb.RecordFailure(ctx, tx, "EMAIL")
			},
		},
		{
			name: "half-open to closed after success threshold",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"channel", "state", "failure_count", "success_count", "last_failure_at", "opened_at", "half_open_at", "threshold_failures", "cooldown_seconds"}).
					AddRow("EMAIL", "HALF_OPEN", 0, 2, nil, now.Add(-10*time.Minute), now.Add(-1*time.Minute), 10, 300)
				mock.ExpectQuery(`(?s)FROM circuit_breaker_state`).WithArgs("EMAIL").WillReturnRows(rows)
				mock.ExpectExec(`(?s)UPDATE circuit_breaker_state`).
					WithArgs("EMAIL", "CLOSED", 0, 0, nil, nil, nil).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			run: func(ctx context.Context, cb *CircuitBreaker, tx *sqlx.Tx) error {
				return cb.RecordSuccess(ctx, tx, "EMAIL")
			},
		},
		{
			name: "half-open to open on failure",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"channel", "state", "failure_count", "success_count", "last_failure_at", "opened_at", "half_open_at", "threshold_failures", "cooldown_seconds"}).
					AddRow("EMAIL", "HALF_OPEN", 0, 1, nil, now.Add(-10*time.Minute), now.Add(-30*time.Second), 10, 300)
				mock.ExpectQuery(`(?s)FROM circuit_breaker_state`).WithArgs("EMAIL").WillReturnRows(rows)
				mock.ExpectExec(`(?s)UPDATE circuit_breaker_state`).
					WithArgs("EMAIL", "OPEN", 10, 0, sqlmock.AnyArg(), sqlmock.AnyArg(), nil).
					WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			run: func(ctx context.Context, cb *CircuitBreaker, tx *sqlx.Tx) error {
				return cb.RecordFailure(ctx, tx, "EMAIL")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			publisher := &stubNotificationPublisher{}
			cb := NewCircuitBreaker(db, 10, 5*time.Minute, 3, nil, publisher)
			cb.nowFunc = func() time.Time { return now }

			tt.setup(mock)
			tx, err := db.BeginTxx(context.Background(), nil)
			assert.NoError(t, err)

			err = tt.run(context.Background(), cb, tx)
			if tt.wantErr {
				assert.Error(t, err)
				_ = tx.Rollback()
			} else {
				assert.NoError(t, err)
				assert.NoError(t, tx.Commit())
			}
			assert.NoError(t, mock.ExpectationsWereMet())

			if tt.name == "closed to open on threshold failure" || tt.name == "half-open to open on failure" {
				assert.GreaterOrEqual(t, len(publisher.events), 1)
				assert.Equal(t, model.EventCircuitBreakerOpened, publisher.events[0].EventType)
			}
		})
	}
}

type stubNotificationPublisher struct {
	events []model.Event
}

func (s *stubNotificationPublisher) PublishEvent(_ context.Context, _ *sqlx.Tx, event model.Event) error {
	s.events = append(s.events, event)
	return nil
}
