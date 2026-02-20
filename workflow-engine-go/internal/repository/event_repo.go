package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"workflow-engine/internal/integration"
	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// PublishEvent — inserts an event within a caller-provided transaction
// (transactional outbox guarantee: event is committed with the business op).
// ---------------------------------------------------------------------------

func (r *Repository) PublishEvent(ctx context.Context, tx DBExecutor, event model.Event) error {
	if tx == nil {
		return fmt.Errorf("PublishEvent requires a non-nil transaction")
	}

	// Default payload
	if event.Payload == nil {
		event.Payload = json.RawMessage("{}")
	}
	if event.Status == "" {
		event.Status = model.EventStatusPending
	}
	if event.MaxAttempts == 0 {
		event.MaxAttempts = 5
	}
	prepared, err := multitenancy.PrepareEventForPublish(ctx, event)
	if err != nil {
		return fmt.Errorf("failed to enrich event tenant payload: %w", err)
	}
	event = prepared

	// Auto-set partition_key to case_id if not explicitly provided
	if event.PartitionKey == nil && event.CaseID != nil {
		event.PartitionKey = event.CaseID
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO events_outbox (
			tenant_id, case_id, task_id, event_type, payload,
			status, target_service,
			max_attempts, partition_key, trace_id
		) VALUES (
			$1::uuid, $2, $3, $4, $5,
			$6, $7,
			$8, $9,
			$10
		)`,
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
		return fmt.Errorf("failed to publish event: %w", err)
	}
	if err := integration.EnqueueWebhookDeliveriesPGX(ctx, tx, event.TenantID, event); err != nil {
		return fmt.Errorf("failed to enqueue webhook deliveries: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// PollEvents — fetches a batch of PENDING events for a target service
// using SELECT FOR UPDATE SKIP LOCKED for concurrent-safe polling.
// ---------------------------------------------------------------------------

func (r *Repository) PollEvents(ctx context.Context, service string, batchSize int) ([]model.Event, error) {
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	if batchSize <= 0 {
		batchSize = 10
	}

	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("PollEvents: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	baseQuery := `
		SELECT id::text AS id, case_id::text AS case_id, task_id::text AS task_id, event_type, payload,
		       tenant_id::text AS tenant_id,
		       status, target_service,
		       attempts, max_attempts, last_attempted_at,
		       partition_key, trace_id,
		       created_at, delivered_at
		FROM events_outbox
		WHERE target_service = $1
		  AND status = 'PENDING'
		  AND attempts < max_attempts
		ORDER BY created_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED`
	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, baseQuery, []interface{}{service, batchSize})
	if scopeErr != nil {
		return nil, fmt.Errorf("PollEvents: %w", scopeErr)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("PollEvents: query: %w", err)
	}
	defer rows.Close()

	var events []model.Event
	var ids []string
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(
			&e.ID, &e.CaseID, &e.TaskID, &e.EventType, &e.Payload,
			&e.TenantID,
			&e.Status, &e.TargetService,
			&e.Attempts, &e.MaxAttempts, &e.LastAttemptedAt,
			&e.PartitionKey, &e.TraceID,
			&e.CreatedAt, &e.DeliveredAt,
		); err != nil {
			return nil, fmt.Errorf("PollEvents: scan: %w", err)
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("PollEvents: rows iteration: %w", err)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// Atomically claim events within the same transaction
	claimQuery, claimArgs, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE events_outbox
		SET status = 'PROCESSING',
		    last_attempted_at = now(),
		    attempts = attempts + 1
		WHERE id = ANY($1::uuid[])`, []interface{}{ids})
	if scopeErr != nil {
		return nil, fmt.Errorf("PollEvents: %w", scopeErr)
	}
	_, err = tx.Exec(ctx, claimQuery, claimArgs...)
	if err != nil {
		return nil, fmt.Errorf("PollEvents: claim events: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("PollEvents: commit: %w", err)
	}

	return events, nil
}

// ---------------------------------------------------------------------------
// AckEvent — marks an event as DELIVERED after successful processing.
// ---------------------------------------------------------------------------

func (r *Repository) AckEvent(ctx context.Context, eventID string) error {
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	now := time.Now().UTC()
	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE events_outbox
		SET status       = 'DELIVERED',
		    delivered_at  = $1,
		    attempts      = attempts + 1,
		    last_attempted_at = $1
		WHERE id = $2::uuid
		  AND status IN ('PENDING', 'PROCESSING')`, []interface{}{now, eventID})
	if scopeErr != nil {
		return fmt.Errorf("AckEvent: %w", scopeErr)
	}
	tag, err := r.Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to ack event %s: %w", eventID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s not found or already delivered", eventID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// NackEvent — records a delivery failure: increments attempts, logs error.
// If max_attempts is reached, status is moved to FAILED.
// ---------------------------------------------------------------------------

func (r *Repository) NackEvent(ctx context.Context, eventID string, deliveryErr error) error {
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	now := time.Now().UTC()
	errMsg := ""
	if deliveryErr != nil {
		errMsg = deliveryErr.Error()
	}

	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE events_outbox
		SET attempts          = attempts + 1,
		    last_attempted_at = $1,
		    status            = CASE
		                            WHEN attempts + 1 >= max_attempts THEN 'FAILED'
		                            ELSE 'PENDING'
		                        END,
		    payload           = jsonb_set(
		                            COALESCE(payload, '{}'),
		                            '{_last_error}',
		                            to_jsonb($2::text)
		                        )
		WHERE id = $3::uuid
		  AND status IN ('PENDING', 'PROCESSING')`, []interface{}{now, errMsg, eventID})
	if scopeErr != nil {
		return fmt.Errorf("NackEvent: %w", scopeErr)
	}
	tag, err := r.Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to nack event %s: %w", eventID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s not found or not in PENDING state", eventID)
	}
	return nil
}
