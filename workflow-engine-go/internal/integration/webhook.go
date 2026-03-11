package integration

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
)

const (
	webhookRetryBaseInterval = 30 * time.Second
	webhookRetryMaxInterval  = 1 * time.Hour
	defaultWebhookPollBatch  = 200
)

// Documented in capability contract.
type WebhookDispatcher struct {
	db       *sqlx.DB
	client   *http.Client
	logger   *slog.Logger
	interval time.Duration
}

// NewWebhookDispatcher creates a queue dispatcher for webhook delivery rows.
func NewWebhookDispatcher(db *sqlx.DB, client *http.Client, interval time.Duration, logger *slog.Logger) *WebhookDispatcher {
	if interval <= 0 {
		interval = 1 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &WebhookDispatcher{db: db, client: client, logger: logger, interval: interval}
}

// SignWebhookPayload returns X-Webhook-Signature value: sha256=<hex>.
func SignWebhookPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// EnqueueWebhookDeliveries inserts queue rows for matching ACTIVE subscriptions.
func EnqueueWebhookDeliveries(
	ctx context.Context,
	tx *sqlx.Tx,
	tenantID string,
	event model.Event,
) error {
	if tx == nil {
		return fmt.Errorf("EnqueueWebhookDeliveries: tx is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(event.TenantID)
	}
	if tenantID == "" {
		if fromCtx, err := multitenancy.TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if tenantID == "" {
		return fmt.Errorf("EnqueueWebhookDeliveries: tenant_id is required")
	}
	if strings.TrimSpace(string(event.EventType)) == "" {
		return fmt.Errorf("EnqueueWebhookDeliveries: event type is required")
	}
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO webhook_delivery_queue (
			subscription_id,
			tenant_id,
			event_type,
			payload,
			status,
			attempts,
			max_attempts,
			scheduled_at
		)
		SELECT
			subscription_id,
			tenant_id,
			$2::text,
			$3::jsonb,
			'PENDING',
			0,
			max_failures,
			now()
		FROM webhook_subscriptions
		WHERE tenant_id = $1::uuid
		  AND status = 'ACTIVE'
		  AND (
			COALESCE(array_length(event_types, 1), 0) = 0
			OR $2::text = ANY(event_types)
		  )
	`, tenantID, string(event.EventType), payload)
	if err != nil {
		return fmt.Errorf("EnqueueWebhookDeliveries: insert queue rows: %w", err)
	}
	return nil
}

// pgxEnqueueExecutor is satisfied by pgx.Tx and pgxpool.Pool for transactional enqueue.
type pgxEnqueueExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// EnqueueWebhookDeliveriesPGX is the pgx variant used by repository.PublishEvent.
func EnqueueWebhookDeliveriesPGX(
	ctx context.Context,
	tx pgxEnqueueExecutor,
	tenantID string,
	event model.Event,
) error {
	if tx == nil {
		return fmt.Errorf("EnqueueWebhookDeliveriesPGX: tx is nil")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(event.TenantID)
	}
	if tenantID == "" {
		if fromCtx, err := multitenancy.TenantFromContext(ctx); err == nil {
			tenantID = fromCtx
		}
	}
	if tenantID == "" {
		return fmt.Errorf("EnqueueWebhookDeliveriesPGX: tenant_id is required")
	}
	if strings.TrimSpace(string(event.EventType)) == "" {
		return fmt.Errorf("EnqueueWebhookDeliveriesPGX: event type is required")
	}
	payload := event.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO webhook_delivery_queue (
			subscription_id,
			tenant_id,
			event_type,
			payload,
			status,
			attempts,
			max_attempts,
			scheduled_at
		)
		SELECT
			subscription_id,
			tenant_id,
			$2::text,
			$3::jsonb,
			'PENDING',
			0,
			max_failures,
			now()
		FROM webhook_subscriptions
		WHERE tenant_id = $1::uuid
		  AND status = 'ACTIVE'
		  AND (
			COALESCE(array_length(event_types, 1), 0) = 0
			OR $2::text = ANY(event_types)
		  )
	`, tenantID, string(event.EventType), payload)
	if err != nil {
		return fmt.Errorf("EnqueueWebhookDeliveriesPGX: insert queue rows: %w", err)
	}
	return nil
}

// DispatchWebhook delivers one claimed queue row and updates status atomically.
func DispatchWebhook(
	ctx context.Context,
	db *sqlx.DB,
	delivery WebhookDelivery,
	client *http.Client,
) error {
	if db == nil {
		return fmt.Errorf("DispatchWebhook: db is nil")
	}
	if strings.TrimSpace(delivery.DeliveryID) == "" {
		return fmt.Errorf("DispatchWebhook: delivery_id is required")
	}
	tenantID, err := tenantFromCtxOrArg(ctx, delivery.TenantID)
	if err != nil {
		return fmt.Errorf("DispatchWebhook: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	payload := delivery.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	var subscription WebhookSubscription
	if err := db.GetContext(ctx, &subscription, `
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
	`, delivery.SubscriptionID, tenantID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("DispatchWebhook: subscription not found")
		}
		return fmt.Errorf("DispatchWebhook: load subscription: %w", err)
	}

	secret, err := decryptWebhookSecret(subscription.SigningSecretEnc)
	if err != nil {
		return fmt.Errorf("DispatchWebhook: decrypt signing secret: %w", err)
	}
	signature := SignWebhookPayload(secret, payload)

	timeout := time.Duration(subscription.TimeoutSeconds) * time.Second
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, subscription.TargetURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("DispatchWebhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signature)
	for k, v := range parseWebhookHeaders(subscription.Headers) {
		req.Header.Set(k, v)
	}

	startedAt := time.Now().UTC()
	response, callErr := client.Do(req)
	duration := time.Since(startedAt)

	statusCode := 0
	responseBody := ""
	if response != nil {
		statusCode = response.StatusCode
		bodyBytes, readErr := io.ReadAll(io.LimitReader(response.Body, 1024))
		_ = response.Body.Close()
		if readErr == nil {
			responseBody = string(bodyBytes)
		}
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("DispatchWebhook: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC()
	auditStatus := IntegrationAuditStatusFailure
	if callErr == nil && statusCode >= 200 && statusCode < 300 {
		auditStatus = IntegrationAuditStatusSuccess
		if _, err := tx.ExecContext(ctx, `
			UPDATE webhook_delivery_queue
			SET status = 'DELIVERED',
				delivered_at = $1,
				last_attempt_at = $1,
				response_status_code = $2,
				response_body = NULLIF($3, ''),
				error_detail = NULL,
				updated_at = $1
			WHERE delivery_id = $4::uuid
			  AND tenant_id = $5::uuid
		`, now, statusCode, truncateString(responseBody, 1024), delivery.DeliveryID, tenantID); err != nil {
			return fmt.Errorf("DispatchWebhook: mark delivered: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE webhook_subscriptions
			SET failure_count = 0,
				updated_at = $1,
				status = CASE WHEN status = 'FAILED' THEN 'ACTIVE' ELSE status END
			WHERE subscription_id = $2::uuid
		`, now, subscription.SubscriptionID); err != nil {
			return fmt.Errorf("DispatchWebhook: reset subscription failure count: %w", err)
		}
		IncWebhookDelivery(tenantID, delivery.EventType, string(WebhookDeliveryStatusDelivered))
		ObserveWebhookDeliveryLatency(tenantID, delivery.EventType, duration.Seconds())
	} else {
		errorMessage := ""
		if callErr != nil {
			errorMessage = callErr.Error()
		}
		if errorMessage == "" && statusCode > 0 {
			errorMessage = fmt.Sprintf("non-2xx response %d", statusCode)
		}
		backoff := computeWebhookBackoff(delivery.Attempts)
		nextAttempt := now.Add(backoff)
		status := WebhookDeliveryStatusFailed
		if delivery.Attempts >= delivery.MaxAttempts {
			status = WebhookDeliveryStatusAbandoned
		}
		errorPayload, _ := json.Marshal(map[string]interface{}{
			"error":          errorMessage,
			"response_code":  statusCode,
			"attempt":        delivery.Attempts,
			"max_attempts":   delivery.MaxAttempts,
			"response_sample": truncateString(responseBody, 1024),
		})
		if _, err := tx.ExecContext(ctx, `
			UPDATE webhook_delivery_queue
			SET status = $1,
				scheduled_at = CASE WHEN $1 = 'ABANDONED' THEN scheduled_at ELSE $2 END,
				last_attempt_at = $3,
				response_status_code = NULLIF($4, 0),
				response_body = NULLIF($5, ''),
				error_detail = $6::jsonb,
				updated_at = $3
			WHERE delivery_id = $7::uuid
			  AND tenant_id = $8::uuid
		`, string(status), nextAttempt, now, statusCode, truncateString(responseBody, 1024), errorPayload, delivery.DeliveryID, tenantID); err != nil {
			return fmt.Errorf("DispatchWebhook: update failed delivery: %w", err)
		}

		var failureCount int
		var maxFailures int
		var currentStatus string
		if err := tx.QueryRowContext(ctx, `
			UPDATE webhook_subscriptions
			SET failure_count = failure_count + 1,
				updated_at = $1
			WHERE subscription_id = $2::uuid
			RETURNING failure_count, max_failures, status
		`, now, subscription.SubscriptionID).Scan(&failureCount, &maxFailures, &currentStatus); err != nil {
			return fmt.Errorf("DispatchWebhook: increment subscription failure count: %w", err)
		}

		if failureCount >= maxFailures && currentStatus == string(WebhookSubscriptionStatusActive) {
			if _, err := tx.ExecContext(ctx, `
				UPDATE webhook_subscriptions
				SET status = 'FAILED',
					updated_at = $1
				WHERE subscription_id = $2::uuid
			`, now, subscription.SubscriptionID); err != nil {
				return fmt.Errorf("DispatchWebhook: fail subscription: %w", err)
			}
			if err := publishOutboxEventTx(ctx, tx, model.Event{
				TenantID:      tenantID,
				EventType:     model.EventWebhookSubscriptionFailed,
				Payload:       mustJSON(map[string]interface{}{"subscription_id": subscription.SubscriptionID, "subscriber_code": subscription.SubscriberCode, "failure_count": failureCount, "max_failures": maxFailures}),
				Status:        model.EventStatusPending,
				TargetService: "case-orchestrator",
				MaxAttempts:   5,
			}); err != nil {
				return fmt.Errorf("DispatchWebhook: publish subscription failed event: %w", err)
			}
			IncWebhookSubscriptionFailure(tenantID, subscription.SubscriberCode)
		}

		IncWebhookDelivery(tenantID, delivery.EventType, string(status))
		ObserveWebhookDeliveryLatency(tenantID, delivery.EventType, duration.Seconds())
	}

	responsePayload := mustJSON(map[string]interface{}{
		"status_code": statusCode,
		"body":        truncateString(responseBody, 1024),
	})
	if callErr != nil {
		responsePayload = mustJSON(map[string]interface{}{
			"error": callErr.Error(),
			"body":  truncateString(responseBody, 1024),
		})
	}
	auditEntry := IntegrationAuditEntry{
		TenantID:        tenantID,
		Direction:       IntegrationDirectionOutbound,
		IntegrationType: IntegrationTypeWebhook,
		SourceOrTarget:  subscription.TargetURL,
		EventType:       stringPtr(delivery.EventType),
		CaseID:          nil,
		TaskID:          nil,
		Status:          auditStatus,
		RequestPayload:  payload,
		ResponsePayload: responsePayload,
		DurationMS:      int(duration.Milliseconds()),
		OccurredAt:      now,
	}
	if auditErr := RecordIntegrationAudit(ctx, tx, auditEntry); auditErr != nil {
		slog.Error("integration audit write failed", "error", auditErr, "tenant_id", tenantID, "delivery_id", delivery.DeliveryID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DispatchWebhook: commit: %w", err)
	}
	return nil
}

// Run starts dispatcher polling loop until cancellation.
func (d *WebhookDispatcher) Run(ctx context.Context) error {
	if d == nil {
		return fmt.Errorf("WebhookDispatcher.Run: dispatcher is nil")
	}
	if d.db == nil {
		return fmt.Errorf("WebhookDispatcher.Run: db is nil")
	}
	if d.client == nil {
		d.client = &http.Client{Timeout: 10 * time.Second}
	}
	if d.logger == nil {
		d.logger = slog.Default()
	}
	if d.interval <= 0 {
		d.interval = 1 * time.Second
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("webhook dispatcher stopped", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			deliveries, err := d.claimDeliveries(ctx, defaultWebhookPollBatch)
			if err != nil {
				d.logger.Error("failed to claim webhook deliveries", "error", err)
				continue
			}
			for i := range deliveries {
				delivery := deliveries[i]
				deliveryCtx := multitenancy.WithTenant(ctx, delivery.TenantID)
				if err := DispatchWebhook(deliveryCtx, d.db, delivery, d.client); err != nil {
					d.logger.Error("webhook dispatch failed",
						"error", err,
						"tenant_id", delivery.TenantID,
						"delivery_id", delivery.DeliveryID,
						"event_type", delivery.EventType)
				}
			}
		}
	}
}

func (d *WebhookDispatcher) claimDeliveries(ctx context.Context, batchSize int) ([]WebhookDelivery, error) {
	if batchSize <= 0 {
		batchSize = defaultWebhookPollBatch
	}
	rows := make([]WebhookDelivery, 0, batchSize)
	if err := d.db.SelectContext(ctx, &rows, `
		WITH candidates AS (
			SELECT delivery_id
			FROM webhook_delivery_queue
			WHERE status IN ('PENDING', 'FAILED')
			  AND scheduled_at <= now()
			ORDER BY scheduled_at ASC, created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE webhook_delivery_queue wdq
		SET attempts = attempts + 1,
			last_attempt_at = now(),
			scheduled_at = now() + interval '1 hour',
			updated_at = now()
		FROM candidates c
		WHERE wdq.delivery_id = c.delivery_id
		RETURNING
			wdq.delivery_id::text AS delivery_id,
			wdq.subscription_id::text AS subscription_id,
			wdq.tenant_id::text AS tenant_id,
			wdq.event_type,
			wdq.payload,
			wdq.status,
			wdq.attempts,
			wdq.max_attempts,
			wdq.scheduled_at,
			wdq.delivered_at,
			wdq.last_attempt_at,
			wdq.response_status_code,
			wdq.response_body,
			wdq.error_detail,
			wdq.created_at,
			wdq.updated_at
	`, batchSize); err != nil {
		return nil, fmt.Errorf("claimDeliveries: claim rows: %w", err)
	}
	if rows == nil {
		return []WebhookDelivery{}, nil
	}
	return rows, nil
}

func computeWebhookBackoff(attempts int) time.Duration {
	if attempts <= 1 {
		return webhookRetryBaseInterval
	}
	raw := float64(webhookRetryBaseInterval) * math.Pow(2, float64(attempts-1))
	backoff := time.Duration(raw)
	if backoff > webhookRetryMaxInterval {
		return webhookRetryMaxInterval
	}
	return backoff
}

func parseWebhookHeaders(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(parsed))
	for k, v := range parsed {
		name := strings.TrimSpace(k)
		if name == "" {
			continue
		}
		result[name] = strings.TrimSpace(fmt.Sprint(v))
	}
	return result
}

func decryptWebhookSecret(secretEnc []byte) (string, error) {
	if len(secretEnc) == 0 {
		return "", fmt.Errorf("decryptWebhookSecret: empty secret")
	}
	keyRaw := strings.TrimSpace(os.Getenv("WEBHOOK_SECRET_KEY"))
	if keyRaw == "" {
		return string(secretEnc), nil
	}
	key, err := decodeSecretKey(keyRaw)
	if err != nil {
		return "", fmt.Errorf("decryptWebhookSecret: parse key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("decryptWebhookSecret: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("decryptWebhookSecret: init gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(secretEnc) <= nonceSize {
		return "", fmt.Errorf("decryptWebhookSecret: ciphertext too short")
	}
	nonce := secretEnc[:nonceSize]
	ciphertext := secretEnc[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryptWebhookSecret: gcm open: %w", err)
	}
	return string(plaintext), nil
}

func decodeSecretKey(value string) ([]byte, error) {
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(value) == 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("decodeSecretKey: expected 32-byte key")
}

func truncateString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{"error":"marshal_failed"}`)
	}
	return b
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func publishOutboxEventTx(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	if tx == nil {
		return fmt.Errorf("publishOutboxEventTx: tx is nil")
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage(`{}`)
	}
	if event.Status == "" {
		event.Status = model.EventStatusPending
	}
	if event.TargetService == "" {
		event.TargetService = "case-orchestrator"
	}
	if event.MaxAttempts <= 0 {
		event.MaxAttempts = 5
	}
	prepared, err := multitenancy.PrepareEventForPublish(ctx, event)
	if err != nil {
		return fmt.Errorf("publishOutboxEventTx: prepare event: %w", err)
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
			NULLIF($2, '')::uuid,
			NULLIF($3, '')::uuid,
			$4,
			$5::jsonb,
			$6,
			$7,
			$8,
			$9,
			$10
		)
	`, event.TenantID, nullableUUIDText(event.CaseID), nullableUUIDText(event.TaskID), string(event.EventType), event.Payload, string(event.Status), event.TargetService, event.MaxAttempts, event.PartitionKey, event.TraceID)
	if err != nil {
		return fmt.Errorf("publishOutboxEventTx: insert event: %w", err)
	}
	if err := EnqueueWebhookDeliveries(ctx, tx, event.TenantID, event); err != nil {
		return fmt.Errorf("publishOutboxEventTx: enqueue webhook deliveries: %w", err)
	}
	return nil
}

// Ensure pgx package is imported for interface compatibility checks.
var _ pgxEnqueueExecutor = (pgx.Tx)(nil)
