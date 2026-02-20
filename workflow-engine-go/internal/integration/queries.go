package integration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// GetWebhookSubscription returns one tenant-scoped webhook subscription.
func GetWebhookSubscription(
	ctx context.Context,
	db *sqlx.DB,
	subscriptionID string,
	tenantID string,
) (WebhookSubscription, error) {
	if db == nil {
		return WebhookSubscription{}, fmt.Errorf("GetWebhookSubscription: db is nil")
	}
	tenantID, err := tenantFromCtxOrArg(ctx, tenantID)
	if err != nil {
		return WebhookSubscription{}, fmt.Errorf("GetWebhookSubscription: %w", err)
	}
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return WebhookSubscription{}, fmt.Errorf("GetWebhookSubscription: subscriptionID is required")
	}

	var row WebhookSubscription
	if err := db.GetContext(ctx, &row, `
		SELECT
			subscription_id::text AS subscription_id,
			tenant_id::text AS tenant_id,
			subscriber_code,
			target_url,
			event_types,
			signing_secret_enc,
			status,
			max_failures,
			failure_count,
			headers,
			timeout_seconds,
			created_at,
			updated_at
		FROM webhook_subscriptions
		WHERE subscription_id = $1::uuid
		  AND tenant_id = $2::uuid
	`, subscriptionID, tenantID); err != nil {
		return WebhookSubscription{}, fmt.Errorf("GetWebhookSubscription: query row: %w", err)
	}
	row.SigningSecretEnc = nil
	return row, nil
}

// ListWebhookSubscriptions returns paginated tenant-scoped webhook subscriptions.
func ListWebhookSubscriptions(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	status WebhookSubscriptionStatus,
	page, size int,
) ([]WebhookSubscription, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListWebhookSubscriptions: db is nil")
	}
	tenantID, err := tenantFromCtxOrArg(ctx, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("ListWebhookSubscriptions: %w", err)
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	statusArg := strings.TrimSpace(string(status))
	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)::int
		FROM webhook_subscriptions
		WHERE tenant_id = $1::uuid
		  AND ($2 = '' OR status = $2)
	`, tenantID, statusArg); err != nil {
		return nil, 0, fmt.Errorf("ListWebhookSubscriptions: count rows: %w", err)
	}

	rows := make([]WebhookSubscription, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			subscription_id::text AS subscription_id,
			tenant_id::text AS tenant_id,
			subscriber_code,
			target_url,
			event_types,
			signing_secret_enc,
			status,
			max_failures,
			failure_count,
			headers,
			timeout_seconds,
			created_at,
			updated_at
		FROM webhook_subscriptions
		WHERE tenant_id = $1::uuid
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, tenantID, statusArg, size, offset); err != nil {
		return nil, 0, fmt.Errorf("ListWebhookSubscriptions: query rows: %w", err)
	}
	for i := range rows {
		rows[i].SigningSecretEnc = nil
	}
	if rows == nil {
		rows = []WebhookSubscription{}
	}
	return rows, total, nil
}

// GetWebhookDeliveryHistory returns paginated delivery rows for a subscription.
func GetWebhookDeliveryHistory(
	ctx context.Context,
	db *sqlx.DB,
	subscriptionID string,
	tenantID string,
	page, size int,
) ([]WebhookDelivery, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetWebhookDeliveryHistory: db is nil")
	}
	tenantID, err := tenantFromCtxOrArg(ctx, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("GetWebhookDeliveryHistory: %w", err)
	}
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return nil, 0, fmt.Errorf("GetWebhookDeliveryHistory: subscriptionID is required")
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)::int
		FROM webhook_delivery_queue
		WHERE tenant_id = $1::uuid
		  AND subscription_id = $2::uuid
	`, tenantID, subscriptionID); err != nil {
		return nil, 0, fmt.Errorf("GetWebhookDeliveryHistory: count rows: %w", err)
	}

	rows := make([]WebhookDelivery, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			delivery_id::text AS delivery_id,
			subscription_id::text AS subscription_id,
			tenant_id::text AS tenant_id,
			event_type,
			payload,
			status,
			attempts,
			max_attempts,
			scheduled_at,
			delivered_at,
			last_attempt_at,
			response_status_code,
			response_body,
			error_detail,
			created_at,
			updated_at
		FROM webhook_delivery_queue
		WHERE tenant_id = $1::uuid
		  AND subscription_id = $2::uuid
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, tenantID, subscriptionID, size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetWebhookDeliveryHistory: query rows: %w", err)
	}
	if rows == nil {
		rows = []WebhookDelivery{}
	}
	return rows, total, nil
}

// ListExternalServices returns paginated external service registry rows.
func ListExternalServices(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	status ExternalServiceStatus,
	page, size int,
) ([]ExternalService, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListExternalServices: db is nil")
	}
	tenantID, err := tenantFromCtxOrArg(ctx, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("ListExternalServices: %w", err)
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size
	statusArg := strings.TrimSpace(string(status))

	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)::int
		FROM external_services
		WHERE tenant_id = $1::uuid
		  AND ($2 = '' OR status = $2)
	`, tenantID, statusArg); err != nil {
		return nil, 0, fmt.Errorf("ListExternalServices: count rows: %w", err)
	}

	rows := make([]ExternalService, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			service_id::text AS service_id,
			tenant_id::text AS tenant_id,
			service_code,
			display_name,
			protocol,
			health_check_url,
			status,
			consecutive_failures,
			last_health_check_at,
			last_successful_at,
			metadata,
			created_at,
			updated_at
		FROM external_services
		WHERE tenant_id = $1::uuid
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, tenantID, statusArg, size, offset); err != nil {
		return nil, 0, fmt.Errorf("ListExternalServices: query rows: %w", err)
	}
	if rows == nil {
		rows = []ExternalService{}
	}
	return rows, total, nil
}

// GetIntegrationAuditLog returns paginated integration audit entries with filters.
func GetIntegrationAuditLog(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filters IntegrationAuditFilters,
	page, size int,
) ([]IntegrationAuditEntry, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: db is nil")
	}
	tenantID, err := tenantFromCtxOrArg(ctx, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: %w", err)
	}
	if filters.From.IsZero() || filters.To.IsZero() {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: from and to are required")
	}
	if filters.To.Before(filters.From) {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: invalid time range")
	}
	if filters.To.Sub(filters.From) > 30*24*time.Hour {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: max range is 30 days")
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	caseID := ""
	taskID := ""
	direction := ""
	integrationType := ""
	if filters.CaseID != nil {
		caseID = strings.TrimSpace(*filters.CaseID)
	}
	if filters.TaskID != nil {
		taskID = strings.TrimSpace(*filters.TaskID)
	}
	if filters.Direction != nil {
		direction = strings.TrimSpace(string(*filters.Direction))
	}
	if filters.IntegrationType != nil {
		integrationType = strings.TrimSpace(string(*filters.IntegrationType))
	}

	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)::int
		FROM integration_audit_log
		WHERE tenant_id = $1::uuid
		  AND occurred_at >= $2
		  AND occurred_at <= $3
		  AND (NULLIF($4, '')::uuid IS NULL OR case_id = NULLIF($4, '')::uuid)
		  AND (NULLIF($5, '')::uuid IS NULL OR task_id = NULLIF($5, '')::uuid)
		  AND ($6 = '' OR direction = $6)
		  AND ($7 = '' OR integration_type = $7)
	`, tenantID, filters.From.UTC(), filters.To.UTC(), caseID, taskID, direction, integrationType); err != nil {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: count rows: %w", err)
	}

	rows := make([]IntegrationAuditEntry, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			audit_id::text AS audit_id,
			tenant_id::text AS tenant_id,
			direction,
			integration_type,
			source_or_target,
			event_type,
			case_id::text AS case_id,
			task_id::text AS task_id,
			status,
			request_payload,
			response_payload,
			duration_ms,
			occurred_at,
			created_at
		FROM integration_audit_log
		WHERE tenant_id = $1::uuid
		  AND occurred_at >= $2
		  AND occurred_at <= $3
		  AND (NULLIF($4, '')::uuid IS NULL OR case_id = NULLIF($4, '')::uuid)
		  AND (NULLIF($5, '')::uuid IS NULL OR task_id = NULLIF($5, '')::uuid)
		  AND ($6 = '' OR direction = $6)
		  AND ($7 = '' OR integration_type = $7)
		ORDER BY occurred_at DESC
		LIMIT $8 OFFSET $9
	`, tenantID, filters.From.UTC(), filters.To.UTC(), caseID, taskID, direction, integrationType, size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetIntegrationAuditLog: query rows: %w", err)
	}
	if rows == nil {
		rows = []IntegrationAuditEntry{}
	}
	return rows, total, nil
}
