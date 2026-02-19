package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

func appendDeliveryEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	notificationID string,
	eventType model.NotificationDeliveryEventType,
	channelResponse json.RawMessage,
	userAgent *string,
) error {
	if tx == nil {
		return fmt.Errorf("appendDeliveryEvent: tx is nil")
	}
	if notificationID == "" {
		return fmt.Errorf("appendDeliveryEvent: notificationID is required")
	}
	if channelResponse == nil {
		channelResponse = json.RawMessage("{}")
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO notification_delivery_events (
			notification_id,
			event_type,
			event_timestamp,
			channel_response,
			user_agent,
			created_at
		)
		VALUES (
			$1::uuid,
			$2,
			now(),
			$3::jsonb,
			$4,
			now()
		)
	`, notificationID, string(eventType), channelResponse, userAgent)
	if err != nil {
		return fmt.Errorf("appendDeliveryEvent: %w", err)
	}
	return nil
}

func publishNotificationInternalEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	publisher EventPublisher,
	eventType model.EventType,
	payload NotificationEventPayload,
) error {
	if tx == nil {
		return fmt.Errorf("publishNotificationInternalEvent: tx is nil")
	}
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publishNotificationInternalEvent: marshal payload: %w", err)
	}
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:        payload.CaseID,
		TaskID:        payload.TaskID,
		EventType:     eventType,
		Payload:       raw,
		Status:        model.EventStatusPending,
		TargetService: "notification-service",
	}); err != nil {
		return fmt.Errorf("publishNotificationInternalEvent: %w", err)
	}
	return nil
}

func buildErrorDetail(reason string, err error) json.RawMessage {
	type errorDetail struct {
		Reason string `json:"reason"`
		Error  string `json:"error"`
	}
	detail := errorDetail{Reason: reason}
	if err != nil {
		detail.Error = err.Error()
	}
	raw, marshalErr := json.Marshal(detail)
	if marshalErr != nil {
		return json.RawMessage(`{"reason":"UNKNOWN","error":"marshal error"}`)
	}
	return raw
}
