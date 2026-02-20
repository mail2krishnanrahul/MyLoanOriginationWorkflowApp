package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	maxAuditRequestBytes  = 4096
	maxAuditResponseBytes = 1024
)

// RecordIntegrationAudit writes one append-only integration audit row.
func RecordIntegrationAudit(
	ctx context.Context,
	tx *sqlx.Tx,
	entry IntegrationAuditEntry,
) error {
	if tx == nil {
		return fmt.Errorf("RecordIntegrationAudit: tx is nil")
	}
	entry.TenantID = strings.TrimSpace(entry.TenantID)
	entry.SourceOrTarget = strings.TrimSpace(entry.SourceOrTarget)
	if entry.TenantID == "" {
		return fmt.Errorf("RecordIntegrationAudit: tenant_id is required")
	}
	if entry.SourceOrTarget == "" {
		return fmt.Errorf("RecordIntegrationAudit: source_or_target is required")
	}
	if entry.Direction == "" {
		return fmt.Errorf("RecordIntegrationAudit: direction is required")
	}
	if entry.IntegrationType == "" {
		return fmt.Errorf("RecordIntegrationAudit: integration_type is required")
	}
	if entry.Status == "" {
		return fmt.Errorf("RecordIntegrationAudit: status is required")
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}

	requestPayload := truncateJSONPayload(entry.RequestPayload, maxAuditRequestBytes)
	responsePayload := truncateJSONPayload(entry.ResponsePayload, maxAuditResponseBytes)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO integration_audit_log (
			tenant_id,
			direction,
			integration_type,
			source_or_target,
			event_type,
			case_id,
			task_id,
			status,
			request_payload,
			response_payload,
			duration_ms,
			occurred_at
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4,
			$5,
			NULLIF($6, '')::uuid,
			NULLIF($7, '')::uuid,
			$8,
			$9::jsonb,
			$10::jsonb,
			$11,
			$12
		)
	`, entry.TenantID, string(entry.Direction), string(entry.IntegrationType), entry.SourceOrTarget, entry.EventType, nullableUUIDText(entry.CaseID), nullableUUIDText(entry.TaskID), string(entry.Status), requestPayload, responsePayload, entry.DurationMS, entry.OccurredAt.UTC())
	if err != nil {
		return fmt.Errorf("RecordIntegrationAudit: insert: %w", err)
	}
	return nil
}

func truncateJSONPayload(payload json.RawMessage, maxBytes int) json.RawMessage {
	if maxBytes <= 0 || len(payload) == 0 {
		return payload
	}
	if len(payload) <= maxBytes {
		if json.Valid(payload) {
			return payload
		}
		wrapped, _ := json.Marshal(map[string]string{"raw": string(payload)})
		return wrapped
	}
	clipped := string(payload[:maxBytes])
	wrapped, _ := json.Marshal(map[string]string{"truncated": clipped})
	return wrapped
}

func nullableUUIDText(value *string) string {
	if value == nil {
		return ""
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return ""
	}
	return trimmed
}
