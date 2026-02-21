package multitenancy

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// PublishEvent inserts an outbox event inside caller-managed transaction.
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

	prepared, err := PrepareEventForPublish(ctx, event)
	if err != nil {
		return fmt.Errorf("PublishEvent: enrich tenant payload: %w", err)
	}
	event = prepared
	if event.PartitionKey == nil {
		if event.TaskID != nil {
			event.PartitionKey = event.TaskID
		} else if event.CaseID != nil {
			event.PartitionKey = event.CaseID
		}
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
		)
		VALUES (
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
	`,
		event.TenantID,
		event.CaseID,
		event.TaskID,
		string(event.EventType),
		event.Payload,
		string(event.Status),
		event.TargetService,
		event.MaxAttempts,
		event.PartitionKey,
		event.TraceID,
	)
	if err != nil {
		return fmt.Errorf("PublishEvent: insert outbox row: %w", err)
	}
	return nil
}
