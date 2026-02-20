package repository

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// ClaimTasks — atomically claims a batch of PENDING tasks for a service.
// Uses SELECT FOR UPDATE SKIP LOCKED for concurrent-safe claiming.
// Ordering: priority DESC (CRITICAL first), then due_at ASC (urgent first).
// Only claims tasks where due_at <= NOW() or due_at IS NULL.
// ---------------------------------------------------------------------------

func (r *Repository) ClaimTasks(ctx context.Context, service string, batchSize int) ([]model.Task, error) {
	if batchSize <= 0 {
		batchSize = 10
	}
	tenantID, err := multitenancy.TenantFromContext(ctx)
	if err != nil {
		tenantID = multitenancy.DefaultTenantID
		ctx = multitenancy.WithTenant(ctx, tenantID)
	}
	if r.SQLX != nil {
		if err := multitenancy.EnforceTenantTaskLimits(ctx, r.SQLX, tenantID); err != nil {
			return nil, fmt.Errorf("ClaimTasks: enforce tenant task limits: %w", err)
		}
	}

	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin claim transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Select and lock claimable tasks
	baseQuery := `
		SELECT id, case_id, task_definition_code, activity_code, stage_code,
		       tenant_id::text AS tenant_id,
		       status, priority, assigned_service,
		       assigned_at, started_at, completed_at, due_at,
		       retry_count, max_retries,
		       input_payload, output_payload, metadata, error_detail,
		       idempotency_key, version, created_at, updated_at
		FROM tasks
		WHERE status = 'PENDING'
		  AND (assigned_service IS NULL OR assigned_service = $1)
		  AND (due_at IS NULL OR due_at <= now())
		  AND (next_retry_at IS NULL OR next_retry_at <= now())
		  AND (is_poison_pill = FALSE OR is_poison_pill IS NULL)
		ORDER BY priority DESC, due_at ASC NULLS LAST
		LIMIT $2
		FOR UPDATE SKIP LOCKED`
	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, baseQuery, []interface{}{service, batchSize})
	if scopeErr != nil {
		return nil, fmt.Errorf("ClaimTasks: %w", scopeErr)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query claimable tasks: %w", err)
	}
	defer rows.Close()

	var tasks []model.Task
	var ids []string
	for rows.Next() {
		var t model.Task
		var status string
		var priority int
		if err := rows.Scan(
			&t.ID, &t.CaseID, &t.TaskDefinitionCode, &t.ActivityCode, &t.StageCode,
			&t.TenantID,
			&status, &priority, &t.AssignedService,
			&t.AssignedAt, &t.StartedAt, &t.CompletedAt, &t.DueAt,
			&t.RetryCount, &t.MaxRetries,
			&t.InputPayload, &t.OutputPayload, &t.Metadata, &t.ErrorDetail,
			&t.IdempotencyKey, &t.Version, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		t.Status = model.TaskStatus(status)
		t.Priority = model.TaskPriority(priority)
		tasks = append(tasks, t)
		ids = append(ids, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// 2. Atomically mark all selected tasks as ASSIGNED
	_, err = tx.Exec(ctx, `
		UPDATE tasks
		SET status            = 'ASSIGNED',
		    assigned_service  = $1,
		    assigned_at       = now(),
		    last_heartbeat_at = now(),
		    version           = version + 1
		WHERE id = ANY($2::uuid[])
		  AND tenant_id = $3::uuid`,
		service, ids, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to mark tasks as ASSIGNED: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit claim transaction: %w", err)
	}

	// 3. Update in-memory status
	now := time.Now().UTC()
	for i := range tasks {
		tasks[i].Status = model.TaskStatusAssigned
		tasks[i].AssignedService = &service
		tasks[i].AssignedAt = &now
		multitenancy.IncTasksClaimed(tenantID, service)
	}

	return tasks, nil
}

// ReleaseTaskClaim clears ASSIGNED claim state when no in-process handler can execute the task.
func (r *Repository) ReleaseTaskClaim(ctx context.Context, taskID string) error {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("ReleaseTaskClaim: taskID is required")
	}
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE tasks
		SET status = 'PENDING',
		    assigned_service = NULL,
		    assigned_at = NULL,
		    last_heartbeat_at = NULL,
		    version = version + 1
		WHERE id = $1::uuid
		  AND status = 'ASSIGNED'`, []interface{}{taskID})
	if scopeErr != nil {
		return fmt.Errorf("ReleaseTaskClaim: %w", scopeErr)
	}
	if _, err := r.Pool.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("ReleaseTaskClaim: release claim: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Heartbeat — updates last_heartbeat_at for a task currently being processed.
// ---------------------------------------------------------------------------

func (r *Repository) Heartbeat(ctx context.Context, taskID string) error {
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE tasks
		SET last_heartbeat_at = now()
		WHERE id = $1::uuid
		  AND status IN ('ASSIGNED', 'IN_PROGRESS')`, []interface{}{taskID})
	if scopeErr != nil {
		return fmt.Errorf("Heartbeat: %w", scopeErr)
	}
	tag, err := r.Pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to heartbeat task %s: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s not found or not in claimable state", taskID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// ReclaimStaleTasks — resets ASSIGNED/IN_PROGRESS tasks whose heartbeat
// has gone stale (older than staleDuration). Returns the count reclaimed.
// ---------------------------------------------------------------------------

func (r *Repository) ReclaimStaleTasks(ctx context.Context, staleDuration time.Duration) (int, error) {
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	cutoff := time.Now().UTC().Add(-staleDuration)

	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE tasks
		SET status            = 'PENDING',
		    assigned_service  = NULL,
		    assigned_at       = NULL,
		    last_heartbeat_at = NULL,
		    version           = version + 1
		WHERE status IN ('ASSIGNED', 'IN_PROGRESS')
		  AND last_heartbeat_at < $1
		  AND last_heartbeat_at IS NOT NULL`, []interface{}{cutoff})
	if scopeErr != nil {
		return 0, fmt.Errorf("ReclaimStaleTasks: %w", scopeErr)
	}
	tag, err := r.Pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to reclaim stale tasks: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ---------------------------------------------------------------------------
// ScheduleRetry — sets a task to PENDING with exponential backoff.
// next_retry_at = NOW() + (2^retry_count * baseInterval)
// If max_retries is exceeded, the task stays FAILED.
// ---------------------------------------------------------------------------

func (r *Repository) ScheduleRetry(ctx context.Context, tx DBExecutor, taskID string, baseInterval time.Duration) error {
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}
	if tx == nil {
		tx = r.Pool
	}

	var retryCount, maxRetries int
	query, args, scopeErr := multitenancy.AssertTenantScope(ctx, `
		SELECT retry_count, max_retries
		FROM tasks
		WHERE id = $1::uuid
		FOR UPDATE`, []interface{}{taskID})
	if scopeErr != nil {
		return fmt.Errorf("ScheduleRetry: %w", scopeErr)
	}
	err := tx.QueryRow(ctx, query, args...).Scan(&retryCount, &maxRetries)
	if err != nil {
		return fmt.Errorf("failed to read retry state for task %s: %w", taskID, err)
	}

	newRetryCount := retryCount + 1
	if newRetryCount > maxRetries {
		return fmt.Errorf("task %s has exceeded max_retries (%d)", taskID, maxRetries)
	}

	// Exponential backoff: 2^retryCount * base
	backoff := time.Duration(math.Pow(2, float64(retryCount))) * baseInterval
	nextRetry := time.Now().UTC().Add(backoff)

	updateQuery, updateArgs, scopeErr := multitenancy.AssertTenantScope(ctx, `
		UPDATE tasks
		SET status        = 'PENDING',
		    retry_count   = $1,
		    next_retry_at = $2,
		    assigned_service  = NULL,
		    assigned_at       = NULL,
		    last_heartbeat_at = NULL,
		    error_detail      = NULL,
		    version           = version + 1
		WHERE id = $3::uuid`, []interface{}{newRetryCount, nextRetry, taskID})
	if scopeErr != nil {
		return fmt.Errorf("ScheduleRetry: %w", scopeErr)
	}
	_, err = tx.Exec(ctx, updateQuery, updateArgs...)
	if err != nil {
		return fmt.Errorf("failed to schedule retry for task %s: %w", taskID, err)
	}

	return nil
}
