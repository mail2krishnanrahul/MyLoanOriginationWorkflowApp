package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestGetNotificationHistory(t *testing.T) {
	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		setup     func(sqlmock.Sqlmock)
		wantCount int
		wantErr   bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM notification_queue`).
					WithArgs("case-1").
					WillReturnRows(sqlmock.NewRows([]string{
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
					}).AddRow(
						"notif-1",
						"TRG_CASE_CREATED",
						"case-1",
						nil,
						"CASE_CREATED",
						"EMAIL",
						"borrower@example.com",
						"Subject",
						"Body",
						"NORMAL",
						now,
						"SENT",
						1,
						now,
						now,
						[]byte(`{}`),
						nil,
						now,
						now,
					))

				mock.ExpectQuery(`(?s)FROM notification_delivery_events`).
					WithArgs("notif-1").
					WillReturnRows(sqlmock.NewRows([]string{
						"id",
						"notification_id",
						"event_type",
						"event_timestamp",
						"channel_response",
						"user_agent",
						"created_at",
					}).AddRow(
						"evt-1",
						"notif-1",
						"DELIVERED",
						now,
						[]byte(`{"provider_id":"abc"}`),
						nil,
						now,
					))
			},
			wantCount: 1,
		},
		{
			name: "edge empty history",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM notification_queue`).
					WithArgs("case-1").
					WillReturnRows(sqlmock.NewRows([]string{
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
					}))
			},
			wantCount: 0,
		},
		{
			name: "failure mode query error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM notification_queue`).
					WithArgs("case-1").
					WillReturnError(errors.New("db down"))
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

			tt.setup(mock)
			records, err := GetNotificationHistory(context.Background(), db, "case-1")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, records, tt.wantCount)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetDeliveryRate(t *testing.T) {
	start := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	tests := []struct {
		name      string
		setup     func(sqlmock.Sqlmock)
		start     time.Time
		end       time.Time
		wantTotal int64
		wantErr   bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM notification_queue q`).
					WithArgs("EMAIL", start, end).
					WillReturnRows(sqlmock.NewRows([]string{"total_sent", "delivered", "failed", "bounced"}).
						AddRow(int64(10), int64(9), int64(1), int64(1)))
			},
			start:     start,
			end:       end,
			wantTotal: 10,
		},
		{
			name: "edge zero volume",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM notification_queue q`).
					WithArgs("EMAIL", start, end).
					WillReturnRows(sqlmock.NewRows([]string{"total_sent", "delivered", "failed", "bounced"}).
						AddRow(int64(0), int64(0), int64(0), int64(0)))
			},
			start:     start,
			end:       end,
			wantTotal: 0,
		},
		{
			name:    "failure mode invalid range",
			start:   end,
			end:     start,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			if tt.setup != nil {
				tt.setup(mock)
			}

			stats, err := GetDeliveryRate(context.Background(), db, "EMAIL", tt.start, tt.end)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTotal, stats.TotalSent)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRefreshCorrespondenceSummary(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`(?s)REFRESH MATERIALIZED VIEW CONCURRENTLY correspondence_summary`).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name: "failure mode refresh error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(`(?s)REFRESH MATERIALIZED VIEW CONCURRENTLY correspondence_summary`).
					WillReturnError(errors.New("refresh failed"))
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

			tt.setup(mock)
			err = RefreshCorrespondenceSummary(context.Background(), db)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetCorrespondenceSummary(t *testing.T) {
	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)
	caseID := "case-1"

	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantID  string
		wantErr bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM correspondence_summary`).
					WithArgs(caseID).
					WillReturnRows(sqlmock.NewRows([]string{
						"case_id",
						"total_sent",
						"sent_by_channel",
						"unacknowledged_borrower_count",
						"failed_count",
						"failed_reasons",
						"avg_delivery_seconds",
						"refreshed_at",
					}).AddRow(
						caseID,
						int64(10),
						[]byte(`{"EMAIL": 10}`),
						int64(0),
						int64(0),
						[]byte(`{}`),
						3.5,
						now,
					))
			},
			wantID: caseID,
		},
		{
			name: "not found",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM correspondence_summary`).
					WithArgs(caseID).
					WillReturnRows(sqlmock.NewRows([]string{
						"case_id",
						"total_sent",
						"sent_by_channel",
						"unacknowledged_borrower_count",
						"failed_count",
						"failed_reasons",
						"avg_delivery_seconds",
						"refreshed_at",
					}))
			},
			wantID: "",
		},
		{
			name: "db error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM correspondence_summary`).
					WithArgs(caseID).
					WillReturnError(errors.New("db error"))
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

			tt.setup(mock)
			summary, err := GetCorrespondenceSummary(context.Background(), db, caseID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.wantID != "" {
					assert.NotNil(t, summary)
					assert.Equal(t, tt.wantID, summary.CaseID)
				} else {
					assert.Nil(t, summary)
				}
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
