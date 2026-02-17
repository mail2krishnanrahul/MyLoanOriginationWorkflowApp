package engine

import (
	"context"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
)

// Repository defines the data access methods required by the Engine.
type Repository interface {
	// Transaction
	WithTransaction(ctx context.Context, fn func(pgx.Tx) error) error

	// Case ops
	GetCaseInstance(ctx context.Context, tx repository.DBExecutor, caseID string) (*model.CaseInstance, error)
	GetCaseInstanceWithLock(ctx context.Context, tx repository.DBExecutor, caseID string) (*model.CaseInstance, error)
	GetCaseWithLock(ctx context.Context, tx repository.DBExecutor, caseID string) (*model.Case, error)
	UpdateCaseLifecycle(ctx context.Context, tx repository.DBExecutor, caseID string, status string, updates map[string]interface{}) error
	UpdateCase(ctx context.Context, tx repository.DBExecutor, c *model.Case) error
	CloneCase(ctx context.Context, tx repository.DBExecutor, sourceCaseID string) (string, error)
	ArchiveCase(ctx context.Context, tx repository.DBExecutor, caseID string) error

	// Event ops
	PublishEvent(ctx context.Context, tx repository.DBExecutor, event model.Event) error
	PollPendingEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error)
	UpdateEventStatus(ctx context.Context, executor repository.DBExecutor, eventID string, status string, errorMessage *string) error
	InsertOutboxEvent(ctx context.Context, executor repository.DBExecutor, eventType string, payload map[string]interface{}) error

	// Definitions & Recursive Workflow
	GetWorkflowDefinition(ctx context.Context, tx repository.DBExecutor, id int64) (*model.WorkflowDefinition, error)
	GetStageDefinition(ctx context.Context, tx repository.DBExecutor, id int64) (*model.LegacyStageDefinition, error)
	GetNextStageDefinition(ctx context.Context, tx repository.DBExecutor, workflowID int64, currentSequence int) (*model.LegacyStageDefinition, error)

	GetRootComponents(ctx context.Context, executor repository.DBExecutor, versionID string) ([]*model.WorkflowComponent, error)
	CreateComponentInstance(ctx context.Context, executor repository.DBExecutor, instance *model.ComponentInstance) error
	GetChildren(ctx context.Context, executor repository.DBExecutor, componentID string) ([]*model.WorkflowComponent, error)
	UpdateComponentInstanceStatus(ctx context.Context, executor repository.DBExecutor, instanceID string, status string) error
	GetComponentInstance(ctx context.Context, executor repository.DBExecutor, instanceID string) (*model.ComponentInstance, error)
	GetWorkflowComponent(ctx context.Context, executor repository.DBExecutor, componentID string) (*model.WorkflowComponent, error)
	GetNextSibling(ctx context.Context, executor repository.DBExecutor, componentID string) (*model.WorkflowComponent, error)
	GetActiveInstanceID(ctx context.Context, executor repository.DBExecutor, caseID string, componentID string) (string, error)
	AreAllChildrenComplete(ctx context.Context, executor repository.DBExecutor, caseID string, parentComponentID string) (bool, error)
}
