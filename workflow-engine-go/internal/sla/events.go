package sla

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/internal/integration"
	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

const (
	EventTypeSLAWarning  = model.EventSLAWarning
	EventTypeSLACritical = model.EventSLACritical
	EventTypeSLABreached = model.EventSLABreached
	EventTypeSLAPaused   = model.EventSLAPaused
	EventTypeSLAResumed  = model.EventSLAResumed
	EventTypeSLAReset    = model.EventSLAReset
	EventTypeSLAExtended = model.EventSLAExtended
)

// SLAEventPayload is the common event payload emitted by SLA operations.
type SLAEventPayload struct {
	EntityType       model.SLAEntityType      `json:"entity_type"`
	EntityID         string                   `json:"entity_id"`
	CaseID           *string                  `json:"case_id,omitempty"`
	TaskID           *string                  `json:"task_id,omitempty"`
	ThresholdPercent *float64                 `json:"threshold_percent,omitempty"`
	BreachSeverity   *model.SLABreachSeverity `json:"breach_severity,omitempty"`
	Action           *model.SLABreachAction   `json:"action,omitempty"`
	Reason           *string                  `json:"reason,omitempty"`
	ApprovedBy       *string                  `json:"approved_by,omitempty"`
}

// EventPublisher abstracts outbox writes for SLA workflows.
type EventPublisher interface {
	PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error
}

// SQLXEventPublisher writes SLA events into the existing events_outbox table.
type SQLXEventPublisher struct{}

// PublishEvent inserts an outbox event within an existing transaction.
func (p *SQLXEventPublisher) PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	return PublishEvent(ctx, tx, event)
}

// PublishEvent inserts an outbox event in the caller's transaction.
func PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	if tx == nil {
		return fmt.Errorf("PublishEvent: tx is nil")
	}
	if event.EventType == "" {
		return fmt.Errorf("PublishEvent: event type is required")
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage("{}")
	}
	if event.Status == "" {
		event.Status = model.EventStatusPending
	}
	if event.TargetService == "" {
		event.TargetService = "case-orchestrator"
	}
	if event.MaxAttempts == 0 {
		event.MaxAttempts = 5
	}
	prepared, err := multitenancy.PrepareEventForPublish(ctx, event)
	if err != nil {
		return fmt.Errorf("PublishEvent: enrich tenant payload: %w", err)
	}
	event = prepared
	if event.PartitionKey == nil && event.CaseID != nil {
		event.PartitionKey = event.CaseID
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO events_outbox (
			tenant_id,
			case_id,
			task_id,
			event_type,
			payload,
			status,
			target_service,
			max_attempts,
			partition_key,
			trace_id
		) VALUES (
			$1::uuid,
			$2::uuid,
			$3::uuid,
			$4,
			$5::jsonb,
			$6,
			$7,
			$8,
			$9,
			$10
		)
	`, event.TenantID, event.CaseID, event.TaskID, string(event.EventType), event.Payload, string(event.Status), event.TargetService, event.MaxAttempts, event.PartitionKey, event.TraceID)
	if err != nil {
		return fmt.Errorf("PublishEvent: %w", err)
	}
	if err := integration.EnqueueWebhookDeliveries(ctx, tx, event.TenantID, event); err != nil {
		return fmt.Errorf("PublishEvent: enqueue webhook deliveries: %w", err)
	}

	return nil
}
