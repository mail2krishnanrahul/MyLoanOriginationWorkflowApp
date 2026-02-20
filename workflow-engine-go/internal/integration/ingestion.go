package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// IngestExternalEvent validates and stores external domain events into outbox.
func IngestExternalEvent(
	ctx context.Context,
	db *sqlx.DB,
	input ExternalEventInput,
) error {
	if db == nil {
		return fmt.Errorf("IngestExternalEvent: db is nil")
	}
	ctxTenantID, err := multitenancy.TenantFromContext(ctx)
	if err != nil {
		return fmt.Errorf("IngestExternalEvent: %w", err)
	}
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.CaseID = strings.TrimSpace(input.CaseID)
	input.EventType = strings.TrimSpace(input.EventType)
	input.SourceSystem = strings.TrimSpace(input.SourceSystem)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.TenantID == "" || input.CaseID == "" || input.EventType == "" || input.SourceSystem == "" || input.IdempotencyKey == "" {
		return fmt.Errorf("IngestExternalEvent: tenant_id, case_id, event_type, source_system, idempotency_key are required")
	}
	if input.TenantID != ctxTenantID {
		IncExternalEventRejected(ctxTenantID, input.EventType, "TENANT_MISMATCH")
		return fmt.Errorf("IngestExternalEvent: tenant mismatch")
	}
	if len(input.Payload) == 0 {
		input.Payload = []byte(`{}`)
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	}

	if err := ValidateEventPayload(ctx, db, input.EventType, input.Payload); err != nil {
		IncExternalEventRejected(ctxTenantID, input.EventType, "SCHEMA_VALIDATION")
		return fmt.Errorf("IngestExternalEvent: validate payload: %w", err)
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("IngestExternalEvent: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var exists bool
	if err := tx.GetContext(ctx, &exists, `
		SELECT EXISTS (
			SELECT 1
			FROM cases
			WHERE id = $1::uuid
			  AND tenant_id = $2::uuid
		)
	`, input.CaseID, input.TenantID); err != nil {
		return fmt.Errorf("IngestExternalEvent: verify case: %w", err)
	}
	if !exists {
		IncExternalEventRejected(ctxTenantID, input.EventType, "CASE_NOT_FOUND")
		return fmt.Errorf("IngestExternalEvent: case %s not found for tenant", input.CaseID)
	}

	duplicate, err := CheckAndRecordIdempotencyKey(
		ctx,
		tx,
		IdempotencyKeyspaceExternalEventIngestion,
		input.IdempotencyKey,
		input.TenantID,
		time.Now().UTC().Add(7*24*time.Hour),
	)
	if err != nil {
		return fmt.Errorf("IngestExternalEvent: check idempotency: %w", err)
	}
	if duplicate {
		IncIdempotencyDuplicate(input.TenantID, IdempotencyKeyspaceExternalEventIngestion)
		if auditErr := RecordIntegrationAudit(ctx, tx, IntegrationAuditEntry{
			TenantID:        input.TenantID,
			Direction:       IntegrationDirectionInbound,
			IntegrationType: IntegrationTypeExternalEventIngestion,
			SourceOrTarget:  input.SourceSystem,
			EventType:       &input.EventType,
			CaseID:          &input.CaseID,
			Status:          IntegrationAuditStatusDuplicateRejected,
			RequestPayload:  input.Payload,
			ResponsePayload: nil,
			DurationMS:      0,
			OccurredAt:      time.Now().UTC(),
		}); auditErr != nil {
			slog.Error("integration audit write failed", "error", auditErr, "tenant_id", input.TenantID, "case_id", input.CaseID)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("IngestExternalEvent: commit duplicate flow: %w", err)
		}
		return nil
	}

	eventPayload := mustJSON(map[string]interface{}{
		"source":          "EXTERNAL",
		"source_system":   input.SourceSystem,
		"occurred_at":     input.OccurredAt.UTC().Format(time.RFC3339Nano),
		"idempotency_key": input.IdempotencyKey,
		"payload":         jsonRawOrString(input.Payload),
		"case_id":         input.CaseID,
		"tenant_id":       input.TenantID,
	})
	if err := publishOutboxEventTx(ctx, tx, model.Event{
		TenantID:      input.TenantID,
		CaseID:        &input.CaseID,
		EventType:     model.EventType(input.EventType),
		Payload:       eventPayload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
		MaxAttempts:   5,
	}); err != nil {
		return fmt.Errorf("IngestExternalEvent: insert outbox event: %w", err)
	}

	if auditErr := RecordIntegrationAudit(ctx, tx, IntegrationAuditEntry{
		TenantID:        input.TenantID,
		Direction:       IntegrationDirectionInbound,
		IntegrationType: IntegrationTypeExternalEventIngestion,
		SourceOrTarget:  input.SourceSystem,
		EventType:       &input.EventType,
		CaseID:          &input.CaseID,
		Status:          IntegrationAuditStatusSuccess,
		RequestPayload:  input.Payload,
		ResponsePayload: mustJSON(map[string]interface{}{"result": "ingested"}),
		DurationMS:      0,
		OccurredAt:      time.Now().UTC(),
	}); auditErr != nil {
		slog.Error("integration audit write failed", "error", auditErr, "tenant_id", input.TenantID, "case_id", input.CaseID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("IngestExternalEvent: commit: %w", err)
	}
	IncExternalEventIngested(input.TenantID, input.EventType, input.SourceSystem)
	return nil
}

func jsonRawOrString(payload []byte) interface{} {
	var v interface{}
	if err := json.Unmarshal(payload, &v); err == nil {
		return v
	}
	return string(payload)
}
