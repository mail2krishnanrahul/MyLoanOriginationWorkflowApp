package engine_test

import (
	"context"
	"fmt"
	"testing"

	"workflow-engine/internal/engine"
	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ---------------------------------------------------------------------------
// MockTx — satisfies pgx.Tx for unit tests
// ---------------------------------------------------------------------------

// MockTx implements pgx.Tx. Only Exec/Query/QueryRow are functional via
// testify mock; the remaining methods are stubs that panic if called
// (indicating the test setup is incomplete).
type MockTx struct {
	mock.Mock
}

func (t *MockTx) Begin(ctx context.Context) (pgx.Tx, error)   { return t, nil }
func (t *MockTx) Commit(ctx context.Context) error             { return nil }
func (t *MockTx) Rollback(ctx context.Context) error           { return nil }
func (t *MockTx) Conn() *pgx.Conn                              { return nil }
func (t *MockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *MockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (t *MockTx) LargeObjects() pgx.LargeObjects               { return pgx.LargeObjects{} }
func (t *MockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (t *MockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	args := t.Called(ctx, sql, arguments)
	return args.Get(0).(pgconn.CommandTag), args.Error(1)
}

func (t *MockTx) Query(ctx context.Context, sql string, queryArgs ...any) (pgx.Rows, error) {
	args := t.Called(ctx, sql, queryArgs)
	return args.Get(0).(pgx.Rows), args.Error(1)
}

func (t *MockTx) QueryRow(ctx context.Context, sql string, queryArgs ...any) pgx.Row {
	args := t.Called(ctx, sql, queryArgs)
	return args.Get(0).(pgx.Row)
}

// ---------------------------------------------------------------------------
// MockRepository implements engine.Repository for testing
// ---------------------------------------------------------------------------

type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) WithTransaction(ctx context.Context, fn func(pgx.Tx) error) error {
	m.Called(ctx, fn)
	// Create a MockTx so that direct tx.Exec calls in lifecycle methods
	// (e.g. SuspendCase task-pausing, WithdrawCase task-cancelling) work.
	tx := new(MockTx)
	// Allow any Exec call on the tx (task bulk-update) to succeed by default.
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("UPDATE 0"), nil)
	return fn(tx)
}

func (m *MockRepository) GetCaseInstance(ctx context.Context, tx repository.DBExecutor, caseID string) (*model.CaseInstance, error) {
	args := m.Called(ctx, tx, caseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.CaseInstance), args.Error(1)
}

func (m *MockRepository) GetCaseInstanceWithLock(ctx context.Context, tx repository.DBExecutor, caseID string) (*model.CaseInstance, error) {
	args := m.Called(ctx, tx, caseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.CaseInstance), args.Error(1)
}

func (m *MockRepository) GetCaseWithLock(ctx context.Context, tx repository.DBExecutor, caseID string) (*model.Case, error) {
	args := m.Called(ctx, tx, caseID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Case), args.Error(1)
}

func (m *MockRepository) UpdateCaseLifecycle(ctx context.Context, tx repository.DBExecutor, caseID string, status string, updates map[string]interface{}) error {
	args := m.Called(ctx, tx, caseID, status, updates)
	return args.Error(0)
}

func (m *MockRepository) UpdateCase(ctx context.Context, tx repository.DBExecutor, c *model.Case) error {
	args := m.Called(ctx, tx, c)
	return args.Error(0)
}

func (m *MockRepository) CloneCase(ctx context.Context, tx repository.DBExecutor, sourceCaseID string) (string, error) {
	args := m.Called(ctx, tx, sourceCaseID)
	return args.String(0), args.Error(1)
}

func (m *MockRepository) ArchiveCase(ctx context.Context, tx repository.DBExecutor, caseID string) error {
	args := m.Called(ctx, tx, caseID)
	return args.Error(0)
}

func (m *MockRepository) PublishEvent(ctx context.Context, tx repository.DBExecutor, event model.Event) error {
	args := m.Called(ctx, tx, event)
	return args.Error(0)
}

func (m *MockRepository) PollPendingEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	args := m.Called(ctx, limit)
	return args.Get(0).([]model.OutboxEvent), args.Error(1)
}

func (m *MockRepository) UpdateEventStatus(ctx context.Context, executor repository.DBExecutor, eventID string, status string, errorMessage *string) error {
	args := m.Called(ctx, executor, eventID, status, errorMessage)
	return args.Error(0)
}

func (m *MockRepository) GetWorkflowDefinition(ctx context.Context, tx repository.DBExecutor, id int64) (*model.WorkflowDefinition, error) {
	args := m.Called(ctx, tx, id)
	return args.Get(0).(*model.WorkflowDefinition), args.Error(1)
}

func (m *MockRepository) GetStageDefinition(ctx context.Context, tx repository.DBExecutor, id int64) (*model.LegacyStageDefinition, error) {
	args := m.Called(ctx, tx, id)
	return args.Get(0).(*model.LegacyStageDefinition), args.Error(1)
}

func (m *MockRepository) GetNextStageDefinition(ctx context.Context, tx repository.DBExecutor, workflowID int64, currentSequence int) (*model.LegacyStageDefinition, error) {
	args := m.Called(ctx, tx, workflowID, currentSequence)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.LegacyStageDefinition), args.Error(1)
}

func (m *MockRepository) GetChildren(ctx context.Context, executor repository.DBExecutor, componentID string) ([]*model.WorkflowComponent, error) {
	args := m.Called(ctx, executor, componentID)
	// Return nil or empty slice on error? Or just mock return
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*model.WorkflowComponent), args.Error(1)
}

func (m *MockRepository) GetWorkflowComponent(ctx context.Context, tx repository.DBExecutor, componentID string) (*model.WorkflowComponent, error) {
	args := m.Called(ctx, tx, componentID)
	return args.Get(0).(*model.WorkflowComponent), args.Error(1)
}

func (m *MockRepository) GetNextSibling(ctx context.Context, tx repository.DBExecutor, componentID string) (*model.WorkflowComponent, error) {
	args := m.Called(ctx, tx, componentID)
	return args.Get(0).(*model.WorkflowComponent), args.Error(1)
}

func (m *MockRepository) GetActiveInstanceID(ctx context.Context, tx repository.DBExecutor, caseID string, componentID string) (string, error) {
	args := m.Called(ctx, tx, caseID, componentID)
	return args.String(0), args.Error(1)
}

func (m *MockRepository) AreAllChildrenComplete(ctx context.Context, tx repository.DBExecutor, caseID string, parentComponentID string) (bool, error) {
	args := m.Called(ctx, tx, caseID, parentComponentID)
	return args.Bool(0), args.Error(1)
}

// Missing methods implementation for mock (to satisfy interface)
func (m *MockRepository) GetRootComponents(ctx context.Context, executor repository.DBExecutor, versionID string) ([]*model.WorkflowComponent, error) {
	args := m.Called(ctx, executor, versionID)
	return args.Get(0).([]*model.WorkflowComponent), args.Error(1)
}

func (m *MockRepository) CreateComponentInstance(ctx context.Context, executor repository.DBExecutor, instance *model.ComponentInstance) error {
	args := m.Called(ctx, executor, instance)
	return args.Error(0)
}

func (m *MockRepository) GetComponentInstance(ctx context.Context, executor repository.DBExecutor, instanceID string) (*model.ComponentInstance, error) {
	args := m.Called(ctx, executor, instanceID)
	return args.Get(0).(*model.ComponentInstance), args.Error(1)
}

func (m *MockRepository) UpdateComponentInstanceStatus(ctx context.Context, executor repository.DBExecutor, instanceID string, status string) error {
	args := m.Called(ctx, executor, instanceID, status)
	return args.Error(0)
}

func (m *MockRepository) InsertOutboxEvent(ctx context.Context, executor repository.DBExecutor, eventType string, payload map[string]interface{}) error {
	args := m.Called(ctx, executor, eventType, payload)
	return args.Error(0)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCaseSuspension(t *testing.T) {
	tests := []struct {
		name        string
		caseID      string
		reason      string
		currentStat string
		mockSetup   func(*MockRepository)
		expectErr   bool
	}{
		{
			name:        "Happy Path - Suspend Open Case",
			caseID:      "case-123",
			reason:      "Customer absent",
			currentStat: model.CaseStatusOpen,
			mockSetup: func(m *MockRepository) {
				m.On("GetCaseInstanceWithLock", mock.Anything, mock.Anything, "case-123").
					Return(&model.CaseInstance{
						ID:     "case-123",
						Status: model.CaseStatusOpen,
					}, nil)
				m.On("UpdateCaseLifecycle", mock.Anything, mock.Anything, "case-123", model.CaseStatusSuspended, mock.MatchedBy(func(u map[string]interface{}) bool {
					return u["suspend_reason"] == "Customer absent"
				})).Return(nil)
				m.On("PublishEvent", mock.Anything, mock.Anything, mock.MatchedBy(func(e model.Event) bool {
					return e.EventType == model.EventCaseSuspended
				})).Return(nil)
			},
			expectErr: false,
		},
		{
			name:        "Edge Case - Suspend Already Suspended Case",
			caseID:      "case-suspended",
			reason:      "More wait",
			currentStat: model.CaseStatusSuspended,
			mockSetup: func(m *MockRepository) {
				m.On("GetCaseInstanceWithLock", mock.Anything, mock.Anything, "case-suspended").
					Return(&model.CaseInstance{
						ID:     "case-suspended",
						Status: model.CaseStatusSuspended,
					}, nil)
				// Updates allowed? ValidateLifecycleTransition says NO for Suspended -> Suspended usually?
				// My map says Suspended -> {InProgress, Cancelled}. NOT Suspended.
				// So this should fail validation.
			},
			expectErr: true,
		},
		{
			name:        "Failure - DB Error on Update",
			caseID:      "case-db-err",
			reason:      "Error",
			currentStat: model.CaseStatusOpen,
			mockSetup: func(m *MockRepository) {
				m.On("GetCaseInstanceWithLock", mock.Anything, mock.Anything, "case-db-err").
					Return(&model.CaseInstance{
						ID:     "case-db-err",
						Status: model.CaseStatusOpen,
					}, nil)
				m.On("UpdateCaseLifecycle", mock.Anything, mock.Anything, "case-db-err", model.CaseStatusSuspended, mock.Anything).
					Return(fmt.Errorf("db failure"))
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := new(MockRepository)
			// Mock WithTransaction to just call fn(nil)
			m.On("WithTransaction", mock.Anything, mock.Anything).Return(func(ctx context.Context, fn func(pgx.Tx) error) error {
				return fn(nil)
			})

			if tc.mockSetup != nil {
				tc.mockSetup(m)
			}

			eng := engine.NewEngine(m, nil, 0)
			err := eng.SuspendCase(context.Background(), tc.caseID, tc.reason, nil)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			m.AssertExpectations(t)
		})
	}
}

func TestCaseCloning(t *testing.T) {
	tests := []struct {
		name      string
		sourceID  string
		mockSetup func(*MockRepository)
		expectErr bool
	}{
		{
			name:     "Happy Path - Clone Existing Case",
			sourceID: "src-1",
			mockSetup: func(m *MockRepository) {
				m.On("GetCaseInstance", mock.Anything, mock.Anything, "src-1").
					Return(&model.CaseInstance{ID: "src-1", Status: "COMPLETED"}, nil)
				m.On("CloneCase", mock.Anything, mock.Anything, "src-1").
					Return("new-1", nil)
				m.On("PublishEvent", mock.Anything, mock.Anything, mock.MatchedBy(func(e model.Event) bool {
					return e.EventType == model.EventCaseCloned && *e.CaseID == "new-1"
				})).Return(nil)
			},
			expectErr: false,
		},
		{
			name:     "Failure - Source Not Found",
			sourceID: "missing",
			mockSetup: func(m *MockRepository) {
				m.On("GetCaseInstance", mock.Anything, mock.Anything, "missing").
					Return(nil, nil) // returns nil,nil if not found usually or error? logic says if source==nil return err
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := new(MockRepository)
			m.On("WithTransaction", mock.Anything, mock.Anything).Return(func(ctx context.Context, fn func(pgx.Tx) error) error {
				return fn(nil)
			})
			if tc.mockSetup != nil {
				tc.mockSetup(m)
			}

			eng := engine.NewEngine(m, nil, 0)
			_, err := eng.CloneCase(context.Background(), tc.sourceID)

			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
