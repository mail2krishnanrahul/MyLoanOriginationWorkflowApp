package model

import "time"

// ErrorClass is the deterministic classification of a task failure.
type ErrorClass string

const (
	ErrorClassTransient ErrorClass = "TRANSIENT"
	ErrorClassPermanent ErrorClass = "PERMANENT"
	ErrorClassUnknown   ErrorClass = "UNKNOWN"
)

// RetryBackoffStrategy controls how retry delay is calculated.
type RetryBackoffStrategy string

const (
	RetryBackoffStrategyFixed       RetryBackoffStrategy = "FIXED"
	RetryBackoffStrategyLinear      RetryBackoffStrategy = "LINEAR"
	RetryBackoffStrategyExponential RetryBackoffStrategy = "EXPONENTIAL"
)

// TaskFailureSeverity controls escalation behavior to case-level exceptions.
type TaskFailureSeverity string

const (
	TaskFailureSeverityLow      TaskFailureSeverity = "LOW"
	TaskFailureSeverityMedium   TaskFailureSeverity = "MEDIUM"
	TaskFailureSeverityHigh     TaskFailureSeverity = "HIGH"
	TaskFailureSeverityCritical TaskFailureSeverity = "CRITICAL"
	TaskFailureSeverityBlocking TaskFailureSeverity = "BLOCKING"
)

// CompensationStatus tracks compensation lifecycle for saga rollback patterns.
type CompensationStatus string

const (
	CompensationStatusPending    CompensationStatus = "PENDING"
	CompensationStatusInProgress CompensationStatus = "IN_PROGRESS"
	CompensationStatusCompleted  CompensationStatus = "COMPLETED"
	CompensationStatusFailed     CompensationStatus = "FAILED"
)

// RetryAttemptOutcome records what happened for each failure attempt.
type RetryAttemptOutcome string

const (
	RetryAttemptOutcomeScheduled RetryAttemptOutcome = "RETRY_SCHEDULED"
	RetryAttemptOutcomeTerminal  RetryAttemptOutcome = "FAILED_TERMINAL"
	RetryAttemptOutcomeRequeued  RetryAttemptOutcome = "DLQ_REQUEUED"
)

// TaskRetryPolicy is configured on each task_definition in case_type config.
type TaskRetryPolicy struct {
	MaxRetries          int                  `json:"max_retries,omitempty"`
	BackoffStrategy     RetryBackoffStrategy `json:"backoff_strategy,omitempty"`
	BaseIntervalSeconds int                  `json:"base_interval_seconds,omitempty"`
	MaxIntervalSeconds  int                  `json:"max_interval_seconds,omitempty"`
	RetryableErrorCodes []string             `json:"retryable_error_codes,omitempty"`
}

// TaskErrorDetail captures structured failure context persisted to tasks.error_detail.
type TaskErrorDetail struct {
	ErrorCode     string      `json:"error_code"`
	ErrorClass    ErrorClass  `json:"error_class"`
	Message       string      `json:"message"`
	SourceService string      `json:"source_service"`
	OccurredAt    time.Time   `json:"occurred_at"`
	StackContext  *string     `json:"stack_context,omitempty"`
	UpstreamError interface{} `json:"upstream_error,omitempty"`
}
