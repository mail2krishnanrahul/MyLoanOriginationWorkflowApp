package integration

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// CompleteTaskFromExternal applies idempotent task completion callbacks from polyglot workers.
func CompleteTaskFromExternal(
	ctx context.Context,
	db *sqlx.DB,
	idempotencyKey string,
	output ExternalTaskCompletion,
) error {
	if db == nil {
		return fmt.Errorf("CompleteTaskFromExternal: db is nil")
	}
	tenantID, err := multitenancy.TenantFromContext(ctx)
	if err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: %w", err)
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(output.IdempotencyKey)
	}
	if idempotencyKey == "" {
		return fmt.Errorf("CompleteTaskFromExternal: idempotency key is required")
	}
	output.TaskID = strings.TrimSpace(output.TaskID)
	output.AssignedService = strings.TrimSpace(output.AssignedService)
	if output.TaskID == "" {
		return fmt.Errorf("CompleteTaskFromExternal: task_id is required")
	}
	if output.AssignedService == "" {
		return fmt.Errorf("CompleteTaskFromExternal: assigned_service is required")
	}
	if output.Status != model.TaskStatusDone && output.Status != model.TaskStatusFailed {
		return fmt.Errorf("CompleteTaskFromExternal: unsupported status %s", output.Status)
	}
	if output.CompletedAt.IsZero() {
		output.CompletedAt = time.Now().UTC()
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	duplicate, err := CheckAndRecordIdempotencyKey(
		ctx,
		tx,
		IdempotencyKeyspaceTaskCompletion,
		idempotencyKey,
		tenantID,
		time.Now().UTC().Add(30*24*time.Hour),
	)
	if err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: check idempotency: %w", err)
	}
	if duplicate {
		IncIdempotencyDuplicate(tenantID, IdempotencyKeyspaceTaskCompletion)
		IncExternalTaskCompletion(tenantID, output.AssignedService, string(IntegrationAuditStatusDuplicateRejected))
		if auditErr := RecordIntegrationAudit(ctx, tx, IntegrationAuditEntry{
			TenantID:        tenantID,
			Direction:       IntegrationDirectionInbound,
			IntegrationType: IntegrationTypeExternalTaskCompletion,
			SourceOrTarget:  output.AssignedService,
			EventType:       stringPtr(string(model.EventTaskCompleted)),
			TaskID:          stringPtr(output.TaskID),
			Status:          IntegrationAuditStatusDuplicateRejected,
			RequestPayload:  mustJSON(output),
			ResponsePayload: nil,
			DurationMS:      0,
			OccurredAt:      time.Now().UTC(),
		}); auditErr != nil {
			slog.Error("integration audit write failed", "error", auditErr, "tenant_id", tenantID, "task_id", output.TaskID)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("CompleteTaskFromExternal: commit duplicate flow: %w", err)
		}
		return nil
	}

	var task struct {
		TaskID          string `db:"id"`
		CaseID          string `db:"case_id"`
		AssignedService string `db:"assigned_service"`
		Status          string `db:"status"`
	}
	if err := tx.GetContext(ctx, &task, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			COALESCE(assigned_service, '') AS assigned_service,
			status
		FROM tasks
		WHERE id = $1::uuid
		  AND tenant_id = $2::uuid
		FOR UPDATE
	`, output.TaskID, tenantID); err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: load task: %w", err)
	}

	if task.AssignedService != output.AssignedService {
		IncExternalTaskCompletion(tenantID, output.AssignedService, string(IntegrationAuditStatusFailure))
		return fmt.Errorf("CompleteTaskFromExternal: %w", ErrServiceMismatch)
	}
	if model.TaskStatus(task.Status) != model.TaskStatusInProgress {
		IncExternalTaskCompletion(tenantID, output.AssignedService, string(IntegrationAuditStatusFailure))
		return fmt.Errorf("CompleteTaskFromExternal: %w", ErrInvalidTaskTransition)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = $1,
			output_payload = COALESCE($2::jsonb, output_payload),
			error_detail = COALESCE($3::jsonb, error_detail),
			completed_at = $4,
			updated_at = $4,
			version = version + 1
		WHERE id = $5::uuid
		  AND tenant_id = $6::uuid
	`, string(output.Status), output.OutputPayload, output.ErrorDetail, output.CompletedAt.UTC(), output.TaskID, tenantID); err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: update task completion: %w", err)
	}

	eventType := model.EventTaskCompleted
	if output.Status == model.TaskStatusFailed {
		eventType = model.EventTaskFailed
	}
	payload := map[string]interface{}{
		"case_id":              task.CaseID,
		"task_id":              task.TaskID,
		"task_definition_code": nil,
		"status":               string(output.Status),
		"source":               "EXTERNAL",
		"assigned_service":     output.AssignedService,
	}
	if output.Status == model.TaskStatusFailed {
		payload["error_detail"] = output.ErrorDetail
	}
	if err := publishOutboxEventTx(ctx, tx, model.Event{
		TenantID:      tenantID,
		CaseID:        &task.CaseID,
		TaskID:        &task.TaskID,
		EventType:     eventType,
		Payload:       mustJSON(payload),
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
		MaxAttempts:   5,
	}); err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: publish completion event: %w", err)
	}

	auditStatus := IntegrationAuditStatusSuccess
	if output.Status == model.TaskStatusFailed {
		auditStatus = IntegrationAuditStatusFailure
	}
	if auditErr := RecordIntegrationAudit(ctx, tx, IntegrationAuditEntry{
		TenantID:        tenantID,
		Direction:       IntegrationDirectionInbound,
		IntegrationType: IntegrationTypeExternalTaskCompletion,
		SourceOrTarget:  output.AssignedService,
		EventType:       stringPtr(string(eventType)),
		CaseID:          &task.CaseID,
		TaskID:          &task.TaskID,
		Status:          auditStatus,
		RequestPayload:  mustJSON(output),
		ResponsePayload: mustJSON(map[string]interface{}{"task_status": output.Status}),
		DurationMS:      0,
		OccurredAt:      time.Now().UTC(),
	}); auditErr != nil {
		slog.Error("integration audit write failed", "error", auditErr, "tenant_id", tenantID, "task_id", task.TaskID)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("CompleteTaskFromExternal: commit: %w", err)
	}
	IncExternalTaskCompletion(tenantID, output.AssignedService, string(output.Status))
	return nil
}
