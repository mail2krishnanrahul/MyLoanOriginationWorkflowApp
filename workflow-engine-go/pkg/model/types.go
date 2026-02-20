package model

import (
	"encoding/json"
	"time"
)

// Global Status Constants
const (
	StatusOpen   = "OPEN"
	StatusClosed = "CLOSED"
)

// Legacy Task Status Constants (pre-v5 schema)
// Deprecated: use TaskStatus* typed constants from task.go instead.
const (
	LegacyTaskStatusPending = "PENDING"
	LegacyTaskStatusDone    = "DONE"
	LegacyTaskStatusManual  = "MANUAL_INTERVENTION"
)

// Outbox Event Types
const (
	EventTypeTaskCompleted = "TASK_COMPLETED"
)

// Outbox Status Constants
const (
	OutboxStatusPending   = "PENDING"
	OutboxStatusProcessed = "DELIVERED"
	OutboxStatusFailed    = "FAILED"
)

// Case represents a workflow instance
type Case struct {
	ID                   string                 `json:"id"`
	WorkflowDefinitionID int64                  `json:"workflow_definition_id"`
	GlobalStatus         string                 `json:"global_status"`
	CurrentStageID       *int64                 `json:"current_stage_id"`
	ApplicantData        map[string]interface{} `json:"applicant_data"`
	Version              int                    `json:"version"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

// TaskInstance represents a specific task execution
type TaskInstance struct {
	ID               string                 `json:"id"`
	CaseID           string                 `json:"case_id"`
	TaskDefinitionID int64                  `json:"task_definition_id"`
	Status           string                 `json:"status"`
	AssignedTo       *string                `json:"assigned_to"`
	DataPayload      map[string]interface{} `json:"data_payload"`
	CreatedAt        time.Time              `json:"created_at"`
	CompletedAt      *time.Time             `json:"completed_at"`
}

// OutboxEvent represents an event in the events_outbox table.
type OutboxEvent struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id,omitempty"`
	CaseID      *string         `json:"case_id,omitempty"`
	TaskID      *string         `json:"task_id,omitempty"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	DeliveredAt *time.Time      `json:"delivered_at"`
	Attempts    int             `json:"attempts"`
}

// PayloadMap is a convenience method that unmarshals the JSONB payload into
// a map. Returns an empty map (never nil) if the payload is empty or invalid.
func (e OutboxEvent) PayloadMap() map[string]interface{} {
	var m map[string]interface{}
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &m); err != nil {
			return map[string]interface{}{}
		}
	}
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}

// LegacyStageDefinition represents a stage in the old (pre-v5) workflow schema.
// Deprecated: use StageDefinition from stage_definition.go instead.
type LegacyStageDefinition struct {
	ID                   int64                  `json:"id"`
	WorkflowDefinitionID int64                  `json:"workflow_definition_id"`
	Name                 string                 `json:"name"`
	SequenceOrder        int                    `json:"sequence_order"`
	ConfigJSON           map[string]interface{} `json:"config_json"`
	CreatedAt            time.Time              `json:"created_at"`
}

// WorkflowDefinition represents a versioned workflow blueprint
type WorkflowDefinition struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Version     int       `json:"version"`
	VersionHash string    `json:"version_hash"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}
