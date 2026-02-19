package model

import (
	"encoding/json"
	"time"
)

// NotificationChannel identifies a delivery medium.
type NotificationChannel string

const (
	NotificationChannelEmail   NotificationChannel = "EMAIL"
	NotificationChannelSMS     NotificationChannel = "SMS"
	NotificationChannelPush    NotificationChannel = "PUSH"
	NotificationChannelInApp   NotificationChannel = "IN_APP"
	NotificationChannelWebhook NotificationChannel = "WEBHOOK"
)

// NotificationTemplateStatus controls lifecycle of template definitions.
type NotificationTemplateStatus string

const (
	NotificationTemplateStatusDraft      NotificationTemplateStatus = "DRAFT"
	NotificationTemplateStatusActive     NotificationTemplateStatus = "ACTIVE"
	NotificationTemplateStatusDeprecated NotificationTemplateStatus = "DEPRECATED"
)

// NotificationRecipientType determines recipient resolution strategy.
type NotificationRecipientType string

const (
	NotificationRecipientCaseOwner    NotificationRecipientType = "CASE_OWNER"
	NotificationRecipientTaskAssignee NotificationRecipientType = "TASK_ASSIGNEE"
	NotificationRecipientApprover     NotificationRecipientType = "APPROVER"
	NotificationRecipientSupervisor   NotificationRecipientType = "SUPERVISOR"
	NotificationRecipientBorrower     NotificationRecipientType = "BORROWER"
	NotificationRecipientFixedAddress NotificationRecipientType = "FIXED_ADDRESS"
	NotificationRecipientDynamicRule  NotificationRecipientType = "DYNAMIC_RULE"
)

// NotificationPriority is used by queue polling and dispatch ordering.
type NotificationPriority string

const (
	NotificationPriorityLow    NotificationPriority = "LOW"
	NotificationPriorityNormal NotificationPriority = "NORMAL"
	NotificationPriorityHigh   NotificationPriority = "HIGH"
	NotificationPriorityUrgent NotificationPriority = "URGENT"
)

// NotificationStatus represents notification_queue state.
type NotificationStatus string

const (
	NotificationStatusPending    NotificationStatus = "PENDING"
	NotificationStatusSent       NotificationStatus = "SENT"
	NotificationStatusFailed     NotificationStatus = "FAILED"
	NotificationStatusSuppressed NotificationStatus = "SUPPRESSED"
	NotificationStatusCancelled  NotificationStatus = "CANCELLED"
)

// NotificationSuppressionReason records why a notification was not sent.
type NotificationSuppressionReason string

const (
	NotificationSuppressionDuplicate    NotificationSuppressionReason = "DUPLICATE"
	NotificationSuppressionOptOut       NotificationSuppressionReason = "OPT_OUT"
	NotificationSuppressionQuietHours   NotificationSuppressionReason = "QUIET_HOURS"
	NotificationSuppressionTypeDisabled NotificationSuppressionReason = "TYPE_DISABLED"
)

// NotificationDeliveryEventType captures lifecycle events for delivery tracking.
type NotificationDeliveryEventType string

const (
	NotificationDeliveryEventQueued     NotificationDeliveryEventType = "QUEUED"
	NotificationDeliveryEventClaimed    NotificationDeliveryEventType = "CLAIMED"
	NotificationDeliveryEventRendered   NotificationDeliveryEventType = "RENDERED"
	NotificationDeliveryEventDispatched NotificationDeliveryEventType = "DISPATCHED"
	NotificationDeliveryEventDelivered  NotificationDeliveryEventType = "DELIVERED"
	NotificationDeliveryEventOpened     NotificationDeliveryEventType = "OPENED"
	NotificationDeliveryEventClicked    NotificationDeliveryEventType = "CLICKED"
	NotificationDeliveryEventBounced    NotificationDeliveryEventType = "BOUNCED"
	NotificationDeliveryEventFailed     NotificationDeliveryEventType = "FAILED"
)

// CircuitBreakerStateType indicates provider health state for a channel.
type CircuitBreakerStateType string

const (
	CircuitBreakerStateClosed   CircuitBreakerStateType = "CLOSED"
	CircuitBreakerStateOpen     CircuitBreakerStateType = "OPEN"
	CircuitBreakerStateHalfOpen CircuitBreakerStateType = "HALF_OPEN"
)

// NotificationTemplate is the persisted reusable template definition.
type NotificationTemplate struct {
	ID              string                     `json:"id" db:"id"`
	TemplateCode    string                     `json:"template_code" db:"template_code"`
	CaseTypeCode    *string                    `json:"case_type_code" db:"case_type_code"`
	Channel         NotificationChannel        `json:"channel" db:"channel"`
	SubjectTemplate *string                    `json:"subject_template" db:"subject_template"`
	BodyTemplate    string                     `json:"body_template" db:"body_template"`
	LanguageCode    string                     `json:"language_code" db:"language_code"`
	Status          NotificationTemplateStatus `json:"status" db:"status"`
	Version         int                        `json:"version" db:"version"`
	Metadata        json.RawMessage            `json:"metadata" db:"metadata"`
	CreatedAt       time.Time                  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at" db:"updated_at"`
}

// NotificationTrigger defines event-to-template dispatch rules.
type NotificationTrigger struct {
	ID                  string                    `json:"id" db:"id"`
	TriggerCode         string                    `json:"trigger_code" db:"trigger_code"`
	CaseTypeCode        *string                   `json:"case_type_code" db:"case_type_code"`
	EventType           EventType                 `json:"event_type" db:"event_type"`
	FilterExpression    *string                   `json:"filter_expression" db:"filter_expression"`
	TemplateCode        string                    `json:"template_code" db:"template_code"`
	RecipientType       NotificationRecipientType `json:"recipient_type" db:"recipient_type"`
	RecipientValue      *string                   `json:"recipient_value" db:"recipient_value"`
	SendAfterMinutes    int                       `json:"send_after_minutes" db:"send_after_minutes"`
	DedupeWindowMinutes int                       `json:"dedupe_window_minutes" db:"dedupe_window_minutes"`
	Priority            NotificationPriority      `json:"priority" db:"priority"`
	IsEnabled           bool                      `json:"is_enabled" db:"is_enabled"`
	CreatedAt           time.Time                 `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at" db:"updated_at"`
}

// Notification is one queued notification instance.
type Notification struct {
	ID             string               `json:"id" db:"id"`
	TriggerCode    string               `json:"trigger_code" db:"trigger_code"`
	CaseID         *string              `json:"case_id" db:"case_id"`
	TaskID         *string              `json:"task_id" db:"task_id"`
	TemplateCode   string               `json:"template_code" db:"template_code"`
	Channel        NotificationChannel  `json:"channel" db:"channel"`
	Recipient      string               `json:"recipient" db:"recipient"`
	Subject        *string              `json:"subject" db:"subject"`
	Body           *string              `json:"body" db:"body"`
	Priority       NotificationPriority `json:"priority" db:"priority"`
	ScheduledAt    time.Time            `json:"scheduled_at" db:"scheduled_at"`
	Status         NotificationStatus   `json:"status" db:"status"`
	Attempts       int                  `json:"attempts" db:"attempts"`
	LastAttemptAt  *time.Time           `json:"last_attempt_at" db:"last_attempt_at"`
	SentAt         *time.Time           `json:"sent_at" db:"sent_at"`
	ErrorDetail    json.RawMessage      `json:"error_detail" db:"error_detail"`
	AcknowledgedAt *time.Time           `json:"acknowledged_at" db:"acknowledged_at"`
	CreatedAt      time.Time            `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at" db:"updated_at"`
}

// NotificationDeliveryEvent is append-only telemetry/audit for delivery lifecycle.
type NotificationDeliveryEvent struct {
	ID              string                        `json:"id" db:"id"`
	NotificationID  string                        `json:"notification_id" db:"notification_id"`
	EventType       NotificationDeliveryEventType `json:"event_type" db:"event_type"`
	EventTimestamp  time.Time                     `json:"event_timestamp" db:"event_timestamp"`
	ChannelResponse json.RawMessage               `json:"channel_response" db:"channel_response"`
	UserAgent       *string                       `json:"user_agent" db:"user_agent"`
	CreatedAt       time.Time                     `json:"created_at" db:"created_at"`
}

// NotificationSuppressionLog captures suppressions for auditing and compliance review.
type NotificationSuppressionLog struct {
	ID             string                        `json:"id" db:"id"`
	NotificationID *string                       `json:"notification_id" db:"notification_id"`
	TriggerCode    string                        `json:"trigger_code" db:"trigger_code"`
	Recipient      string                        `json:"recipient" db:"recipient"`
	CaseID         *string                       `json:"case_id" db:"case_id"`
	SuppressedAt   time.Time                     `json:"suppressed_at" db:"suppressed_at"`
	Reason         NotificationSuppressionReason `json:"reason" db:"reason"`
	CreatedAt      time.Time                     `json:"created_at" db:"created_at"`
}

// UserPreference stores recipient-level notification controls.
type UserPreference struct {
	ID                       string               `json:"id" db:"id"`
	UserID                   string               `json:"user_id" db:"user_id"`
	Channel                  *NotificationChannel `json:"channel" db:"channel"`
	OptOut                   bool                 `json:"opt_out" db:"opt_out"`
	QuietHoursStart          *string              `json:"quiet_hours_start" db:"quiet_hours_start"`
	QuietHoursEnd            *string              `json:"quiet_hours_end" db:"quiet_hours_end"`
	QuietHoursTimezone       *string              `json:"quiet_hours_timezone" db:"quiet_hours_timezone"`
	EnabledNotificationTypes json.RawMessage      `json:"enabled_notification_types" db:"enabled_notification_types"`
	CreatedAt                time.Time            `json:"created_at" db:"created_at"`
	UpdatedAt                time.Time            `json:"updated_at" db:"updated_at"`
}

// NotificationCircuitBreakerState is persisted per channel.
type NotificationCircuitBreakerState struct {
	Channel           NotificationChannel   `json:"channel" db:"channel"`
	State             CircuitBreakerStateType `json:"state" db:"state"`
	FailureCount      int                   `json:"failure_count" db:"failure_count"`
	SuccessCount      int                   `json:"success_count" db:"success_count"`
	LastFailureAt     *time.Time            `json:"last_failure_at" db:"last_failure_at"`
	OpenedAt          *time.Time            `json:"opened_at" db:"opened_at"`
	HalfOpenAt        *time.Time            `json:"half_open_at" db:"half_open_at"`
	ThresholdFailures int                   `json:"threshold_failures" db:"threshold_failures"`
	CooldownSeconds   int                   `json:"cooldown_seconds" db:"cooldown_seconds"`
	CreatedAt         time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at" db:"updated_at"`
}

// NotificationRecord is a query projection with event history.
type NotificationRecord struct {
	Notification Notification               `json:"notification"`
	Events       []NotificationDeliveryEvent `json:"events"`
}

// DeliveryStats summarizes delivery reliability for reporting.
type DeliveryStats struct {
	Channel   NotificationChannel `json:"channel"`
	TotalSent int64               `json:"total_sent"`
	Delivered int64               `json:"delivered"`
	Failed    int64               `json:"failed"`
	Bounced   int64               `json:"bounced"`
	BounceRate float64            `json:"bounce_rate"`
}

// CorrespondenceSummary aggregates notification metrics for a case.
type CorrespondenceSummary struct {
	CaseID                      string          `json:"case_id" db:"case_id"`
	TotalSent                   int64           `json:"total_sent" db:"total_sent"`
	SentByChannel               json.RawMessage `json:"sent_by_channel" db:"sent_by_channel"`
	UnacknowledgedBorrowerCount int64           `json:"unacknowledged_borrower_count" db:"unacknowledged_borrower_count"`
	FailedCount                 int64           `json:"failed_count" db:"failed_count"`
	FailedReasons               json.RawMessage `json:"failed_reasons" db:"failed_reasons"`
	AvgDeliverySeconds          *float64        `json:"avg_delivery_seconds" db:"avg_delivery_seconds"`
	RefreshedAt                 time.Time       `json:"refreshed_at" db:"refreshed_at"`
}
