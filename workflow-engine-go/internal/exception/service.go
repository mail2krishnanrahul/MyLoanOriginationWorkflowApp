package exception

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type failureContextRow struct {
	TaskID              string `db:"task_id"`
	CaseID              string `db:"case_id"`
	ParentCaseID        *string `db:"parent_case_id"`
	CaseTypeID          string `db:"case_type_id"`
	TaskDefinitionCode  string `db:"task_definition_code"`
	ActivityCode        string `db:"activity_code"`
	StageCode           string `db:"stage_code"`
	RetryCount          int    `db:"retry_count"`
	MaxRetries          int    `db:"max_retries"`
	TotalFailureCount   int    `db:"total_failure_count"`
	Status              string `db:"status"`
	Config              []byte `db:"config"`
}

type retryHistoryInsert struct {
	AttemptNumber           int
	RetryCountBefore        int
	MaxRetries              int
	BackoffStrategy         model.RetryBackoffStrategy
	BaseIntervalSeconds     int
	MaxIntervalSeconds      int
	ComputedIntervalSeconds int
	ScheduledAt             time.Time
	NextAttemptAt           *time.Time
	ErrorCode               string
	ErrorClass              model.ErrorClass
	ErrorDetail             []byte
	SourceService           string
	Outcome                 model.RetryAttemptOutcome
}

type taskLocation struct {
	TaskDef      model.TaskDefinitionV2
	ActivityCode string
	StageCode    string
}

// HandleTaskFailure classifies errors, applies retry policy, moves exhausted tasks to DLQ,
// escalates cases, and starts compensation when configured.
func HandleTaskFailure(ctx context.Context, tx *sqlx.Tx, input TaskFailureInput) error {
	if tx == nil {
		return fmt.Errorf("HandleTaskFailure: tx is nil")
	}
	taskID := strings.TrimSpace(input.TaskID)
	if taskID == "" {
		return fmt.Errorf("HandleTaskFailure: taskID is required")
	}
	if input.Err == nil {
		return fmt.Errorf("HandleTaskFailure: err is required")
	}
	sourceService := strings.TrimSpace(input.SourceService)
	if sourceService == "" {
		sourceService = "unknown-service"
	}

	failureCtx, cfg, taskLoc, err := lockFailureContext(ctx, tx, taskID)
	if err != nil {
		return fmt.Errorf("HandleTaskFailure: %w", err)
	}

	if isTerminalTaskStatus(failureCtx.Status) {
		return fmt.Errorf("HandleTaskFailure: task %s already in terminal state %s", taskID, failureCtx.Status)
	}

	errorCode := ExtractErrorCode(input.Err)
	errorClass := ClassifyErrorCode(errorCode)
	occurredAt := time.Now().UTC()
	errorDetail, errorDetailRaw, err := buildTaskErrorDetail(input, sourceService, errorCode, errorClass, occurredAt)
	if err != nil {
		return fmt.Errorf("HandleTaskFailure: build error detail: %w", err)
	}

	decision, err := evaluateRetryDecision(failureCtx, taskLoc.TaskDef, cfg, errorCode, errorClass)
	if err != nil {
		return fmt.Errorf("HandleTaskFailure: evaluate retry decision: %w", err)
	}

	if decision.ShouldRetry {
		nextAttemptAt := occurredAt.Add(decision.ComputedBackoff)
		nextRetryCount := failureCtx.RetryCount + 1
		totalFailureCount := failureCtx.TotalFailureCount + 1

		_, err = tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'PENDING',
			    retry_count = $1,
			    max_retries = $2,
			    total_failure_count = $3,
			    next_retry_at = $4,
			    assigned_service = NULL,
			    assigned_at = NULL,
			    last_heartbeat_at = NULL,
			    error_detail = $5::jsonb,
			    last_error_code = $6,
			    last_error_class = $7,
			    updated_at = now(),
			    version = version + 1
			WHERE id = $8::uuid
		`, nextRetryCount, decision.Policy.MaxRetries, totalFailureCount, nextAttemptAt, errorDetailRaw, errorCode, string(errorClass), failureCtx.TaskID)
		if err != nil {
			return fmt.Errorf("HandleTaskFailure: schedule retry update: %w", err)
		}

		nextAttemptPtr := &nextAttemptAt
		retryHistory := retryHistoryInsert{
			AttemptNumber:           decision.AttemptNumber,
			RetryCountBefore:        failureCtx.RetryCount,
			MaxRetries:              decision.Policy.MaxRetries,
			BackoffStrategy:         decision.Policy.BackoffStrategy,
			BaseIntervalSeconds:     decision.Policy.BaseIntervalSeconds,
			MaxIntervalSeconds:      decision.Policy.MaxIntervalSeconds,
			ComputedIntervalSeconds: int(decision.ComputedBackoff / time.Second),
			ScheduledAt:             occurredAt,
			NextAttemptAt:           nextAttemptPtr,
			ErrorCode:               errorCode,
			ErrorClass:              errorClass,
			ErrorDetail:             errorDetailRaw,
			SourceService:           sourceService,
			Outcome:                 model.RetryAttemptOutcomeScheduled,
		}
		if err := insertRetryHistory(ctx, tx, failureCtx, retryHistory); err != nil {
			return fmt.Errorf("HandleTaskFailure: insert retry history: %w", err)
		}
		if err := publishTaskFailedEvent(ctx, tx, failureCtx, decision, errorCode, errorClass, errorDetail); err != nil {
			return fmt.Errorf("HandleTaskFailure: publish TASK_FAILED retry attempt: %w", err)
		}

		slog.Warn("task failure scheduled for retry",
			"task_id", failureCtx.TaskID,
			"case_id", failureCtx.CaseID,
			"error_code", errorCode,
			"error_class", errorClass,
			"retry_count", nextRetryCount,
			"max_retries", decision.Policy.MaxRetries,
			"next_attempt_at", nextAttemptAt)
		return nil
	}

	if err := transitionTaskToTerminalFailure(ctx, tx, failureCtx, decision, errorDetailRaw, errorCode, errorClass, occurredAt); err != nil {
		return fmt.Errorf("HandleTaskFailure: transition terminal failure: %w", err)
	}

	retryHistory := retryHistoryInsert{
		AttemptNumber:           decision.AttemptNumber,
		RetryCountBefore:        failureCtx.RetryCount,
		MaxRetries:              decision.Policy.MaxRetries,
		BackoffStrategy:         decision.Policy.BackoffStrategy,
		BaseIntervalSeconds:     decision.Policy.BaseIntervalSeconds,
		MaxIntervalSeconds:      decision.Policy.MaxIntervalSeconds,
		ComputedIntervalSeconds: int(decision.ComputedBackoff / time.Second),
		ScheduledAt:             occurredAt,
		NextAttemptAt:           nil,
		ErrorCode:               errorCode,
		ErrorClass:              errorClass,
		ErrorDetail:             errorDetailRaw,
		SourceService:           sourceService,
		Outcome:                 model.RetryAttemptOutcomeTerminal,
	}
	if err := insertRetryHistory(ctx, tx, failureCtx, retryHistory); err != nil {
		return fmt.Errorf("HandleTaskFailure: insert terminal retry history: %w", err)
	}

	if err := insertDLQEntry(ctx, tx, failureCtx, decision, errorDetailRaw, errorDetail.Message, occurredAt); err != nil {
		return fmt.Errorf("HandleTaskFailure: insert DLQ entry: %w", err)
	}

	if err := publishTaskFailedEvent(ctx, tx, failureCtx, decision, errorCode, errorClass, errorDetail); err != nil {
		return fmt.Errorf("HandleTaskFailure: publish TASK_FAILED: %w", err)
	}

	if decision.PoisonPill {
		if err := publishPoisonPillEvent(ctx, tx, failureCtx, decision, errorCode); err != nil {
			return fmt.Errorf("HandleTaskFailure: publish poison pill event: %w", err)
		}
	}

	if ShouldEscalateCase(taskLoc.TaskDef, errorClass, decision.RetriesExhausted) {
		if err := escalateCaseException(ctx, tx, failureCtx, taskLoc.TaskDef, errorDetail); err != nil {
			return fmt.Errorf("HandleTaskFailure: escalate case exception: %w", err)
		}
	}

	if strings.TrimSpace(taskLoc.TaskDef.CompensatingTaskCode) != "" {
		if err := startCompensation(ctx, tx, failureCtx, cfg, taskLoc, errorDetail); err != nil {
			return fmt.Errorf("HandleTaskFailure: start compensation: %w", err)
		}
	}

	slog.Error("task moved to terminal failure",
		"task_id", failureCtx.TaskID,
		"case_id", failureCtx.CaseID,
		"error_code", errorCode,
		"error_class", errorClass,
		"retries_exhausted", decision.RetriesExhausted,
		"poison_pill", decision.PoisonPill)

	return nil
}

// SyncCompensationStateForTask updates compensation tracking when a compensation task completes/fails.
func SyncCompensationStateForTask(ctx context.Context, tx *sqlx.Tx, taskID string, taskStatus model.TaskStatus) error {
	if tx == nil {
		return fmt.Errorf("SyncCompensationStateForTask: tx is nil")
	}
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("SyncCompensationStateForTask: taskID is required")
	}

	nextStatus := ""
	eventType := model.EventType("")
	switch taskStatus {
	case model.TaskStatusDone:
		nextStatus = string(model.CompensationStatusCompleted)
		eventType = model.EventCompensationCompleted
	case model.TaskStatusFailed:
		nextStatus = string(model.CompensationStatusFailed)
		eventType = model.EventCompensationFailed
	default:
		return nil
	}

	var compensationID string
	var caseID string
	err := tx.QueryRowContext(ctx, `
		UPDATE task_compensations
		SET status = $1,
		    completed_at = now(),
		    updated_at = now()
		WHERE compensating_task_id = $2::uuid
		RETURNING compensation_id::text, case_id::text
	`, nextStatus, taskID).Scan(&compensationID, &caseID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("SyncCompensationStateForTask: update compensation: %w", err)
	}

	payload, err := json.Marshal(map[string]interface{}{
		"case_id":          caseID,
		"task_id":          taskID,
		"compensation_id":  compensationID,
		"compensation_status": nextStatus,
	})
	if err != nil {
		return fmt.Errorf("SyncCompensationStateForTask: marshal payload: %w", err)
	}

	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		TaskID:        &taskID,
		EventType:     eventType,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("SyncCompensationStateForTask: publish event: %w", err)
	}

	slog.Info("compensation state synced", "compensation_id", compensationID, "status", nextStatus, "task_id", taskID)
	return nil
}

func lockFailureContext(ctx context.Context, tx *sqlx.Tx, taskID string) (failureContextRow, *model.CaseTypeConfig, taskLocation, error) {
	var row failureContextRow
	err := tx.QueryRowxContext(ctx, `
		SELECT
			t.id::text AS task_id,
			t.case_id::text AS case_id,
			c.parent_case_id::text AS parent_case_id,
			c.case_type_id::text AS case_type_id,
			t.task_definition_code,
			t.activity_code,
			t.stage_code,
			t.retry_count,
			t.max_retries,
			t.total_failure_count,
			t.status,
			ct.config
		FROM tasks t
		JOIN cases c ON c.id = t.case_id
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE t.id = $1::uuid
		FOR UPDATE
	`, taskID).StructScan(&row)
	if err != nil {
		return failureContextRow{}, nil, taskLocation{}, fmt.Errorf("lockFailureContext: query context: %w", err)
	}

	cfg := &model.CaseTypeConfig{}
	if err := json.Unmarshal(row.Config, cfg); err != nil {
		return failureContextRow{}, nil, taskLocation{}, fmt.Errorf("lockFailureContext: unmarshal case type config: %w", err)
	}

	taskLoc, ok := findTaskDefinitionWithLocation(cfg, row.TaskDefinitionCode)
	if !ok {
		taskLoc = taskLocation{
			TaskDef: model.TaskDefinitionV2{
				Code: row.TaskDefinitionCode,
			},
			ActivityCode: row.ActivityCode,
			StageCode:    row.StageCode,
		}
	}
	return row, cfg, taskLoc, nil
}

func evaluateRetryDecision(
	ctx failureContextRow,
	taskDef model.TaskDefinitionV2,
	cfg *model.CaseTypeConfig,
	errorCode string,
	errorClass model.ErrorClass,
) (RetryDecision, error) {
	policy := ResolveRetryPolicy(taskDef)
	if policy.MaxRetries == 0 && ctx.MaxRetries > 0 {
		policy.MaxRetries = ctx.MaxRetries
	}
	if policy.MaxRetries < 0 {
		return RetryDecision{}, fmt.Errorf("evaluateRetryDecision: max retries must be >= 0")
	}

	attemptNumber := ctx.RetryCount + 1
	retryableByCode := isErrorCodeRetryable(policy, errorCode)
	isClassRetryCandidate := errorClass == model.ErrorClassTransient || (errorClass == model.ErrorClassUnknown && len(policy.RetryableErrorCodes) > 0)
	withinRetryBudget := attemptNumber <= policy.MaxRetries

	decision := RetryDecision{
		Policy:           policy,
		ErrorCode:        errorCode,
		ErrorClass:       errorClass,
		AttemptNumber:    attemptNumber,
		RetriesExhausted: !withinRetryBudget,
	}

	if isClassRetryCandidate && retryableByCode && withinRetryBudget {
		backoff, err := ComputeBackoffDuration(policy, attemptNumber)
		if err != nil {
			return RetryDecision{}, fmt.Errorf("evaluateRetryDecision: compute backoff: %w", err)
		}
		decision.ShouldRetry = true
		decision.ComputedBackoff = backoff
	}

	if errorClass == model.ErrorClassPermanent {
		decision.ShouldRetry = false
		decision.RetriesExhausted = true
	}

	threshold := ResolvePoisonPillThreshold(taskDef, cfg)
	totalFailures := ctx.TotalFailureCount + 1
	if threshold > 0 && totalFailures >= threshold {
		decision.PoisonPill = true
		decision.ShouldRetry = false
		decision.RetriesExhausted = true
		decision.PoisonPillReason = fmt.Sprintf("total failures %d reached poison-pill threshold %d", totalFailures, threshold)
	}

	if decision.Policy.MaxIntervalSeconds < decision.Policy.BaseIntervalSeconds {
		decision.Policy.MaxIntervalSeconds = decision.Policy.BaseIntervalSeconds
	}
	return decision, nil
}

func buildTaskErrorDetail(
	input TaskFailureInput,
	sourceService string,
	errorCode string,
	errorClass model.ErrorClass,
	occurredAt time.Time,
) (model.TaskErrorDetail, []byte, error) {
	detail := model.TaskErrorDetail{
		ErrorCode:     errorCode,
		ErrorClass:    errorClass,
		Message:       strings.TrimSpace(input.Err.Error()),
		SourceService: sourceService,
		OccurredAt:    occurredAt,
	}
	if detail.Message == "" {
		detail.Message = "task execution failed"
	}

	if input.RecoveredStack != nil && strings.TrimSpace(*input.RecoveredStack) != "" {
		stack := strings.TrimSpace(*input.RecoveredStack)
		detail.StackContext = &stack
	} else {
		var stackCarrier StackContextCarrier
		if input.Err != nil && strings.TrimSpace(detail.Message) != "" {
			if ok := errorAs(input.Err, &stackCarrier); ok {
				stack := strings.TrimSpace(stackCarrier.StackContext())
				if stack != "" {
					detail.StackContext = &stack
				}
			}
		}
	}

	var upstream UpstreamErrorCarrier
	if input.Err != nil && errorAs(input.Err, &upstream) {
		detail.UpstreamError = upstream.UpstreamError()
	}

	raw, err := json.Marshal(detail)
	if err != nil {
		return model.TaskErrorDetail{}, nil, fmt.Errorf("buildTaskErrorDetail: marshal: %w", err)
	}
	return detail, raw, nil
}

func errorAs(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case *StackContextCarrier:
		carrier, ok := err.(StackContextCarrier)
		if !ok {
			return false
		}
		*t = carrier
		return true
	case *UpstreamErrorCarrier:
		carrier, ok := err.(UpstreamErrorCarrier)
		if !ok {
			return false
		}
		*t = carrier
		return true
	default:
		return false
	}
}

func transitionTaskToTerminalFailure(
	ctx context.Context,
	tx *sqlx.Tx,
	failureCtx failureContextRow,
	decision RetryDecision,
	errorDetailRaw []byte,
	errorCode string,
	errorClass model.ErrorClass,
	at time.Time,
) error {
	totalFailures := failureCtx.TotalFailureCount + 1
	_, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'FAILED',
		    retry_count = $1,
		    max_retries = $2,
		    total_failure_count = $3,
		    next_retry_at = NULL,
		    completed_at = COALESCE(completed_at, $4),
		    assigned_service = NULL,
		    assigned_at = NULL,
		    last_heartbeat_at = NULL,
		    error_detail = $5::jsonb,
		    last_error_code = $6,
		    last_error_class = $7,
		    is_poison_pill = $8,
		    poison_pill_quarantined_at = CASE WHEN $8 THEN COALESCE(poison_pill_quarantined_at, $4) ELSE poison_pill_quarantined_at END,
		    poison_pill_reason = CASE WHEN $8 THEN $9 ELSE poison_pill_reason END,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $10::uuid
	`, failureCtx.RetryCount, decision.Policy.MaxRetries, totalFailures, at, errorDetailRaw, errorCode, string(errorClass), decision.PoisonPill, nullIfBlank(decision.PoisonPillReason), failureCtx.TaskID)
	if err != nil {
		return fmt.Errorf("transitionTaskToTerminalFailure: update task: %w", err)
	}
	return nil
}

func insertDLQEntry(
	ctx context.Context,
	tx *sqlx.Tx,
	failureCtx failureContextRow,
	decision RetryDecision,
	errorDetailRaw []byte,
	failureReason string,
	movedAt time.Time,
) error {
	if strings.TrimSpace(failureReason) == "" {
		failureReason = "task execution failed"
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO task_dlq (
			task_id,
			case_id,
			failure_reason,
			error_detail,
			moved_at,
			requeue_count,
			last_requeue_at,
			is_poison_pill
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4::jsonb,
			$5,
			0,
			NULL,
			$6
		)
	`, failureCtx.TaskID, failureCtx.CaseID, failureReason, errorDetailRaw, movedAt, decision.PoisonPill)
	if err != nil {
		return fmt.Errorf("insertDLQEntry: insert: %w", err)
	}
	return nil
}

func insertRetryHistory(ctx context.Context, tx *sqlx.Tx, failureCtx failureContextRow, row retryHistoryInsert) error {
	_, err := tx.ExecContext(ctx, `
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
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14::jsonb,
			$15,
			$16
		)
	`,
		failureCtx.TaskID,
		failureCtx.CaseID,
		row.AttemptNumber,
		row.RetryCountBefore,
		row.MaxRetries,
		string(row.BackoffStrategy),
		row.BaseIntervalSeconds,
		row.MaxIntervalSeconds,
		row.ComputedIntervalSeconds,
		row.ScheduledAt,
		row.NextAttemptAt,
		row.ErrorCode,
		string(row.ErrorClass),
		row.ErrorDetail,
		row.SourceService,
		string(row.Outcome),
	)
	if err != nil {
		return fmt.Errorf("insertRetryHistory: insert: %w", err)
	}
	return nil
}

func publishTaskFailedEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	failureCtx failureContextRow,
	decision RetryDecision,
	errorCode string,
	errorClass model.ErrorClass,
	errorDetail model.TaskErrorDetail,
) error {
	payload, err := json.Marshal(map[string]interface{}{
		"case_id":              failureCtx.CaseID,
		"task_id":              failureCtx.TaskID,
		"task_definition_code": failureCtx.TaskDefinitionCode,
		"stage_code":           failureCtx.StageCode,
		"activity_code":        failureCtx.ActivityCode,
		"status":               string(model.TaskStatusFailed),
		"retry_count":          failureCtx.RetryCount,
		"max_retries":          decision.Policy.MaxRetries,
		"retries_exhausted":    decision.RetriesExhausted,
		"error_code":           errorCode,
		"error_class":          errorClass,
		"poison_pill":          decision.PoisonPill,
		"error_detail":         errorDetail,
	})
	if err != nil {
		return fmt.Errorf("publishTaskFailedEvent: marshal payload: %w", err)
	}
	caseID := failureCtx.CaseID
	taskID := failureCtx.TaskID
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		TaskID:        &taskID,
		EventType:     model.EventTaskFailed,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("publishTaskFailedEvent: %w", err)
	}
	return nil
}

func publishPoisonPillEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	failureCtx failureContextRow,
	decision RetryDecision,
	errorCode string,
) error {
	payload, err := json.Marshal(map[string]interface{}{
		"case_id":            failureCtx.CaseID,
		"task_id":            failureCtx.TaskID,
		"task_definition_code": failureCtx.TaskDefinitionCode,
		"error_code":         errorCode,
		"reason":             decision.PoisonPillReason,
	})
	if err != nil {
		return fmt.Errorf("publishPoisonPillEvent: marshal payload: %w", err)
	}
	caseID := failureCtx.CaseID
	taskID := failureCtx.TaskID
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		TaskID:        &taskID,
		EventType:     model.EventTaskPoisonPillDetected,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("publishPoisonPillEvent: %w", err)
	}
	return nil
}

func escalateCaseException(
	ctx context.Context,
	tx *sqlx.Tx,
	failureCtx failureContextRow,
	taskDef model.TaskDefinitionV2,
	errorDetail model.TaskErrorDetail,
) error {
	severity := string(ResolveFailureSeverity(taskDef))
	reason := strings.TrimSpace(errorDetail.Message)
	if reason == "" {
		reason = "task failed"
	}

	if err := updateCaseExceptionStatus(ctx, tx, failureCtx.CaseID, failureCtx.TaskID, severity, reason, model.EventCaseExceptionRaised, nil); err != nil {
		return fmt.Errorf("escalateCaseException: update child case: %w", err)
	}

	if failureCtx.ParentCaseID != nil && strings.TrimSpace(*failureCtx.ParentCaseID) != "" {
		propagatedReason := fmt.Sprintf("sub-case %s in exception due to task %s", failureCtx.CaseID, failureCtx.TaskID)
		if err := updateCaseExceptionStatus(ctx, tx, strings.TrimSpace(*failureCtx.ParentCaseID), failureCtx.TaskID, severity, propagatedReason, model.EventCaseExceptionPropagated, &failureCtx.CaseID); err != nil {
			return fmt.Errorf("escalateCaseException: propagate to parent: %w", err)
		}
	}

	return nil
}

func updateCaseExceptionStatus(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	taskID string,
	severity string,
	reason string,
	eventType model.EventType,
	subCaseID *string,
) error {
	if strings.TrimSpace(caseID) == "" {
		return nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET status = 'EXCEPTION',
		    exception_at = COALESCE(exception_at, now()),
		    exception_reason = $1,
		    exception_task_id = $2::uuid,
		    exception_severity = $3,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $4::uuid
		  AND status NOT IN ('COMPLETED', 'CANCELLED', 'REJECTED')
	`, reason, taskID, severity, caseID)
	if err != nil {
		return fmt.Errorf("updateCaseExceptionStatus: update case %s: %w", caseID, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("updateCaseExceptionStatus: rows affected: %w", err)
	}
	if rows == 0 {
		return nil
	}

	payloadMap := map[string]interface{}{
		"case_id":     caseID,
		"task_id":     taskID,
		"severity":    severity,
		"reason":      reason,
		"status":      model.CaseStatusException,
	}
	if subCaseID != nil {
		payloadMap["sub_case_id"] = *subCaseID
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return fmt.Errorf("updateCaseExceptionStatus: marshal payload: %w", err)
	}

	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		TaskID:        &taskID,
		EventType:     eventType,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("updateCaseExceptionStatus: publish event: %w", err)
	}

	slog.Warn("case moved to exception", "case_id", caseID, "task_id", taskID, "severity", severity, "reason", reason)
	return nil
}

func startCompensation(
	ctx context.Context,
	tx *sqlx.Tx,
	failureCtx failureContextRow,
	cfg *model.CaseTypeConfig,
	failedTask taskLocation,
	errorDetail model.TaskErrorDetail,
) error {
	code := strings.TrimSpace(failedTask.TaskDef.CompensatingTaskCode)
	if code == "" {
		return nil
	}
	compTaskLoc, ok := findTaskDefinitionWithLocation(cfg, code)
	if !ok {
		compTaskLoc = taskLocation{
			TaskDef: model.TaskDefinitionV2{Code: code},
			ActivityCode: failedTask.ActivityCode,
			StageCode: failedTask.StageCode,
		}
	}
	policy := ResolveRetryPolicy(compTaskLoc.TaskDef)
	inputPayload, err := json.Marshal(map[string]interface{}{
		"failed_task_id":              failureCtx.TaskID,
		"failed_task_definition_code": failureCtx.TaskDefinitionCode,
		"reason":                      errorDetail.Message,
		"source_case_id":              failureCtx.CaseID,
	})
	if err != nil {
		return fmt.Errorf("startCompensation: marshal payload: %w", err)
	}

	idempotencyKey := fmt.Sprintf("compensate:%s:%s", failureCtx.TaskID, code)
	var compensationTaskID string
	err = tx.QueryRowContext(ctx, `
		WITH inserted AS (
			INSERT INTO tasks (
				case_id,
				task_definition_code,
				activity_code,
				stage_code,
				status,
				priority,
				assigned_service,
				max_retries,
				input_payload,
				output_payload,
				metadata,
				idempotency_key
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4,
				'PENDING',
				2,
				NULL,
				$5,
				$6::jsonb,
				'{}'::jsonb,
				'{}'::jsonb,
				$7
			)
			ON CONFLICT (idempotency_key) DO NOTHING
			RETURNING id::text
		)
		SELECT id FROM inserted
		UNION ALL
		SELECT id::text FROM tasks WHERE idempotency_key = $7
		LIMIT 1
	`,
		failureCtx.CaseID,
		code,
		nonBlankOrDefault(compTaskLoc.ActivityCode, failureCtx.ActivityCode),
		nonBlankOrDefault(compTaskLoc.StageCode, failureCtx.StageCode),
		policy.MaxRetries,
		inputPayload,
		idempotencyKey,
	).Scan(&compensationTaskID)
	if err != nil {
		return fmt.Errorf("startCompensation: upsert compensation task: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_compensations (
			case_id,
			failed_task_id,
			failed_task_definition_code,
			compensating_task_code,
			compensating_task_id,
			status,
			started_at,
			error_detail
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			$5::uuid,
			'PENDING',
			now(),
			$6::jsonb
		)
		ON CONFLICT (failed_task_id, compensating_task_code)
		DO UPDATE SET
			compensating_task_id = EXCLUDED.compensating_task_id,
			updated_at = now()
	`, failureCtx.CaseID, failureCtx.TaskID, failureCtx.TaskDefinitionCode, code, compensationTaskID, marshalMap(map[string]interface{}{
		"source_task_id": failureCtx.TaskID,
		"reason":         errorDetail.Message,
	}))
	if err != nil {
		return fmt.Errorf("startCompensation: upsert compensation row: %w", err)
	}

	payload, err := json.Marshal(map[string]interface{}{
		"case_id":                  failureCtx.CaseID,
		"failed_task_id":           failureCtx.TaskID,
		"failed_task_definition_code": failureCtx.TaskDefinitionCode,
		"compensating_task_code":   code,
		"compensating_task_id":     compensationTaskID,
	})
	if err != nil {
		return fmt.Errorf("startCompensation: marshal event payload: %w", err)
	}
	caseID := failureCtx.CaseID
	taskID := compensationTaskID
	if err := sla.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseID,
		TaskID:        &taskID,
		EventType:     model.EventCompensationStarted,
		Payload:       payload,
		Status:        model.EventStatusPending,
		TargetService: "case-orchestrator",
	}); err != nil {
		return fmt.Errorf("startCompensation: publish event: %w", err)
	}

	slog.Warn("compensation task created",
		"case_id", failureCtx.CaseID,
		"failed_task_id", failureCtx.TaskID,
		"compensating_task_id", compensationTaskID,
		"compensating_task_code", code)
	return nil
}

func findTaskDefinitionWithLocation(config *model.CaseTypeConfig, taskDefinitionCode string) (taskLocation, bool) {
	if config == nil {
		return taskLocation{}, false
	}
	target := strings.TrimSpace(taskDefinitionCode)
	for _, stage := range config.Stages {
		for _, activity := range stage.Activities {
			for _, taskDef := range activity.TaskDefs {
				if strings.TrimSpace(taskDef.Code) == target {
					return taskLocation{
						TaskDef:      taskDef,
						ActivityCode: activity.Code,
						StageCode:    stage.Code,
					}, true
				}
			}
		}
	}
	return taskLocation{}, false
}

func isTerminalTaskStatus(status string) bool {
	s := strings.TrimSpace(strings.ToUpper(status))
	return s == string(model.TaskStatusDone) || s == string(model.TaskStatusFailed) || s == string(model.TaskStatusCancelled) || s == string(model.TaskStatusSkipped)
}

func marshalMap(value map[string]interface{}) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func nullIfBlank(v string) interface{} {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func nonBlankOrDefault(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}
