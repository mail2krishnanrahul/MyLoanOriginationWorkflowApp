package approval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestGetApprovalHistory(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantLen int
		wantErr bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "approval_request_id", "event_type", "actor_id", "decision_text", "evidence_refs", "previous_state", "new_state", "created_at", "approver_name",
				}).AddRow("a1", "r1", "APPROVED", "u1", "ok", []byte("[]"), "PENDING", "APPROVED", time.Now().UTC(), "Jane Doe")
				mock.ExpectQuery(`(?s)FROM approval_audit_log`).WithArgs("case-1").WillReturnRows(rows)
			},
			wantLen: 1,
		},
		{
			name: "failure mode db error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)FROM approval_audit_log`).WithArgs("case-1").WillReturnError(errors.New("db error"))
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
			got, err := GetApprovalHistory(context.Background(), db, "case-1")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, got, tt.wantLen)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
