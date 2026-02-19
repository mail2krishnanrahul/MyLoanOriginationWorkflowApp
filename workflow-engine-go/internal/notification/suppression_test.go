package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestCheckDuplicateNotification(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(sqlmock.Sqlmock)
		windowMins    int
		wantDuplicate bool
		wantErr       bool
	}{
		{
			name:       "duplicate within window suppress",
			windowMins: 60,
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"exists"}).AddRow(true)
				mock.ExpectQuery(`(?s)SELECT EXISTS`).WithArgs("a@b.com", "TASK_ASSIGNED_EMAIL", "case-1", 60).WillReturnRows(rows)
			},
			wantDuplicate: true,
		},
		{
			name:       "duplicate outside window allow",
			windowMins: 60,
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"exists"}).AddRow(false)
				mock.ExpectQuery(`(?s)SELECT EXISTS`).WithArgs("a@b.com", "TASK_ASSIGNED_EMAIL", "case-1", 60).WillReturnRows(rows)
			},
			wantDuplicate: false,
		},
		{
			name:       "edge dedupe window boundary zero",
			windowMins: 0,
		},
		{
			name:       "failure mode db error",
			windowMins: 60,
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)SELECT EXISTS`).WithArgs("a@b.com", "TASK_ASSIGNED_EMAIL", "case-1", 60).WillReturnError(errors.New("db error"))
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

			if tt.setup != nil {
				tt.setup(mock)
			}
			dup, err := CheckDuplicateNotification(context.Background(), db, "a@b.com", "TASK_ASSIGNED_EMAIL", "case-1", tt.windowMins)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantDuplicate, dup)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCheckUserPreferences(t *testing.T) {
	quietStart := time.Now().UTC().Add(-1 * time.Hour).Format("15:04:05")
	quietEnd := time.Now().UTC().Add(1 * time.Hour).Format("15:04:05")

	tests := []struct {
		name         string
		setup        func(sqlmock.Sqlmock)
		wantSuppress bool
		wantReason   string
		wantErr      bool
	}{
		{
			name: "user opt-out suppress",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"opt_out", "quiet_hours_start", "quiet_hours_end", "quiet_hours_timezone", "enabled_notification_types"}).
					AddRow(true, nil, nil, nil, json.RawMessage(`[]`))
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnRows(rows)
			},
			wantSuppress: true,
			wantReason:   "OPT_OUT",
		},
		{
			name: "notification type not enabled suppress",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"opt_out", "quiet_hours_start", "quiet_hours_end", "quiet_hours_timezone", "enabled_notification_types"}).
					AddRow(false, nil, nil, nil, json.RawMessage(`["TASK_ASSIGNED"]`))
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnRows(rows)
			},
			wantSuppress: true,
			wantReason:   "TYPE_DISABLED",
		},
		{
			name: "quiet hours delay",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"opt_out", "quiet_hours_start", "quiet_hours_end", "quiet_hours_timezone", "enabled_notification_types"}).
					AddRow(false, quietStart, quietEnd, "UTC", json.RawMessage(`[]`))
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnRows(rows)
			},
			wantSuppress: false,
			wantReason:   "QUIET_HOURS",
		},
		{
			name: "no preference row allow",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnError(sql.ErrNoRows)
			},
			wantSuppress: false,
			wantReason:   "",
		},
		{
			name: "failure mode invalid json",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"opt_out", "quiet_hours_start", "quiet_hours_end", "quiet_hours_timezone", "enabled_notification_types"}).
					AddRow(false, nil, nil, nil, json.RawMessage(`{"bad":true}`))
				mock.ExpectQuery(`(?s)FROM user_preferences`).WithArgs("user-1", "EMAIL").WillReturnRows(rows)
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
			suppress, reason, err := CheckUserPreferences(context.Background(), db, "user-1", "EMAIL", "CASE_CREATED")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantSuppress, suppress)
				assert.Equal(t, tt.wantReason, reason)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNextQuietHoursEnd(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Time
		start      string
		end        string
		tz         string
		wantWithin bool
		want       time.Time
		wantErr    bool
	}{
		{
			name:       "within standard window",
			now:        time.Date(2026, 2, 19, 15, 30, 0, 0, time.UTC),
			start:      "15:00:00",
			end:        "16:00:00",
			tz:         "UTC",
			wantWithin: true,
			want:       time.Date(2026, 2, 19, 16, 0, 0, 0, time.UTC),
		},
		{
			name:       "within overnight window",
			now:        time.Date(2026, 2, 19, 23, 30, 0, 0, time.UTC),
			start:      "22:00:00",
			end:        "07:00:00",
			tz:         "UTC",
			wantWithin: true,
			want:       time.Date(2026, 2, 20, 7, 0, 0, 0, time.UTC),
		},
		{
			name:       "outside window",
			now:        time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC),
			start:      "22:00:00",
			end:        "07:00:00",
			tz:         "UTC",
			wantWithin: false,
		},
		{
			name:    "failure mode bad timezone",
			now:     time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC),
			start:   "22:00:00",
			end:     "07:00:00",
			tz:      "Bad/Timezone",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, within, err := NextQuietHoursEnd(tt.now, tt.start, tt.end, tt.tz)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantWithin, within)
			if tt.wantWithin {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
