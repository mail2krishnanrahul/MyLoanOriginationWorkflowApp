package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// EventType constants — all known event types in the system
// ---------------------------------------------------------------------------

type EventType string

const (
	EventTaskCompleted     EventType = "TASK_COMPLETED"
	EventTaskFailed        EventType = "TASK_FAILED"
	EventTaskAssigned      EventType = "TASK_ASSIGNED"
	EventCaseCreated       EventType = "CASE_CREATED"
	EventCaseCompleted     EventType = "CASE_COMPLETED"
	EventCaseStageChanged  EventType = "CASE_STAGE_CHANGED"
	EventActivityCompleted EventType = "ACTIVITY_COMPLETED"

	// Lifecycle Events
	EventCaseCloned          EventType = "CASE_CLONED"
	EventCaseSuspended       EventType = "CASE_SUSPENDED"
	EventCaseResumed         EventType = "CASE_RESUMED"
	EventCaseWithdrawn       EventType = "CASE_WITHDRAWN"
	EventCaseArchived        EventType = "CASE_ARCHIVED"
	EventCaseExpired         EventType = "CASE_EXPIRED"
	EventCaseEmergencyClosed EventType = "CASE_EMERGENCY_CLOSED"

	// Assignment & Routing Events
	EventTaskQueued              EventType = "TASK_QUEUED"
	EventTaskDelegated           EventType = "TASK_DELEGATED"
	EventTaskReassigned          EventType = "TASK_REASSIGNED"
	EventTaskSLAWarning          EventType = "TASK_SLA_WARNING"
	EventTaskSLABreached         EventType = "TASK_SLA_BREACHED"
	EventWorkbasketTaskClaimed   EventType = "WORKBASKET_TASK_CLAIMED"
	EventWorkerCapacityExceeded  EventType = "WORKER_CAPACITY_EXCEEDED"

	// SLA Events (cross-entity and explicit lifecycle)
	EventSLAWarning  EventType = "SLA_WARNING"
	EventSLACritical EventType = "SLA_CRITICAL"
	EventSLABreached EventType = "SLA_BREACHED"
	EventSLAPaused   EventType = "SLA_PAUSED"
	EventSLAResumed  EventType = "SLA_RESUMED"
	EventSLAReset    EventType = "SLA_RESET"
	EventSLAExtended EventType = "SLA_EXTENDED"
)

// ---------------------------------------------------------------------------
// Event status constants
// ---------------------------------------------------------------------------

type EventStatus string

const (
	EventStatusPending   EventStatus = "PENDING"
	EventStatusDelivered EventStatus = "DELIVERED"
	EventStatusFailed    EventStatus = "FAILED"
)

// ---------------------------------------------------------------------------
// Event — one row in the events_outbox table
// ---------------------------------------------------------------------------

// Event represents a domain event stored in the transactional outbox.
type Event struct {
	ID              string          `json:"id"                db:"id"`
	CaseID          *string         `json:"case_id"           db:"case_id"`
	TaskID          *string         `json:"task_id"           db:"task_id"`
	EventType       EventType       `json:"event_type"        db:"event_type"`
	Payload         json.RawMessage `json:"payload"           db:"payload"`
	Status          EventStatus     `json:"status"            db:"status"`
	TargetService   string          `json:"target_service"    db:"target_service"`
	Attempts        int             `json:"attempts"          db:"attempts"`
	MaxAttempts     int             `json:"max_attempts"      db:"max_attempts"`
	LastAttemptedAt *time.Time      `json:"last_attempted_at" db:"last_attempted_at"`
	PartitionKey    *string         `json:"partition_key"     db:"partition_key"`
	TraceID         *string         `json:"trace_id"          db:"trace_id"`
	CreatedAt       time.Time       `json:"created_at"        db:"created_at"`
	DeliveredAt     *time.Time      `json:"delivered_at"      db:"delivered_at"`
}
