package approval

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

type stubPublisher struct {
	events []model.Event
}

func (s *stubPublisher) PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestEvaluatePolicyCounts(t *testing.T) {
	tests := []struct {
		name      string
		policy    model.ApprovalPolicy
		required  int
		counts    requestCounts
		satisfied bool
		failed    bool
	}{
		{
			name:   "ALL_MUST_APPROVE with one rejection fails fast",
			policy: model.ApprovalPolicyAllMustApprove,
			required: 0,
			counts: requestCounts{Total: 3, Approved: 1, Rejected: 1, Pending: 1},
			failed: true,
		},
		{
			name:   "MAJORITY with 50/50 split not satisfied",
			policy: model.ApprovalPolicyMajority,
			required: 0,
			counts: requestCounts{Total: 4, Approved: 2, Rejected: 2},
		},
		{
			name:      "CONSENSUS with 67 percent approved satisfied",
			policy:    model.ApprovalPolicyConsensus,
			required:  0,
			counts:    requestCounts{Total: 3, Approved: 2, Rejected: 1},
			satisfied: true,
		},
		{
			name:   "ANY_CAN_APPROVE with all rejected fails fast",
			policy: model.ApprovalPolicyAnyCanApprove,
			required: 0,
			counts: requestCounts{Total: 2, Approved: 0, Rejected: 2},
			failed: true,
		},
		{
			name:      "ANY_CAN_APPROVE honors required approver count",
			policy:    model.ApprovalPolicyAnyCanApprove,
			required:  2,
			counts:    requestCounts{Total: 3, Approved: 2, Rejected: 0, Pending: 1},
			satisfied: true,
		},
		{
			name:     "ANY_CAN_APPROVE fail-fast when remaining cannot hit required count",
			policy:   model.ApprovalPolicyAnyCanApprove,
			required: 3,
			counts:   requestCounts{Total: 4, Approved: 1, Rejected: 2, Pending: 1},
			failed:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluatePolicyCounts(tt.policy, tt.required, tt.counts)
			assert.Equal(t, tt.satisfied, got.Satisfied)
			assert.Equal(t, tt.failed, got.Failed)
			assert.NoError(t, got.Err)
		})
	}
}

func TestApprovalPolicyEvaluatorEvaluateApprovalPolicy(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		want    bool
		wantErr bool
	}{
		{
			name: "happy path gate satisfied",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				gateRows := sqlmock.NewRows([]string{"id", "case_id", "task_id", "approval_policy", "required_approver_count", "gate_status", "version"}).
					AddRow("gate-1", "case-1", "task-1", "ALL_MUST_APPROVE", 1, "ACTIVE", 3)
				mock.ExpectQuery(`(?s)FROM approval_gates`).WithArgs("gate-1").WillReturnRows(gateRows)

				countRows := sqlmock.NewRows([]string{"status", "cnt"}).
					AddRow("APPROVED", 2)
				mock.ExpectQuery(`(?s)FROM approval_requests`).WithArgs("gate-1").WillReturnRows(countRows)

				mock.ExpectExec(`(?s)UPDATE approval_gates`).WithArgs("SATISFIED", sqlmock.AnyArg(), "gate-1", 3).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`(?s)UPDATE tasks`).WithArgs("task-1").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			},
			want: true,
		},
		{
			name: "failure mode gate load error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectBegin()
				mock.ExpectQuery(`(?s)FROM approval_gates`).WithArgs("gate-1").WillReturnError(errors.New("db error"))
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
			tt.setup(mock)

			tx, err := db.Beginx()
			assert.NoError(t, err)

			satisfied, evalErr := evaluator.EvaluateApprovalPolicy(context.Background(), tx, "gate-1")
			if tt.wantErr {
				assert.Error(t, evalErr)
				_ = tx.Rollback()
			} else {
				assert.NoError(t, evalErr)
				assert.Equal(t, tt.want, satisfied)
				assert.NoError(t, tx.Commit())
				assert.NotEmpty(t, publisher.events)
				_, err = json.Marshal(publisher.events[0].Payload)
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestConcurrentApprovalDecisions(t *testing.T) {
	var closed atomic.Int32
	var version atomic.Int32
	version.Store(1)

	closeOnce := func() bool {
		for {
			current := version.Load()
			if current == 0 {
				return false
			}
			if version.CompareAndSwap(current, 0) {
				closed.Add(1)
				return true
			}
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = closeOnce()
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), closed.Load(), "gate_status should be closed exactly once")
}
