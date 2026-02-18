package sla

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

type testHoliday struct {
	date      time.Time
	recurring bool
}

func TestAddBusinessHours(t *testing.T) {
	tests := []struct {
		name       string
		start      time.Time
		duration   time.Duration
		calendarID string
		holidays   []testHoliday
		want       time.Time
		wantErr    bool
	}{
		{
			name:       "same day within working hours",
			start:      parseTime("2025-02-18 10:00:00 UTC"),
			duration:   3 * time.Hour,
			calendarID: "default",
			want:       parseTime("2025-02-18 13:00:00 UTC"),
		},
		{
			name:       "overnight span",
			start:      parseTime("2025-02-18 16:00:00 UTC"),
			duration:   4 * time.Hour,
			calendarID: "default",
			want:       parseTime("2025-02-19 12:00:00 UTC"),
		},
		{
			name:       "weekend skip",
			start:      parseTime("2025-02-21 16:00:00 UTC"),
			duration:   4 * time.Hour,
			calendarID: "default",
			want:       parseTime("2025-02-24 12:00:00 UTC"),
		},
		{
			name:       "holiday skip",
			start:      parseTime("2025-02-18 16:00:00 UTC"),
			duration:   4 * time.Hour,
			calendarID: "default",
			holidays: []testHoliday{
				{date: time.Date(2025, 2, 19, 0, 0, 0, 0, time.UTC), recurring: false},
			},
			want: parseTime("2025-02-20 12:00:00 UTC"),
		},
		{
			name:       "multiple weeks",
			start:      parseTime("2025-02-17 10:00:00 UTC"),
			duration:   40 * time.Hour,
			calendarID: "default",
			want:       parseTime("2025-02-24 10:00:00 UTC"),
		},
		{
			name:       "start in non-working hours",
			start:      parseTime("2025-02-18 07:00:00 UTC"),
			duration:   2 * time.Hour,
			calendarID: "default",
			want:       parseTime("2025-02-18 11:00:00 UTC"),
		},
		{
			name:       "negative duration fails",
			start:      parseTime("2025-02-18 10:00:00 UTC"),
			duration:   -1 * time.Hour,
			calendarID: "default",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newCalendarMockDB(t, tt.calendarID, tt.holidays)
			defer db.Close()

			got, err := AddBusinessHours(context.Background(), db, tt.start, tt.duration, tt.calendarID)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.True(t, tt.want.Equal(got), "expected %s got %s", tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestBusinessHoursElapsed(t *testing.T) {
	tests := []struct {
		name       string
		start      time.Time
		end        time.Time
		calendarID string
		want       time.Duration
		wantErr    bool
	}{
		{
			name:       "same day",
			start:      parseTime("2025-02-18 10:00:00 UTC"),
			end:        parseTime("2025-02-18 15:00:00 UTC"),
			calendarID: "default",
			want:       5 * time.Hour,
		},
		{
			name:       "overnight",
			start:      parseTime("2025-02-18 16:00:00 UTC"),
			end:        parseTime("2025-02-19 11:00:00 UTC"),
			calendarID: "default",
			want:       3 * time.Hour,
		},
		{
			name:       "end before start",
			start:      parseTime("2025-02-19 11:00:00 UTC"),
			end:        parseTime("2025-02-18 16:00:00 UTC"),
			calendarID: "default",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newCalendarMockDB(t, tt.calendarID, nil)
			defer db.Close()

			got, err := BusinessHoursElapsed(context.Background(), db, tt.start, tt.end, tt.calendarID)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func newCalendarMockDB(t *testing.T, calendarID string, holidays []testHoliday) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	db := sqlx.NewDb(sqlDB, "sqlmock")

	calendarRow := sqlmock.NewRows([]string{"id", "name", "timezone", "start_time", "end_time", "working_days"}).
		AddRow("00000000-0000-0000-0000-000000000001", "default", "UTC", "09:00", "17:00", 31)

	mock.ExpectQuery(`(?s)FROM business_calendars`).
		WithArgs(calendarID).
		WillReturnRows(calendarRow)

	holidayRows := sqlmock.NewRows([]string{"holiday_date", "is_recurring"})
	for _, h := range holidays {
		holidayRows.AddRow(h.date, h.recurring)
	}
	mock.ExpectQuery(`(?s)FROM holiday_calendars`).
		WithArgs("00000000-0000-0000-0000-000000000001").
		WillReturnRows(holidayRows)

	return db, mock
}

func parseTime(v string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05 MST", v)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}
