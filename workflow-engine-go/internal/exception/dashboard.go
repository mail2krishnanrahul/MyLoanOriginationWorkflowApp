package exception

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ListExceptionCases returns cases currently in EXCEPTION status (oldest first).
func ListExceptionCases(ctx context.Context, db *sqlx.DB, limit int) ([]ExceptionCaseSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("ListExceptionCases: db is nil")
	}
	if limit <= 0 {
		limit = 100
	}

	rows, err := db.QueryxContext(ctx, `
		SELECT
			c.id::text AS case_id,
			c.reference_number,
			c.case_type_id::text AS case_type_id,
			c.status,
			c.exception_at,
			c.exception_reason,
			c.exception_severity,
			c.exception_task_id::text AS exception_task_id,
			t.task_definition_code,
			t.last_error_code,
			c.created_at
		FROM cases c
		LEFT JOIN tasks t ON t.id = c.exception_task_id
		WHERE c.status = 'EXCEPTION'
		ORDER BY c.exception_at ASC NULLS LAST, c.created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("ListExceptionCases: query: %w", err)
	}
	defer rows.Close()

	out := make([]ExceptionCaseSummary, 0)
	for rows.Next() {
		var item ExceptionCaseSummary
		if err := rows.StructScan(&item); err != nil {
			return nil, fmt.Errorf("ListExceptionCases: scan: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListExceptionCases: rows: %w", err)
	}
	return out, nil
}

// GetDLQEntries returns DLQ entries for a case with full error_detail payload.
func GetDLQEntries(ctx context.Context, db *sqlx.DB, caseID string) ([]TaskDLQEntry, error) {
	if db == nil {
		return nil, fmt.Errorf("GetDLQEntries: db is nil")
	}
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("GetDLQEntries: caseID is required")
	}

	rows, err := db.QueryxContext(ctx, `
		SELECT
			dlq_id::text AS dlq_id,
			task_id::text AS task_id,
			case_id::text AS case_id,
			failure_reason,
			error_detail,
			moved_at,
			requeue_count,
			last_requeue_at,
			is_poison_pill,
			quarantine_released_at,
			soft_deleted_at,
			created_at
		FROM task_dlq
		WHERE case_id = $1::uuid
		ORDER BY moved_at DESC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("GetDLQEntries: query: %w", err)
	}
	defer rows.Close()

	entries := make([]TaskDLQEntry, 0)
	for rows.Next() {
		var entry TaskDLQEntry
		if err := rows.StructScan(&entry); err != nil {
			return nil, fmt.Errorf("GetDLQEntries: scan: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetDLQEntries: rows: %w", err)
	}
	return entries, nil
}

// GetRetryHistory returns all retry attempts for a task ordered by attempt number.
func GetRetryHistory(ctx context.Context, db *sqlx.DB, taskID string) ([]RetryHistoryEntry, error) {
	if db == nil {
		return nil, fmt.Errorf("GetRetryHistory: db is nil")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("GetRetryHistory: taskID is required")
	}

	rows, err := db.QueryxContext(ctx, `
		SELECT
			attempt_id::text AS attempt_id,
			task_id::text AS task_id,
			case_id::text AS case_id,
			attempt_number,
			retry_count_before,
			max_retries,
			backoff_strategy,
			base_interval_seconds,
			max_interval_seconds,
			computed_interval_seconds,
			scheduled_at,
			next_attempt_at,
			error_code,
			error_class,
			source_service,
			outcome
		FROM task_retry_history
		WHERE task_id = $1::uuid
		ORDER BY attempt_number ASC, scheduled_at ASC
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("GetRetryHistory: query: %w", err)
	}
	defer rows.Close()

	history := make([]RetryHistoryEntry, 0)
	for rows.Next() {
		var item RetryHistoryEntry
		var backoffStrategy string
		var errorClass string
		var outcome string
		if err := rows.Scan(
			&item.AttemptID,
			&item.TaskID,
			&item.CaseID,
			&item.AttemptNumber,
			&item.RetryCountBefore,
			&item.MaxRetries,
			&backoffStrategy,
			&item.BaseIntervalSeconds,
			&item.MaxIntervalSeconds,
			&item.ComputedIntervalSeconds,
			&item.ScheduledAt,
			&item.NextAttemptAt,
			&item.ErrorCode,
			&errorClass,
			&item.SourceService,
			&outcome,
		); err != nil {
			return nil, fmt.Errorf("GetRetryHistory: scan: %w", err)
		}
		item.BackoffStrategy = model.RetryBackoffStrategy(backoffStrategy)
		item.ErrorClass = model.ErrorClass(errorClass)
		item.Outcome = model.RetryAttemptOutcome(outcome)
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetRetryHistory: rows: %w", err)
	}
	return history, nil
}

// RequeueDLQEntry requeues a DLQ task back to PENDING with retry_count reset.
func RequeueDLQEntry(ctx context.Context, tx *sqlx.Tx, dlqID string, operatorID string) error {
	if tx == nil {
		return fmt.Errorf("RequeueDLQEntry: tx is nil")
	}
	if strings.TrimSpace(dlqID) == "" {
		return fmt.Errorf("RequeueDLQEntry: dlqID is required")
	}
	if strings.TrimSpace(operatorID) == "" {
		return fmt.Errorf("RequeueDLQEntry: operatorID is required")
	}

	type dlqRow struct {
		TaskID        string `db:"task_id"`
		CaseID        string `db:"case_id"`
		RequeueCount  int    `db:"requeue_count"`
		IsPoisonPill  bool   `db:"is_poison_pill"`
	}
	var row dlqRow
	err := tx.QueryRowxContext(ctx, `
		SELECT
			task_id::text AS task_id,
			case_id::text AS case_id,
			requeue_count,
			is_poison_pill
		FROM task_dlq
		WHERE dlq_id = $1::uuid
		  AND soft_deleted_at IS NULL
		FOR UPDATE
	`, dlqID).StructScan(&row)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("RequeueDLQEntry: active dlq entry %s not found", dlqID)
		}
		return fmt.Errorf("RequeueDLQEntry: load dlq entry: %w", err)
	}

	type taskStateRow struct {
		RetryCount int    `db:"retry_count"`
		MaxRetries int    `db:"max_retries"`
		Status     string `db:"status"`
	}
	var taskState taskStateRow
	err = tx.QueryRowxContext(ctx, `
		SELECT retry_count, max_retries, status
		FROM tasks
		WHERE id = $1::uuid
		FOR UPDATE
	`, row.TaskID).StructScan(&taskState)
	if err != nil {
		return fmt.Errorf("RequeueDLQEntry: load task state: %w", err)
	}

	var generatedID string
	if err := tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&generatedID); err != nil {
		return fmt.Errorf("RequeueDLQEntry: generate idempotency suffix: %w", err)
	}
	newIdempotency := fmt.Sprintf("requeue:%s:%s", row.TaskID, generatedID)

	_, err = tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'PENDING',
		    retry_count = 0,
		    next_retry_at = NULL,
		    assigned_service = NULL,
		    assigned_at = NULL,
		    started_at = NULL,
		    completed_at = NULL,
		    last_heartbeat_at = NULL,
		    error_detail = NULL,
		    is_poison_pill = FALSE,
		    poison_pill_quarantined_at = NULL,
		    poison_pill_reason = NULL,
		    idempotency_key = $1,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid
	`, newIdempotency, row.TaskID)
	if err != nil {
		return fmt.Errorf("RequeueDLQEntry: update task: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE task_dlq
		SET soft_deleted_at = now(),
		    requeue_count = requeue_count + 1,
		    last_requeue_at = now(),
		    quarantine_released_at = CASE WHEN is_poison_pill THEN now() ELSE quarantine_released_at END,
		    updated_at = now()
		WHERE dlq_id = $1::uuid
	`, dlqID)
	if err != nil {
		return fmt.Errorf("RequeueDLQEntry: soft-delete dlq entry: %w", err)
	}

	payloadMap := map[string]interface{}{
		"operator_id":     operatorID,
		"source":          "DLQ_REQUEUE",
		"dlq_id":          dlqID,
		"task_id":         row.TaskID,
		"case_id":         row.CaseID,
		"new_idempotency": newIdempotency,
	}
	errorDetailRaw, marshalErr := json.Marshal(payloadMap)
	if marshalErr != nil {
		return fmt.Errorf("RequeueDLQEntry: marshal retry history detail: %w", marshalErr)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO task_retry_history (
			task_id,
			case_id,
			attempt_number,
			retry_count_before,
			max_retries,
			backoff_strategy,
			base_interval_seconds,
			max_interval_seconds,
			computed_interval_seconds,
			scheduled_at,
			next_attempt_at,
			error_code,
			error_class,
			error_detail,
			source_service,
			outcome
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			0,
			$3,
			$4,
			'FIXED',
			1,
			1,
			0,
			now(),
			NULL,
			'DLQ_REQUEUE',
			'UNKNOWN',
			$5::jsonb,
			$6,
			'DLQ_REQUEUED'
		)
	`, row.TaskID, row.CaseID, taskState.RetryCount, taskState.MaxRetries, errorDetailRaw, operatorID); err != nil {
		return fmt.Errorf("RequeueDLQEntry: insert retry history: %w", err)
	}

	eventPayload, err := json.Marshal(map[string]interface{}{
		"case_id":       row.CaseID,
		"task_id":       row.TaskID,
		"dlq_id":        dlqID,
		"operator_id":   operatorID,
		"requeue_count": row.RequeueCount + 1,
		"released_poison_pill": row.IsPoisonPill,
	})
	if err != nil {
		return fmt.Errorf("RequeueDLQEntry: marshal event payload: %w", err)
	}
	caseID := row.CaseID
	taskID := row.TaskID
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		TaskID:        &taskID,
		EventType:     model.EventTaskRequeued,
		Payload:       eventPayload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("RequeueDLQEntry: publish TASK_REQUEUED: %w", err)
	}

	slog.Warn("DLQ entry requeued", "dlq_id", dlqID, "task_id", row.TaskID, "operator_id", operatorID)
	return nil
}

// PoisonPillRelease allows operator workflows to explicitly release quarantined tasks.
func PoisonPillRelease(ctx context.Context, tx *sqlx.Tx, taskID string, operatorID string) error {
	if tx == nil {
		return fmt.Errorf("PoisonPillRelease: tx is nil")
	}
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("PoisonPillRelease: taskID is required")
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET is_poison_pill = FALSE,
		    poison_pill_quarantined_at = NULL,
		    poison_pill_reason = NULL,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $1::uuid
	`, taskID)
	if err != nil {
		return fmt.Errorf("PoisonPillRelease: update task: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE task_dlq
		SET quarantine_released_at = now(),
		    updated_at = now()
		WHERE task_id = $1::uuid
		  AND soft_deleted_at IS NULL
	`, taskID)
	if err != nil {
		return fmt.Errorf("PoisonPillRelease: update dlq entries: %w", err)
	}

	_ = operatorID
	return nil
}
