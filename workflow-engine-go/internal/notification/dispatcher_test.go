package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestNotificationDispatcherRun(t *testing.T) {
	tests := []struct {
		name       string
		channelMap map[string]NotificationChannel
		setup      func(sqlmock.Sqlmock)
		wantErr    bool
	}{
		{
			name: "happy path sent",
			channelMap: map[string]NotificationChannel{
				"EMAIL": &mockChannel{name: "EMAIL"},
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue`).WithArgs(5, 500).WillReturnRows(singleDispatchRow("n1", "EMAIL", 0))
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // CLAIMED
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnError(sql.ErrNoRows)
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // DISPATCHED
				mock.ExpectExec(`(?s)UPDATE notification_queue`).WillReturnResult(sqlmock.NewResult(0, 1))                // SENT
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // DELIVERED
				mock.ExpectCommit()
			},
		},
		{
			name: "edge transient retry",
			channelMap: map[string]NotificationChannel{
				"EMAIL": &mockChannel{name: "EMAIL", sendErr: newTransientChannelError("timeout", "EMAIL_TIMEOUT", errors.New("timeout")), transient: true},
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue`).WithArgs(5, 500).WillReturnRows(singleDispatchRow("n2", "EMAIL", 1))
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // CLAIMED
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnError(sql.ErrNoRows)
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // DISPATCHED
				mock.ExpectExec(`(?s)UPDATE notification_queue`).WillReturnResult(sqlmock.NewResult(0, 1))                // retry schedule
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // FAILED event
				mock.ExpectCommit()
			},
		},
		{
			name:       "failure mode channel missing",
			channelMap: map[string]NotificationChannel{},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue`).WithArgs(5, 500).WillReturnRows(singleDispatchRow("n3", "WEBHOOK", 0))
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // CLAIMED
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "WEBHOOK").WillReturnError(sql.ErrNoRows)
				mock.ExpectExec(`(?s)UPDATE notification_queue`).WillReturnResult(sqlmock.NewResult(0, 1))                // FAILED
				mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1)) // FAILED event
				mock.ExpectCommit()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			tt.setup(mock)
			dispatcher := NewNotificationDispatcher(db, NewTemplateRenderer(), tt.channelMap, nil, 10*time.Second, 500, 5, nil, &stubNotificationPublisher{})
			dispatcher.checkCircuitFn = func(context.Context, string) (bool, error) { return true, nil }
			dispatcher.recordCircuitFailureFn = func(context.Context, *sqlx.Tx, string) error { return nil }
			dispatcher.recordCircuitSuccessFn = func(context.Context, *sqlx.Tx, string) error { return nil }
			dispatcher.nowFunc = func() time.Time { return time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC) }
			dispatcher.jitterFunc = func(time.Duration) time.Duration { return 0 }

			err = dispatcher.Run(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNotificationDispatcherEndToEnd(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	mock.ExpectBegin()
	rows := sqlmock.NewRows(dispatchRowColumns())
	for i := 0; i < 10; i++ {
		channel := "EMAIL"
		rows.AddRow(
			fmt.Sprintf("n-%d", i),
			"TRG_CASE_CREATED",
			"case-1",
			nil,
			"TPL_CASE_CREATED",
			channel,
			fmt.Sprintf("user-%d", i),
			"Subject",
			"Body",
			"NORMAL",
			time.Now().UTC().Add(-time.Minute),
			"PENDING",
			0,
			nil,
			nil,
			nil,
			nil,
			time.Now().UTC().Add(-2*time.Minute),
			time.Now().UTC().Add(-2*time.Minute),
			"CASE_CREATED",
			"Subject {{.reference_number}}",
			"Body {{.reference_number}}",
			"REF-1",
			"HOME_LOAN",
			[]byte(`{"borrower_email":"x@y.com"}`),
			"UNDERWRITING",
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
		)
	}
	mock.ExpectQuery(`(?s)FROM notification_queue`).WithArgs(5, 500).WillReturnRows(rows)

	for i := 0; i < 10; i++ {
		mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs(fmt.Sprintf("user-%d", i), "EMAIL").WillReturnError(sql.ErrNoRows)
	}

	// Each notification writes CLAIMED event.
	for i := 0; i < 10; i++ {
		mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1))
	}

	// 8 success flows: DISPATCHED + UPDATE SENT + DELIVERED
	for i := 0; i < 8; i++ {
		mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`(?s)UPDATE notification_queue`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1))
	}

	// 2 transient failures: DISPATCHED + UPDATE RETRY + FAILED EVENT
	for i := 0; i < 2; i++ {
		mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`(?s)UPDATE notification_queue`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`(?s)INSERT INTO notification_delivery_events`).WillReturnResult(sqlmock.NewResult(1, 1))
	}

	mock.ExpectCommit()

	channel := &mockChannel{name: "EMAIL", failRecipients: map[string]error{
		"user-8": newTransientChannelError("timeout", "EMAIL_TIMEOUT", errors.New("timeout")),
		"user-9": newTransientChannelError("timeout", "EMAIL_TIMEOUT", errors.New("timeout")),
	}, transient: true}

	dispatcher := NewNotificationDispatcher(db, NewTemplateRenderer(), map[string]NotificationChannel{"EMAIL": channel}, nil, 10*time.Second, 500, 5, nil, &stubNotificationPublisher{})
	dispatcher.checkCircuitFn = func(context.Context, string) (bool, error) { return true, nil }
	dispatcher.recordCircuitFailureFn = func(context.Context, *sqlx.Tx, string) error { return nil }
	dispatcher.recordCircuitSuccessFn = func(context.Context, *sqlx.Tx, string) error { return nil }
	dispatcher.jitterFunc = func(time.Duration) time.Duration { return 0 }

	err = dispatcher.Run(context.Background())
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())

	assert.Equal(t, 10, channel.sendCount)
	assert.Equal(t, 2, channel.failCount)
}

func TestNotificationDispatcherComputeNextRetry(t *testing.T) {
	dispatcher := NewNotificationDispatcher(nil, NewTemplateRenderer(), nil, nil, 10*time.Second, 500, 5, nil, &stubNotificationPublisher{})
	dispatcher.baseRetryInterval = 30 * time.Second
	dispatcher.jitterFunc = func(base time.Duration) time.Duration {
		assert.Equal(t, 30*time.Second, base)
		return 5 * time.Second
	}

	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		attempt int
		want    time.Time
	}{
		{
			name:    "first retry",
			attempt: 1,
			want:    now.Add(65 * time.Second),
		},
		{
			name:    "third retry",
			attempt: 3,
			want:    now.Add(245 * time.Second),
		},
		{
			name:    "edge negative attempt treated as zero",
			attempt: -1,
			want:    now.Add(35 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dispatcher.computeNextRetry(now, tt.attempt)
			assert.Equal(t, tt.want, got)
		})
	}
}

func singleDispatchRow(id, channel string, attempts int) *sqlmock.Rows {
	return sqlmock.NewRows(dispatchRowColumns()).AddRow(
		id,
		"TRG_CASE_CREATED",
		"case-1",
		nil,
		"TPL_CASE_CREATED",
		channel,
		"user-1",
		"Subject",
		"Body",
		"NORMAL",
		time.Now().UTC().Add(-time.Minute),
		"PENDING",
		attempts,
		nil,
		nil,
		nil,
		nil,
		time.Now().UTC().Add(-2*time.Minute),
		time.Now().UTC().Add(-2*time.Minute),
		"CASE_CREATED",
		"Subject {{.reference_number}}",
		"Body {{.reference_number}}",
		"REF-1",
		"HOME_LOAN",
		[]byte(`{"borrower_email":"x@y.com"}`),
		"UNDERWRITING",
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func dispatchRowColumns() []string {
	return []string{
		"id",
		"trigger_code",
		"case_id",
		"task_id",
		"template_code",
		"channel",
		"recipient",
		"subject",
		"body",
		"priority",
		"scheduled_at",
		"status",
		"attempts",
		"last_attempt_at",
		"sent_at",
		"error_detail",
		"acknowledged_at",
		"created_at",
		"updated_at",
		"trigger_event_type",
		"template_subject_template",
		"template_body_template",
		"case_reference_number",
		"case_type_code",
		"case_metadata",
		"case_current_stage_code",
		"task_input_payload",
		"task_output_payload",
		"task_metadata",
		"task_stage_code",
		"task_activity_code",
		"task_definition_code",
	}
}

type mockChannel struct {
	name           string
	sendErr        error
	transient      bool
	failRecipients map[string]error
	sendCount      int
	failCount      int
}

func (m *mockChannel) Name() string { return m.name }

func (m *mockChannel) Send(_ context.Context, notif Notification) error {
	m.sendCount++
	if m.failRecipients != nil {
		if err, ok := m.failRecipients[notif.Recipient]; ok {
			m.failCount++
			return err
		}
	}
	if m.sendErr != nil {
		m.failCount++
		return m.sendErr
	}
	return nil
}

func (m *mockChannel) IsTransientError(err error) bool {
	if m.sendErr == nil && m.failRecipients == nil {
		return false
	}
	if m.transient {
		return true
	}
	return isTransientChannelError(err)
}
