package model

import (
	"encoding/json"
	"time"
)

// ExecutionStrategy defines how children are executed
type ExecutionStrategy string

const (
	StrategySequential ExecutionStrategy = "SEQUENTIAL"
	StrategyParallel   ExecutionStrategy = "PARALLEL"
)

// ComponentType defines the type of a node in the workflow tree
type ComponentType string

const (
	ComponentTypeStage    ComponentType = "STAGE"
	ComponentTypeActivity ComponentType = "ACTIVITY"
	ComponentTypeTask     ComponentType = "TASK"
)

// CaseDefinition represents a high-level workflow type (e.g. Business Loan)
type CaseDefinition struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// VersionRegistry represents a snapshot of a workflow definition
type VersionRegistry struct {
	ID               string    `json:"id"`
	CaseDefinitionID string    `json:"case_definition_id"`
	Version          int       `json:"version"`
	Status           string    `json:"status"` // DRAFT, ACTIVE
	CreatedAt        time.Time `json:"created_at"`
}

// WorkflowComponent represents a node in the recursive workflow tree
type WorkflowComponent struct {
	ID                string            `json:"id"`
	VersionID         string            `json:"version_id"`
	ParentComponentID *string           `json:"parent_component_id,omitempty"`
	Type              ComponentType     `json:"type"`
	Name              string            `json:"name"`
	ExecutionStrategy ExecutionStrategy `json:"execution_strategy"` // SEQUENTIAL, PARALLEL
	ExecutionOrder    int               `json:"execution_order"`
	Config            json.RawMessage   `json:"config,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`

	// Children are populated during tree traversal/loading
	Children []*WorkflowComponent `json:"children,omitempty"`
}

// ComponentHook represents logic to run before or after a component
type ComponentHook struct {
	ID             string          `json:"id"`
	ComponentID    string          `json:"component_id"`
	Type           string          `json:"type"`   // PRE_EXECUTE, POST_EXECUTE
	Action         string          `json:"action"` // NOTIFY, WEBHOOK
	Config         json.RawMessage `json:"config,omitempty"`
	ExecutionOrder int             `json:"execution_order"`
}

// ComponentInstance tracks the execution state of a specific component for a case
type ComponentInstance struct {
	ID          string          `json:"id"`
	CaseID      string          `json:"case_id"`
	ComponentID string          `json:"component_id"`
	Status      string          `json:"status"` // PENDING, IN_PROGRESS, COMPLETED, FAILED
	DataPayload json.RawMessage `json:"data_payload,omitempty"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}
