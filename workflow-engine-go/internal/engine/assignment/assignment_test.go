package assignment_test

import (
	"context"
	"testing"

	"workflow-engine/internal/engine/assignment"
	"workflow-engine/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ---------------------------------------------------------------------------
// MockTx — satisfies repository.DBExecutor for unit tests
// ---------------------------------------------------------------------------

type MockTx struct {
	mock.Mock
}

func (t *MockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	args := t.Called(ctx, sql, arguments)
	return args.Get(0).(pgconn.CommandTag), args.Error(1)
}

func (t *MockTx) Query(ctx context.Context, sql string, queryArgs ...any) (pgx.Rows, error) {
	args := t.Called(ctx, sql, queryArgs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Rows), args.Error(1)
}

func (t *MockTx) QueryRow(ctx context.Context, sql string, queryArgs ...any) pgx.Row {
	args := t.Called(ctx, sql, queryArgs)
	return args.Get(0).(pgx.Row)
}

// ---------------------------------------------------------------------------
// MockRepo — satisfies repository.Repository for Manager tests
// ---------------------------------------------------------------------------

type MockRepo struct {
	mock.Mock
	Pool interface{} // unused stub
}

func (m *MockRepo) WithTransaction(ctx context.Context, fn func(pgx.Tx) error) error {
	return fn(nil)
}

func (m *MockRepo) InsertOutboxEvent(ctx context.Context, executor repository.DBExecutor, eventType string, payload map[string]interface{}) error {
	args := m.Called(ctx, executor, eventType, payload)
	return args.Error(0)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAssignToWorkbasket(t *testing.T) {
	tests := []struct {
		name      string
		taskID    string
		basketID  string
		mockSetup func(*MockRepo, *MockTx)
		expectErr bool
	}{
		{
			name:     "Happy Path",
			taskID:   "task-1",
			basketID: "basket-1",
			mockSetup: func(r *MockRepo, tx *MockTx) {
				tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
					Return(pgconn.NewCommandTag("UPDATE 1"), nil)
				r.On("InsertOutboxEvent", mock.Anything, tx, "TASK_QUEUED", mock.Anything).
					Return(nil)
			},
			expectErr: false,
		},
		{
			name:     "DB Exec Failure",
			taskID:   "task-2",
			basketID: "basket-2",
			mockSetup: func(r *MockRepo, tx *MockTx) {
				tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
					Return(pgconn.CommandTag{}, assert.AnError)
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := new(MockRepo)
			tx := new(MockTx)

			if tc.mockSetup != nil {
				tc.mockSetup(repo, tx)
			}

			mgr := assignment.NewManager(repo)
			err := mgr.AssignToWorkbasket(context.Background(), tx, tc.taskID, tc.basketID)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
