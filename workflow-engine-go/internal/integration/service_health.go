package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ServiceHealthChecker polls configured external service health endpoints.
type ServiceHealthChecker struct {
	db       *sqlx.DB
	client   *http.Client
	logger   *slog.Logger
	interval time.Duration
}

// NewServiceHealthChecker constructs an external-service health checker.
func NewServiceHealthChecker(db *sqlx.DB, client *http.Client, interval time.Duration, logger *slog.Logger) *ServiceHealthChecker {
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ServiceHealthChecker{db: db, client: client, logger: logger, interval: interval}
}

// Run executes health checks on configured interval until cancellation.
func (s *ServiceHealthChecker) Run(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("ServiceHealthChecker.Run: checker is nil")
	}
	if s.db == nil {
		return fmt.Errorf("ServiceHealthChecker.Run: db is nil")
	}
	if s.client == nil {
		s.client = &http.Client{Timeout: 10 * time.Second}
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if s.interval <= 0 {
		s.interval = 1 * time.Minute
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("service health checker stopped", "reason", ctx.Err())
			return nil
		case <-ticker.C:
			if err := s.checkAll(ctx); err != nil {
				s.logger.Error("service health check cycle failed", "error", err)
			}
		}
	}
}

func (s *ServiceHealthChecker) checkAll(ctx context.Context) error {
	rows := make([]ExternalService, 0)
	if err := s.db.SelectContext(ctx, &rows, `
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
		WHERE status IN ('ACTIVE', 'DEGRADED')
		  AND health_check_url IS NOT NULL
		ORDER BY updated_at ASC
	`); err != nil {
		return fmt.Errorf("checkAll: query services: %w", err)
	}

	for i := range rows {
		service := rows[i]
		serviceCtx := multitenancy.WithTenant(ctx, service.TenantID)
		if err := s.checkOne(serviceCtx, service); err != nil {
			s.logger.Error("service health check failed",
				"error", err,
				"tenant_id", service.TenantID,
				"service_code", service.ServiceCode,
				"service_id", service.ServiceID)
		}
	}
	return nil
}

func (s *ServiceHealthChecker) checkOne(ctx context.Context, service ExternalService) error {
	if service.HealthCheckURL == nil {
		return nil
	}

	startedAt := time.Now().UTC()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *service.HealthCheckURL, nil)
	if err != nil {
		return fmt.Errorf("checkOne: build request: %w", err)
	}
	resp, callErr := s.client.Do(req)
	duration := time.Since(startedAt)

	statusCode := 0
	bodySample := ""
	if resp != nil {
		statusCode = resp.StatusCode
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		bodySample = truncateString(string(body), 1024)
	}

	now := time.Now().UTC()
	success := callErr == nil && statusCode == http.StatusOK

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("checkOne: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var newStatus ExternalServiceStatus
	newConsecutiveFailures := service.ConsecutiveFailures
	if success {
		newStatus = ExternalServiceStatusActive
		newConsecutiveFailures = 0
		if _, err := tx.ExecContext(ctx, `
			UPDATE external_services
			SET status = 'ACTIVE',
				consecutive_failures = 0,
				last_health_check_at = $1,
				last_successful_at = $1,
				updated_at = $1
			WHERE service_id = $2::uuid
			  AND tenant_id = $3::uuid
		`, now, service.ServiceID, service.TenantID); err != nil {
			return fmt.Errorf("checkOne: update active status: %w", err)
		}
	} else {
		if newConsecutiveFailures+1 >= 3 {
			newStatus = ExternalServiceStatusOffline
		} else {
			newStatus = ExternalServiceStatusDegraded
		}
		if err := tx.QueryRowContext(ctx, `
			UPDATE external_services
			SET status = $1,
				consecutive_failures = consecutive_failures + 1,
				last_health_check_at = $2,
				updated_at = $2
			WHERE service_id = $3::uuid
			  AND tenant_id = $4::uuid
			RETURNING consecutive_failures
		`, string(newStatus), now, service.ServiceID, service.TenantID).Scan(&newConsecutiveFailures); err != nil {
			return fmt.Errorf("checkOne: update degraded/offline status: %w", err)
		}
		if newStatus == ExternalServiceStatusOffline && service.Status != ExternalServiceStatusOffline {
			if err := publishOutboxEventTx(ctx, tx, model.Event{
				TenantID:      service.TenantID,
				EventType:     model.EventServiceOffline,
				Payload:       mustJSON(map[string]interface{}{"service_id": service.ServiceID, "service_code": service.ServiceCode, "status": newStatus, "consecutive_failures": newConsecutiveFailures}),
				Status:        model.EventStatusPending,
				TargetService: "case-orchestrator",
				MaxAttempts:   5,
			}); err != nil {
				return fmt.Errorf("checkOne: publish service offline event: %w", err)
			}
		}
	}

	auditStatus := IntegrationAuditStatusFailure
	if success {
		auditStatus = IntegrationAuditStatusSuccess
	}
	responsePayload := mustJSON(map[string]interface{}{"status_code": statusCode, "body": bodySample})
	if callErr != nil {
		responsePayload = mustJSON(map[string]interface{}{"error": callErr.Error(), "status_code": statusCode, "body": bodySample})
	}
	if auditErr := RecordIntegrationAudit(ctx, tx, IntegrationAuditEntry{
		TenantID:        service.TenantID,
		Direction:       IntegrationDirectionOutbound,
		IntegrationType: IntegrationTypeHealthCheck,
		SourceOrTarget:  service.ServiceCode,
		EventType:       nil,
		CaseID:          nil,
		TaskID:          nil,
		Status:          auditStatus,
		RequestPayload:  mustJSON(map[string]interface{}{"health_check_url": service.HealthCheckURL}),
		ResponsePayload: responsePayload,
		DurationMS:      int(duration.Milliseconds()),
		OccurredAt:      now,
	}); auditErr != nil {
		s.logger.Error("integration audit write failed", "error", auditErr, "tenant_id", service.TenantID, "service_code", service.ServiceCode)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("checkOne: commit: %w", err)
	}

	switch newStatus {
	case ExternalServiceStatusActive:
		SetServiceHealthStatus(service.TenantID, service.ServiceCode, 1)
	case ExternalServiceStatusDegraded:
		SetServiceHealthStatus(service.TenantID, service.ServiceCode, 0.5)
	case ExternalServiceStatusOffline:
		SetServiceHealthStatus(service.TenantID, service.ServiceCode, 0)
	default:
		SetServiceHealthStatus(service.TenantID, service.ServiceCode, 0)
	}
	return nil
}
