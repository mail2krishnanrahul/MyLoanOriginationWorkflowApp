package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// TaskStatus — lifecycle states for a task
// ---------------------------------------------------------------------------

type TaskStatus string

const (
	TaskStatusPending          TaskStatus = "PENDING"
	TaskStatusAssigned         TaskStatus = "ASSIGNED"
	TaskStatusInProgress       TaskStatus = "IN_PROGRESS"
	TaskStatusAwaitingExternal TaskStatus = "AWAITING_EXTERNAL"
	TaskStatusDone             TaskStatus = "COMPLETED"
	TaskStatusFailed           TaskStatus = "FAILED"
	TaskStatusCancelled        TaskStatus = "CANCELLED"
	TaskStatusSkipped          TaskStatus = "SKIPPED"
)

// IsTerminal returns true for statuses that cannot transition further.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskStatusDone, TaskStatusFailed, TaskStatusCancelled, TaskStatusSkipped:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// TaskPriority — sort-friendly integer enum (1=LOW .. 4=CRITICAL)
// ---------------------------------------------------------------------------

type TaskPriority int

const (
	TaskPriorityLow      TaskPriority = 1
	TaskPriorityNormal   TaskPriority = 2
	TaskPriorityHigh     TaskPriority = 3
	TaskPriorityCritical TaskPriority = 4
)

// String returns the human-readable label for a priority.
func (p TaskPriority) String() string {
	switch p {
	case TaskPriorityLow:
		return "LOW"
	case TaskPriorityNormal:
		return "NORMAL"
	case TaskPriorityHigh:
		return "HIGH"
	case TaskPriorityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// ---------------------------------------------------------------------------
// Task — the central operational row
// ---------------------------------------------------------------------------

// Task is the atomic unit of work consumed by workers.
type Task struct {
	ID                 string          `json:"id"                   db:"id"`
	TenantID           string          `json:"tenant_id"            db:"tenant_id"`
	CaseID             string          `json:"case_id"              db:"case_id"`
	TaskDefinitionCode string          `json:"task_definition_code" db:"task_definition_code"`
	ActivityCode       string          `json:"activity_code"        db:"activity_code"`
	StageCode          string          `json:"stage_code"           db:"stage_code"`
	RequiresApproval   bool            `json:"requires_approval"    db:"requires_approval"`
	ApprovalGateID     *string         `json:"approval_gate_id"     db:"approval_gate_id"`
	ApprovalAmount     *float64        `json:"approval_amount"      db:"approval_amount"`
	IsDocumentVerification bool        `json:"is_document_verification" db:"is_document_verification"`
	Status             TaskStatus      `json:"status"               db:"status"`
	Priority           TaskPriority    `json:"priority"             db:"priority"`
	AssignedService    *string         `json:"assigned_service"     db:"assigned_service"`
	AssignedAt         *time.Time      `json:"assigned_at"          db:"assigned_at"`
	StartedAt          *time.Time      `json:"started_at"           db:"started_at"`
	CompletedAt        *time.Time      `json:"completed_at"         db:"completed_at"`
	DueAt              *time.Time      `json:"due_at"               db:"due_at"`
	RetryCount         int             `json:"retry_count"          db:"retry_count"`
	MaxRetries         int             `json:"max_retries"          db:"max_retries"`
	TotalFailureCount  int             `json:"total_failure_count"  db:"total_failure_count"`
	IsPoisonPill       bool            `json:"is_poison_pill"       db:"is_poison_pill"`
	PoisonPillQuarantinedAt *time.Time `json:"poison_pill_quarantined_at" db:"poison_pill_quarantined_at"`
	PoisonPillReason   *string         `json:"poison_pill_reason"   db:"poison_pill_reason"`
	LastErrorCode      *string         `json:"last_error_code"      db:"last_error_code"`
	LastErrorClass     *string         `json:"last_error_class"     db:"last_error_class"`
	InputPayload       json.RawMessage `json:"input_payload"        db:"input_payload"`
	OutputPayload      json.RawMessage `json:"output_payload"       db:"output_payload"`
	Metadata           json.RawMessage `json:"metadata"             db:"metadata"`
	ErrorDetail        json.RawMessage `json:"error_detail"         db:"error_detail"`
	IdempotencyKey     string          `json:"idempotency_key"      db:"idempotency_key"`
	Version            int             `json:"version"              db:"version"`
	CreatedAt          time.Time       `json:"created_at"           db:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"           db:"updated_at"`

	// Assignment Extension
	WorkbasketID   *string         `json:"workbasket_id"   db:"workbasket_id"`
	AssigneeID     *string         `json:"assignee_id"     db:"assignee_id"`
	RequiredSkills json.RawMessage `json:"required_skills" db:"required_skills"`
}
