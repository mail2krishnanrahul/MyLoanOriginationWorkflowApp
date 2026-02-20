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
	EventTaskRequeued      EventType = "TASK_REQUEUED"
	EventTaskAssigned      EventType = "TASK_ASSIGNED"
	EventCaseCreated       EventType = "CASE_CREATED"
	EventCaseCompleted     EventType = "CASE_COMPLETED"
	EventCaseStageChanged  EventType = "CASE_STAGE_CHANGED"
	EventActivityCompleted EventType = "ACTIVITY_COMPLETED"
	EventCaseExceptionRaised     EventType = "CASE_EXCEPTION_RAISED"
	EventCaseExceptionPropagated EventType = "CASE_EXCEPTION_PROPAGATED"
	EventTaskPoisonPillDetected  EventType = "TASK_POISON_PILL_DETECTED"
	EventCompensationStarted     EventType = "COMPENSATION_STARTED"
	EventCompensationCompleted   EventType = "COMPENSATION_COMPLETED"
	EventCompensationFailed      EventType = "COMPENSATION_FAILED"

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

	// Approval Events
	EventApprovalGateCreated  EventType = "APPROVAL_GATE_CREATED"
	EventApprovalRequested    EventType = "APPROVAL_REQUESTED"
	EventApprovalGranted      EventType = "APPROVAL_GRANTED"
	EventApprovalRejected     EventType = "APPROVAL_REJECTED"
	EventApprovalDelegated    EventType = "APPROVAL_DELEGATED"
	EventApprovalExpired      EventType = "APPROVAL_EXPIRED"
	EventApprovalGateSatisfied EventType = "APPROVAL_GATE_SATISFIED"
	EventApprovalGateFailed   EventType = "APPROVAL_GATE_FAILED"
	EventCaseSentToRework     EventType = "CASE_SENT_TO_REWORK"
	EventCaseRejected         EventType = "CASE_REJECTED"
	EventCaseMaxReworkExceeded EventType = "CASE_MAX_REWORK_EXCEEDED"
	EventNoEligibleApprover   EventType = "NO_ELIGIBLE_APPROVER"

	// Document/Data Management Events
	EventDocumentUploaded            EventType = "DOCUMENT_UPLOADED"
	EventDocumentVerified            EventType = "DOCUMENT_VERIFIED"
	EventDocumentRejected            EventType = "DOCUMENT_REJECTED"
	EventDocumentDeleted             EventType = "DOCUMENT_DELETED"
	EventDocumentArchived            EventType = "DOCUMENT_ARCHIVED"
	EventDocumentVersionCreated      EventType = "DOCUMENT_VERSION_CREATED"
	EventDocumentRequirementFulfilled EventType = "DOCUMENT_REQUIREMENT_FULFILLED"
	EventStageTransitionBlocked      EventType = "STAGE_TRANSITION_BLOCKED"
	EventTaskCreationBlocked         EventType = "TASK_CREATION_BLOCKED"

	// Versioning & Configuration Governance Events
	EventCaseTypeActivated    EventType = "CASE_TYPE_ACTIVATED"
	EventCaseTypeDeprecated   EventType = "CASE_TYPE_DEPRECATED"
	EventCaseVersionMigrated  EventType = "CASE_VERSION_MIGRATED"

	// Multi-tenancy lifecycle events
	EventTenantOnboarded     EventType = "TENANT_ONBOARDED"
	EventTenantOffboarded    EventType = "TENANT_OFFBOARDED"
	EventTenantConfigUpdated EventType = "TENANT_CONFIG_UPDATED"

	// Notification Internal Events
	EventNotificationQueued       EventType = "NOTIFICATION_QUEUED"
	EventNotificationSent         EventType = "NOTIFICATION_SENT"
	EventNotificationFailed       EventType = "NOTIFICATION_FAILED"
	EventNotificationSuppressed   EventType = "NOTIFICATION_SUPPRESSED"
	EventCircuitBreakerOpened     EventType = "CIRCUIT_BREAKER_OPENED"

	// Integration & Extensibility Events
	EventWebhookSubscriptionFailed EventType = "WEBHOOK_SUBSCRIPTION_FAILED"
	EventServiceOffline            EventType = "SERVICE_OFFLINE"
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
	TenantID        string          `json:"tenant_id"         db:"tenant_id"`
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
