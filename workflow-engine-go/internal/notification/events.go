package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// EventPublisher abstracts transactional outbox publishing for notification flows.
type EventPublisher interface {
	PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error
}

// SQLXEventPublisher writes notification events into events_outbox.
type SQLXEventPublisher struct{}

func (p *SQLXEventPublisher) PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	return PublishEvent(ctx, tx, event)
}

// NotificationEventPayload is the shared envelope for internal notification events.
type NotificationEventPayload struct {
	NotificationID string                              `json:"notification_id,omitempty"`
	TriggerCode    string                              `json:"trigger_code,omitempty"`
	CaseID         *string                             `json:"case_id,omitempty"`
	TaskID         *string                             `json:"task_id,omitempty"`
	Channel        model.NotificationChannel           `json:"channel,omitempty"`
	Recipient      string                              `json:"recipient,omitempty"`
	Status         model.NotificationStatus            `json:"status,omitempty"`
	Reason         string                              `json:"reason,omitempty"`
	Suppression    *model.NotificationSuppressionReason `json:"suppression,omitempty"`
}

// PublishEvent inserts an outbox event in the caller transaction.
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
		event.TargetService = "notification-service"
	}
	if event.MaxAttempts == 0 {
		event.MaxAttempts = 5
	}
	if event.PartitionKey == nil && event.CaseID != nil {
		event.PartitionKey = event.CaseID
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO events_outbox (
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
			$3,
			$4::jsonb,
			$5,
			$6,
			$7,
			$8,
			$9
		)
	`, event.CaseID, event.TaskID, string(event.EventType), event.Payload, string(event.Status), event.TargetService, event.MaxAttempts, event.PartitionKey, event.TraceID)
	if err != nil {
		return fmt.Errorf("PublishEvent: %w", err)
	}
	return nil
}
