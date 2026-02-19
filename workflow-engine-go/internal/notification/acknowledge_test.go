package notification

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestAcknowledgeNotification(t *testing.T) {
	now := time.Date(2026, 2, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		setup       func(sqlmock.Sqlmock)
		wantAlready bool
		wantErr     bool
	}{
		{
			name: "happy path acknowledge",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue q`).WithArgs("notif-1").WillReturnRows(
					sqlmock.NewRows([]string{"acknowledged_at"}).AddRow(nil),
				)
				mock.ExpectQuery(`(?s)UPDATE notification_queue`).WithArgs("notif-1").WillReturnRows(
					sqlmock.NewRows([]string{"acknowledged_at"}).AddRow(now),
				)
				mock.ExpectCommit()
			},
		},
		{
			name: "edge already acknowledged",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue q`).WithArgs("notif-1").WillReturnRows(
					sqlmock.NewRows([]string{"acknowledged_at"}).AddRow(now),
				)
				mock.ExpectCommit()
			},
			wantAlready: true,
		},
		{
			name: "failure mode db error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue q`).WithArgs("notif-1").WillReturnError(errors.New("db error"))
				mock.ExpectRollback()
			},
			wantErr: true,
		},
		{
			name: "failure mode not found",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM notification_queue q`).WithArgs("notif-1").WillReturnError(sql.ErrNoRows)
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

			tt.setup(mock)
			_, already, err := AcknowledgeNotification(context.Background(), db, "notif-1")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantAlready, already)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
