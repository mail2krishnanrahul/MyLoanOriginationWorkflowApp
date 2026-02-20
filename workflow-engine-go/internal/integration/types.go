package integration

import (
	"encoding/json"
	"errors"
	"time"

	"workflow-engine/pkg/model"
)

// WebhookSubscriptionStatus is the lifecycle state of a webhook subscription.
type WebhookSubscriptionStatus string

const (
	WebhookSubscriptionStatusActive WebhookSubscriptionStatus = "ACTIVE"
	WebhookSubscriptionStatusPaused WebhookSubscriptionStatus = "PAUSED"
	WebhookSubscriptionStatusFailed WebhookSubscriptionStatus = "FAILED"
)

// WebhookDeliveryStatus is the delivery lifecycle state for queued webhook rows.
type WebhookDeliveryStatus string

const (
	WebhookDeliveryStatusPending   WebhookDeliveryStatus = "PENDING"
	WebhookDeliveryStatusDelivered WebhookDeliveryStatus = "DELIVERED"
	WebhookDeliveryStatusFailed    WebhookDeliveryStatus = "FAILED"
	WebhookDeliveryStatusAbandoned WebhookDeliveryStatus = "ABANDONED"
)

// ExternalServiceStatus is the health/lifecycle state of an external service.
type ExternalServiceStatus string

const (
	ExternalServiceStatusActive         ExternalServiceStatus = "ACTIVE"
	ExternalServiceStatusDegraded       ExternalServiceStatus = "DEGRADED"
	ExternalServiceStatusOffline        ExternalServiceStatus = "OFFLINE"
	ExternalServiceStatusDecommissioned ExternalServiceStatus = "DECOMMISSIONED"
)

// ExternalServiceProtocol describes the integration protocol used by external services.
type ExternalServiceProtocol string

const (
	ExternalServiceProtocolHTTPCallback ExternalServiceProtocol = "HTTP_CALLBACK"
	ExternalServiceProtocolPolling      ExternalServiceProtocol = "POLLING"
	ExternalServiceProtocolEventDriven  ExternalServiceProtocol = "EVENT_DRIVEN"
)

// IdempotencyKeyspace scopes idempotency keys across integration seams.
type IdempotencyKeyspace string

const (
	IdempotencyKeyspaceTaskCompletion         IdempotencyKeyspace = "TASK_COMPLETION"
	IdempotencyKeyspaceExternalEventIngestion IdempotencyKeyspace = "EXTERNAL_EVENT_INGESTION"
	IdempotencyKeyspaceWebhookDelivery        IdempotencyKeyspace = "WEBHOOK_DELIVERY"
)

// EventDirection indicates whether an event type is consumed, emitted, or both.
type EventDirection string

const (
	EventDirectionEmitted  EventDirection = "EMITTED"
	EventDirectionConsumed EventDirection = "CONSUMED"
	EventDirectionBoth     EventDirection = "BOTH"
)

// IntegrationDirection indicates inbound vs outbound integration traffic.
type IntegrationDirection string

const (
	IntegrationDirectionInbound  IntegrationDirection = "INBOUND"
	IntegrationDirectionOutbound IntegrationDirection = "OUTBOUND"
)

// IntegrationType identifies the integration capability generating audit entries.
type IntegrationType string

const (
	IntegrationTypeWebhook                IntegrationType = "WEBHOOK"
	IntegrationTypeExternalTaskCompletion IntegrationType = "EXTERNAL_TASK_COMPLETION"
	IntegrationTypeExternalEventIngestion IntegrationType = "EXTERNAL_EVENT_INGESTION"
	IntegrationTypeHealthCheck            IntegrationType = "HEALTH_CHECK"
)

// IntegrationAuditStatus is the operation result stored in integration_audit_log.
type IntegrationAuditStatus string

const (
	IntegrationAuditStatusSuccess           IntegrationAuditStatus = "SUCCESS"
	IntegrationAuditStatusFailure           IntegrationAuditStatus = "FAILURE"
	IntegrationAuditStatusDuplicateRejected IntegrationAuditStatus = "DUPLICATE_REJECTED"
)

// WebhookSubscription maps webhook_subscriptions rows.
type WebhookSubscription struct {
	SubscriptionID   string                    `db:"subscription_id" json:"subscription_id"`
	TenantID         string                    `db:"tenant_id" json:"tenant_id"`
	SubscriberCode   string                    `db:"subscriber_code" json:"subscriber_code"`
	TargetURL        string                    `db:"target_url" json:"target_url"`
	EventTypes       []string                  `db:"event_types" json:"event_types"`
	SigningSecretEnc []byte                    `db:"signing_secret_enc" json:"-"`
	Status           WebhookSubscriptionStatus `db:"status" json:"status"`
	MaxFailures      int                       `db:"max_failures" json:"max_failures"`
	FailureCount     int                       `db:"failure_count" json:"failure_count"`
	Headers          json.RawMessage           `db:"headers" json:"headers"`
	TimeoutSeconds   int                       `db:"timeout_seconds" json:"timeout_seconds"`
	CreatedAt        time.Time                 `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time                 `db:"updated_at" json:"updated_at"`
}

// WebhookDelivery maps webhook_delivery_queue rows.
type WebhookDelivery struct {
	DeliveryID         string                `db:"delivery_id" json:"delivery_id"`
	SubscriptionID     string                `db:"subscription_id" json:"subscription_id"`
	TenantID           string                `db:"tenant_id" json:"tenant_id"`
	EventType          string                `db:"event_type" json:"event_type"`
	Payload            json.RawMessage       `db:"payload" json:"payload"`
	Status             WebhookDeliveryStatus `db:"status" json:"status"`
	Attempts           int                   `db:"attempts" json:"attempts"`
	MaxAttempts        int                   `db:"max_attempts" json:"max_attempts"`
	ScheduledAt        time.Time             `db:"scheduled_at" json:"scheduled_at"`
	DeliveredAt        *time.Time            `db:"delivered_at" json:"delivered_at,omitempty"`
	LastAttemptAt      *time.Time            `db:"last_attempt_at" json:"last_attempt_at,omitempty"`
	ResponseStatusCode *int                  `db:"response_status_code" json:"response_status_code,omitempty"`
	ResponseBody       *string               `db:"response_body" json:"response_body,omitempty"`
	ErrorDetail        json.RawMessage       `db:"error_detail" json:"error_detail,omitempty"`
	CreatedAt          time.Time             `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time             `db:"updated_at" json:"updated_at"`
}

// ExternalTaskCompletion is the inbound completion contract for polyglot workers.
type ExternalTaskCompletion struct {
	TaskID          string           `json:"task_id"`
	AssignedService string           `json:"assigned_service"`
	Status          model.TaskStatus `json:"status"`
	OutputPayload   json.RawMessage  `json:"output_payload"`
	ErrorDetail     json.RawMessage  `json:"error_detail"`
	CompletedAt     time.Time        `json:"completed_at"`
	IdempotencyKey  string           `json:"idempotency_key"`
}

// ExternalEventInput is the inbound domain event ingestion contract.
type ExternalEventInput struct {
	TenantID       string          `json:"tenant_id"`
	CaseID         string          `json:"case_id"`
	EventType      string          `json:"event_type"`
	SourceSystem   string          `json:"source_system"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

// ExternalService maps external_services rows.
type ExternalService struct {
	ServiceID           string                  `db:"service_id" json:"service_id"`
	TenantID            string                  `db:"tenant_id" json:"tenant_id"`
	ServiceCode         string                  `db:"service_code" json:"service_code"`
	DisplayName         string                  `db:"display_name" json:"display_name"`
	Protocol            ExternalServiceProtocol `db:"protocol" json:"protocol"`
	HealthCheckURL      *string                 `db:"health_check_url" json:"health_check_url,omitempty"`
	Status              ExternalServiceStatus   `db:"status" json:"status"`
	ConsecutiveFailures int                     `db:"consecutive_failures" json:"consecutive_failures"`
	LastHealthCheckAt   *time.Time              `db:"last_health_check_at" json:"last_health_check_at,omitempty"`
	LastSuccessfulAt    *time.Time              `db:"last_successful_at" json:"last_successful_at,omitempty"`
	Metadata            json.RawMessage         `db:"metadata" json:"metadata"`
	CreatedAt           time.Time               `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time               `db:"updated_at" json:"updated_at"`
}

// EventTypeCatalogueEntry maps event_type_catalogue rows.
type EventTypeCatalogueEntry struct {
	EventTypeCode       string          `db:"event_type_code" json:"event_type_code"`
	Direction           EventDirection  `db:"direction" json:"direction"`
	Description         string          `db:"description" json:"description"`
	PayloadSchema       json.RawMessage `db:"payload_schema" json:"payload_schema"`
	IntroducedInVersion string          `db:"introduced_in_version" json:"introduced_in_version"`
	DeprecatedAt        *time.Time      `db:"deprecated_at" json:"deprecated_at,omitempty"`
	ExamplePayload      json.RawMessage `db:"example_payload" json:"example_payload,omitempty"`
	CreatedAt           time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at" json:"updated_at"`
}

// IntegrationAuditEntry maps integration_audit_log rows.
type IntegrationAuditEntry struct {
	AuditID         string                 `db:"audit_id" json:"audit_id"`
	TenantID        string                 `db:"tenant_id" json:"tenant_id"`
	Direction       IntegrationDirection   `db:"direction" json:"direction"`
	IntegrationType IntegrationType        `db:"integration_type" json:"integration_type"`
	SourceOrTarget  string                 `db:"source_or_target" json:"source_or_target"`
	EventType       *string                `db:"event_type" json:"event_type,omitempty"`
	CaseID          *string                `db:"case_id" json:"case_id,omitempty"`
	TaskID          *string                `db:"task_id" json:"task_id,omitempty"`
	Status          IntegrationAuditStatus `db:"status" json:"status"`
	RequestPayload  json.RawMessage        `db:"request_payload" json:"request_payload,omitempty"`
	ResponsePayload json.RawMessage        `db:"response_payload" json:"response_payload,omitempty"`
	DurationMS      int                    `db:"duration_ms" json:"duration_ms"`
	OccurredAt      time.Time              `db:"occurred_at" json:"occurred_at"`
	CreatedAt       time.Time              `db:"created_at" json:"created_at"`
}

// IntegrationAuditFilters constrain integration audit queries.
type IntegrationAuditFilters struct {
	CaseID          *string               `json:"case_id,omitempty"`
	TaskID          *string               `json:"task_id,omitempty"`
	Direction       *IntegrationDirection `json:"direction,omitempty"`
	IntegrationType *IntegrationType      `json:"integration_type,omitempty"`
	From            time.Time             `json:"from"`
	To              time.Time             `json:"to"`
}

var (
	ErrHandlerAlreadyRegistered = errors.New("handler already registered")
	ErrServiceMismatch          = errors.New("assigned service mismatch")
	ErrInvalidTaskTransition    = errors.New("invalid task transition")
	ErrUnknownEventType         = errors.New("unknown event type")
)
