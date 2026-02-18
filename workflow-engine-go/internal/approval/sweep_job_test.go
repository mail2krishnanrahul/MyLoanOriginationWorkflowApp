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

func TestApprovalExpirySweepJobRun(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path auto approve expiry",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{
					"request_id", "approval_gate_id", "case_id", "task_id", "approver_id", "status",
					"tier", "expires_at", "on_timeout_action", "approval_timeout_hours", "fallback_supervisor_role", "calendar_id",
				}).AddRow(
					"req-1", "gate-1", "case-1", "task-1", "u-1", "PENDING", nil, time.Now().Add(-time.Hour), "AUTO_APPROVE", 2.0, nil, nil,
				)
				mock.ExpectQuery(`(?s)FROM approval_requests r`).WithArgs(500).WillReturnRows(rows)
				mock.ExpectExec(`(?s)UPDATE approval_requests`).WithArgs("req-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`(?s)INSERT INTO approval_audit_log`).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec(`(?s)UPDATE approval_requests`).WithArgs("req-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`(?s)INSERT INTO approval_audit_log`).WillReturnResult(sqlmock.NewResult(1, 1))

				gateRows := sqlmock.NewRows([]string{"id", "case_id", "task_id", "approval_policy", "required_approver_count", "gate_status", "version"}).
					AddRow("gate-1", "case-1", "task-1", "SINGLE_APPROVER", 1, "ACTIVE", 1)
				mock.ExpectQuery(`(?s)FROM approval_gates`).WithArgs("gate-1").WillReturnRows(gateRows)
				countRows := sqlmock.NewRows([]string{"status", "cnt"}).AddRow("APPROVED", 1)
				mock.ExpectQuery(`(?s)FROM approval_requests`).WithArgs("gate-1").WillReturnRows(countRows)
				mock.ExpectExec(`(?s)UPDATE approval_gates`).WithArgs("SATISFIED", sqlmock.AnyArg(), "gate-1", 1).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`(?s)UPDATE tasks`).WithArgs("task-1").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
		},
		{
			name: "failure mode candidate query error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM approval_requests r`).WithArgs(500).WillReturnError(errors.New("db error"))
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

			publisher := &stubPublisher{}
			evaluator := NewApprovalPolicyEvaluator(db, nil, publisher)
			job := NewApprovalExpirySweepJob(db, publisher, evaluator, 0, 500, nil)
			tt.setup(mock)

			err = job.Run(context.Background())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
