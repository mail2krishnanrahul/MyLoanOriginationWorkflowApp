package exception

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"workflow-engine/pkg/model"
)

// ErrorCodeCarrier allows worker handlers to provide deterministic error codes.
type ErrorCodeCarrier interface {
	ErrorCode() string
}

// UpstreamErrorCarrier allows preserving raw downstream error payloads.
type UpstreamErrorCarrier interface {
	UpstreamError() interface{}
}

// StackContextCarrier allows explicitly attaching recovered panic stack traces.
type StackContextCarrier interface {
	StackContext() string
}

// TaskFailureInput is the normalized failure context received from worker execution.
type TaskFailureInput struct {
	TaskID         string
	SourceService  string
	Err            error
	RecoveredStack *string
}

// TaskDLQEntry is one dead-letter queue record.
type TaskDLQEntry struct {
	DLQID                string          `json:"dlq_id" db:"dlq_id"`
	TaskID               string          `json:"task_id" db:"task_id"`
	CaseID               string          `json:"case_id" db:"case_id"`
	FailureReason        string          `json:"failure_reason" db:"failure_reason"`
	ErrorDetail          json.RawMessage `json:"error_detail" db:"error_detail"`
	MovedAt              time.Time       `json:"moved_at" db:"moved_at"`
	RequeueCount         int             `json:"requeue_count" db:"requeue_count"`
	LastRequeueAt        *time.Time      `json:"last_requeue_at,omitempty" db:"last_requeue_at"`
	IsPoisonPill         bool            `json:"is_poison_pill" db:"is_poison_pill"`
	QuarantineReleasedAt *time.Time      `json:"quarantine_released_at,omitempty" db:"quarantine_released_at"`
	SoftDeletedAt        *time.Time      `json:"soft_deleted_at,omitempty" db:"soft_deleted_at"`
	CreatedAt            time.Time       `json:"created_at" db:"created_at"`
}

// RetryHistoryEntry is one retry attempt record.
type RetryHistoryEntry struct {
	AttemptID               string                `json:"attempt_id" db:"attempt_id"`
	TaskID                  string                `json:"task_id" db:"task_id"`
	CaseID                  string                `json:"case_id" db:"case_id"`
	AttemptNumber           int                   `json:"attempt_number" db:"attempt_number"`
	RetryCountBefore        int                   `json:"retry_count_before" db:"retry_count_before"`
	MaxRetries              int                   `json:"max_retries" db:"max_retries"`
	BackoffStrategy         model.RetryBackoffStrategy `json:"backoff_strategy" db:"backoff_strategy"`
	BaseIntervalSeconds     int                   `json:"base_interval_seconds" db:"base_interval_seconds"`
	MaxIntervalSeconds      int                   `json:"max_interval_seconds" db:"max_interval_seconds"`
	ComputedIntervalSeconds int                   `json:"computed_interval_seconds" db:"computed_interval_seconds"`
	ScheduledAt             time.Time             `json:"scheduled_at" db:"scheduled_at"`
	NextAttemptAt           *time.Time            `json:"next_attempt_at,omitempty" db:"next_attempt_at"`
	ErrorCode               string                `json:"error_code" db:"error_code"`
	ErrorClass              model.ErrorClass      `json:"error_class" db:"error_class"`
	SourceService           string                `json:"source_service" db:"source_service"`
	Outcome                 model.RetryAttemptOutcome `json:"outcome" db:"outcome"`
}

// ExceptionCaseSummary powers exception dashboard case listings.
type ExceptionCaseSummary struct {
	CaseID             string     `json:"case_id" db:"case_id"`
	ReferenceNumber    string     `json:"reference_number" db:"reference_number"`
	CaseTypeID         string     `json:"case_type_id" db:"case_type_id"`
	Status             string     `json:"status" db:"status"`
	ExceptionAt        *time.Time `json:"exception_at,omitempty" db:"exception_at"`
	ExceptionReason    *string    `json:"exception_reason,omitempty" db:"exception_reason"`
	ExceptionSeverity  *string    `json:"exception_severity,omitempty" db:"exception_severity"`
	ExceptionTaskID    *string    `json:"exception_task_id,omitempty" db:"exception_task_id"`
	TaskDefinitionCode *string    `json:"task_definition_code,omitempty" db:"task_definition_code"`
	LastErrorCode      *string    `json:"last_error_code,omitempty" db:"last_error_code"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
}

// RetryDecision is the computed handling path for a failure.
type RetryDecision struct {
	Policy            model.TaskRetryPolicy
	ErrorCode         string
	ErrorClass        model.ErrorClass
	ShouldRetry       bool
	RetriesExhausted  bool
	AttemptNumber     int
	ComputedBackoff   time.Duration
	PoisonPill        bool
	PoisonPillReason  string
}

// ValidationError indicates invalid exception configuration/inputs.
type ValidationError struct {
	Operation string `json:"operation"`
	Message   string `json:"message"`
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "validation error"
	}
	op := strings.TrimSpace(e.Operation)
	if op == "" {
		return strings.TrimSpace(e.Message)
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("%s: validation error", op)
	}
	return fmt.Sprintf("%s: %s", op, strings.TrimSpace(e.Message))
}
