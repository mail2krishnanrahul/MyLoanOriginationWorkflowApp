package sla

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// SLASweepJob scans active SLA entities and emits threshold/breach events.
type SLASweepJob struct {
	db             *sqlx.DB
	eventPublisher EventPublisher
	sweepInterval  time.Duration
	batchSize      int
	logger         *slog.Logger
}

// NewSLASweepJob constructs an SLA sweep worker.
func NewSLASweepJob(db *sqlx.DB, publisher EventPublisher, interval time.Duration, batchSize int, logger *slog.Logger) *SLASweepJob {
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	if batchSize <= 0 {
		batchSize = 2000
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &SLASweepJob{
		db:             db,
		eventPublisher: publisher,
		sweepInterval:  interval,
		batchSize:      batchSize,
		logger:         logger,
	}
}

type taskSweepCandidate struct {
	ID                    string          `db:"id"`
	CaseID                string          `db:"case_id"`
	CaseTypeCode          string          `db:"case_type_code"`
	StageCode             string          `db:"stage_code"`
	ActivityCode          string          `db:"activity_code"`
	TaskDefinitionCode    string          `db:"task_definition_code"`
	Status                string          `db:"status"`
	EffectiveStartTime    time.Time       `db:"effective_start_time"`
	DueAt                 time.Time       `db:"due_at"`
	DurationMS            int64           `db:"duration_ms"`
	WarningThresholdPct   sql.NullFloat64 `db:"warning_threshold_pct"`
	CriticalThresholdPct  sql.NullFloat64 `db:"critical_threshold_pct"`
	WarningIssuedAt       sql.NullTime    `db:"warning_issued_at"`
	CriticalIssuedAt      sql.NullTime    `db:"critical_issued_at"`
	BreachDetectedAt      sql.NullTime    `db:"breach_detected_at"`
	BreachAction          sql.NullString  `db:"breach_action"`
	CalendarID            sql.NullString  `db:"calendar_id"`
	AssigneeID            sql.NullString  `db:"assignee_id"`
	Priority              int             `db:"priority"`
	SLACycle              int             `db:"sla_cycle"`
	CompletedAt           sql.NullTime    `db:"completed_at"`
	CreatedAt             time.Time       `db:"created_at"`
}

type caseSweepCandidate struct {
	ID                    string          `db:"id"`
	CaseTypeCode          string          `db:"case_type_code"`
	Status                string          `db:"status"`
	EffectiveStartTime    time.Time       `db:"effective_start_time"`
	DueAt                 time.Time       `db:"due_at"`
	DurationMS            int64           `db:"duration_ms"`
	WarningThresholdPct   sql.NullFloat64 `db:"warning_threshold_pct"`
	CriticalThresholdPct  sql.NullFloat64 `db:"critical_threshold_pct"`
	WarningIssuedAt       sql.NullTime    `db:"warning_issued_at"`
	CriticalIssuedAt      sql.NullTime    `db:"critical_issued_at"`
	BreachDetectedAt      sql.NullTime    `db:"breach_detected_at"`
	BreachAction          sql.NullString  `db:"breach_action"`
	CalendarID            sql.NullString  `db:"calendar_id"`
	SLACycle              int             `db:"sla_cycle"`
	AssignedTo            sql.NullString  `db:"assigned_to"`
	CompletedAt           sql.NullTime    `db:"completed_at"`
	CreatedAt             time.Time       `db:"created_at"`
}

type metricsKey struct {
	MetricDate          time.Time
	CaseTypeCode        string
	StageCode           string
	ActivityCode        string
	TaskDefinitionCode  string
}

type metricsAccumulator struct {
	TotalCount      int64
	CompletedCount  int64
	BreachedCount   int64
	TotalPauseMins  int64
	ElapsedMinutes  []int
}

// Run executes one full SLA sweep pass.
func (j *SLASweepJob) Run(ctx context.Context) error {
	if j.db == nil {
		return fmt.Errorf("SLASweepJob.Run: db is nil")
	}

	tx, err := j.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("SLASweepJob.Run: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC()
	agg := make(map[metricsKey]*metricsAccumulator)

	if err := j.processTaskBatch(ctx, tx, now, agg); err != nil {
		return fmt.Errorf("SLASweepJob.Run: task batch: %w", err)
	}
	if err := j.processCaseBatch(ctx, tx, now); err != nil {
		return fmt.Errorf("SLASweepJob.Run: case batch: %w", err)
	}
	if err := j.flushMetricsSummary(ctx, tx, agg); err != nil {
		return fmt.Errorf("SLASweepJob.Run: metrics flush: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("SLASweepJob.Run: commit: %w", err)
	}

	j.logger.Info("sla sweep completed", "tasks_checked", len(agg), "batch_size", j.batchSize)
	return nil
}

// Start runs the sweep loop on a ticker and exits when context is cancelled.
func (j *SLASweepJob) Start(ctx context.Context) {
	ticker := time.NewTicker(j.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("sla sweep job stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("sla sweep failed", "error", err)
			}
		}
	}
}

func (j *SLASweepJob) processTaskBatch(ctx context.Context, tx *sqlx.Tx, now time.Time, agg map[metricsKey]*metricsAccumulator) error {
	var rows []taskSweepCandidate
	err := tx.SelectContext(ctx, &rows, `
		SELECT
			t.id::text AS id,
			t.case_id::text AS case_id,
			ct.code AS case_type_code,
			t.stage_code,
			t.activity_code,
			t.task_definition_code,
			t.status,
			COALESCE(t.effective_start_time, t.created_at) AS effective_start_time,
			COALESCE(t.task_due_at, t.due_at) AS due_at,
			COALESCE(t.sla_duration_ms, EXTRACT(EPOCH FROM (COALESCE(t.task_due_at, t.due_at) - COALESCE(t.effective_start_time, t.created_at))) * 1000)::bigint AS duration_ms,
			t.sla_warning_threshold_pct AS warning_threshold_pct,
			t.sla_critical_threshold_pct AS critical_threshold_pct,
			t.sla_warning_issued_at AS warning_issued_at,
			t.sla_critical_issued_at AS critical_issued_at,
			t.sla_breach_detected_at AS breach_detected_at,
			t.sla_breach_action AS breach_action,
			t.sla_calendar_id::text AS calendar_id,
			t.assignee_id,
			t.priority,
			t.sla_cycle,
			t.completed_at,
			t.created_at
		FROM tasks t
		JOIN cases c ON c.id = t.case_id
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE t.status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS')
		  AND COALESCE(t.task_due_at, t.due_at) IS NOT NULL
		ORDER BY COALESCE(t.task_due_at, t.due_at) ASC
		LIMIT $1
		FOR UPDATE OF t SKIP LOCKED
	`, j.batchSize)
	if err != nil {
		return fmt.Errorf("processTaskBatch: query candidates: %w", err)
	}

	for _, c := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}

		if !c.CalendarID.Valid || strings.TrimSpace(c.CalendarID.String) == "" {
			j.logger.Error("task missing calendar_id", "task_id", c.ID)
			continue
		}
		if c.DurationMS <= 0 {
			j.logger.Error("task has non-positive duration", "task_id", c.ID, "duration_ms", c.DurationMS)
			continue
		}

		elapsed, err := BusinessHoursElapsed(ctx, j.db, c.EffectiveStartTime, now, c.CalendarID.String)
		if err != nil {
			j.logger.Error("task elapsed calc failed", "task_id", c.ID, "error", err)
			continue
		}

		duration := time.Duration(c.DurationMS) * time.Millisecond
		elapsedPct := float64(elapsed) / float64(duration) * 100
		warnPct := nullableFloatOr(c.WarningThresholdPct, 80)
		criticalPct := nullableFloatOr(c.CriticalThresholdPct, 95)
		action := model.SLABreachAction(nullableStringOr(c.BreachAction, string(model.SLABreachActionNotifyOnly)))

		if !c.WarningIssuedAt.Valid && elapsedPct >= warnPct && elapsedPct < criticalPct {
			if err := j.issueTaskWarning(ctx, tx, c, warnPct); err != nil {
				j.logger.Error("task warning failed", "task_id", c.ID, "error", err)
			}
		}

		if !c.CriticalIssuedAt.Valid && !c.BreachDetectedAt.Valid && elapsedPct >= criticalPct {
			if err := j.issueTaskCritical(ctx, tx, c, criticalPct, action); err != nil {
				j.logger.Error("task critical failed", "task_id", c.ID, "error", err)
			}
		}

		breachedNow := false
		if now.After(c.DueAt) && !c.BreachDetectedAt.Valid {
			if err := j.breachTask(ctx, tx, c, elapsed, duration, action); err != nil {
				j.logger.Error("task breach failed", "task_id", c.ID, "error", err)
			} else {
				breachedNow = true
			}
		}

		key := metricsKey{
			MetricDate:         time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			CaseTypeCode:       c.CaseTypeCode,
			StageCode:          c.StageCode,
			ActivityCode:       c.ActivityCode,
			TaskDefinitionCode: c.TaskDefinitionCode,
		}
		bucket := agg[key]
		if bucket == nil {
			bucket = &metricsAccumulator{}
			agg[key] = bucket
		}
		bucket.TotalCount++
		if isTerminalTaskStatus(c.Status) {
			bucket.CompletedCount++
		}
		if breachedNow || c.BreachDetectedAt.Valid {
			bucket.BreachedCount++
		}
		bucket.ElapsedMinutes = append(bucket.ElapsedMinutes, int(elapsed/time.Minute))

		pausedMins, err := totalPauseMinutes(ctx, tx, model.SLAEntityTypeTask, c.ID)
		if err == nil {
			bucket.TotalPauseMins += pausedMins
		}
	}

	return nil
}

func (j *SLASweepJob) processCaseBatch(ctx context.Context, tx *sqlx.Tx, now time.Time) error {
	var rows []caseSweepCandidate
	err := tx.SelectContext(ctx, &rows, `
		SELECT
			c.id::text AS id,
			ct.code AS case_type_code,
			c.status,
			COALESCE(c.case_effective_start_time, c.created_at) AS effective_start_time,
			c.case_due_at AS due_at,
			COALESCE(c.case_sla_duration_ms, EXTRACT(EPOCH FROM (c.case_due_at - COALESCE(c.case_effective_start_time, c.created_at))) * 1000)::bigint AS duration_ms,
			c.case_sla_warning_threshold_pct AS warning_threshold_pct,
			c.case_sla_critical_threshold_pct AS critical_threshold_pct,
			c.case_sla_warning_issued_at AS warning_issued_at,
			c.case_sla_critical_issued_at AS critical_issued_at,
			c.case_sla_breach_detected_at AS breach_detected_at,
			c.case_sla_breach_action AS breach_action,
			c.case_sla_calendar_id::text AS calendar_id,
			c.assigned_to,
			c.case_sla_cycle,
			c.completed_at,
			c.created_at
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.status IN ('OPEN', 'IN_PROGRESS')
		  AND c.case_due_at IS NOT NULL
		ORDER BY c.case_due_at ASC
		LIMIT $1
		FOR UPDATE OF c SKIP LOCKED
	`, j.batchSize)
	if err != nil {
		return fmt.Errorf("processCaseBatch: query candidates: %w", err)
	}

	for _, c := range rows {
		if !c.CalendarID.Valid || c.DurationMS <= 0 {
			continue
		}
		elapsed, err := BusinessHoursElapsed(ctx, j.db, c.EffectiveStartTime, now, c.CalendarID.String)
		if err != nil {
			j.logger.Error("case elapsed calc failed", "case_id", c.ID, "error", err)
			continue
		}

		duration := time.Duration(c.DurationMS) * time.Millisecond
		elapsedPct := float64(elapsed) / float64(duration) * 100
		warnPct := nullableFloatOr(c.WarningThresholdPct, 80)
		criticalPct := nullableFloatOr(c.CriticalThresholdPct, 95)
		action := model.SLABreachAction(nullableStringOr(c.BreachAction, string(model.SLABreachActionNotifyOnly)))

		if !c.WarningIssuedAt.Valid && elapsedPct >= warnPct && elapsedPct < criticalPct {
			if err := j.issueCaseWarning(ctx, tx, c, warnPct); err != nil {
				j.logger.Error("case warning failed", "case_id", c.ID, "error", err)
			}
		}
		if !c.CriticalIssuedAt.Valid && !c.BreachDetectedAt.Valid && elapsedPct >= criticalPct {
			if err := j.issueCaseCritical(ctx, tx, c, criticalPct, action); err != nil {
				j.logger.Error("case critical failed", "case_id", c.ID, "error", err)
			}
		}
		if now.After(c.DueAt) && !c.BreachDetectedAt.Valid {
			if err := j.breachCase(ctx, tx, c, elapsed, duration, action); err != nil {
				j.logger.Error("case breach failed", "case_id", c.ID, "error", err)
			}
		}
	}

	return nil
}

func (j *SLASweepJob) issueTaskWarning(ctx context.Context, tx *sqlx.Tx, c taskSweepCandidate, threshold float64) error {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET sla_warning_issued_at = $1,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid
		  AND sla_warning_issued_at IS NULL
	`, now, c.ID)
	if err != nil {
		return fmt.Errorf("issueTaskWarning: update: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	payload := SLAEventPayload{EntityType: model.SLAEntityTypeTask, EntityID: c.ID, CaseID: &c.CaseID, TaskID: &c.ID, ThresholdPercent: &threshold}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("issueTaskWarning: marshal: %w", err)
	}
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{CaseID: &c.CaseID, TaskID: &c.ID, EventType: EventTypeSLAWarning, Payload: raw, Status: model.EventStatusPending}); err != nil {
		return fmt.Errorf("issueTaskWarning: publish: %w", err)
	}
	return nil
}

func (j *SLASweepJob) issueTaskCritical(ctx context.Context, tx *sqlx.Tx, c taskSweepCandidate, threshold float64, action model.SLABreachAction) error {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET sla_critical_issued_at = $1,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid
		  AND sla_critical_issued_at IS NULL
	`, now, c.ID)
	if err != nil {
		return fmt.Errorf("issueTaskCritical: update: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	if action == model.SLABreachActionEscalateToSupervisor {
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET priority = CASE WHEN priority < 4 THEN 4 ELSE priority END,
			    updated_at = now(),
			    version = version + 1
			WHERE id = $1::uuid
		`, c.ID); err != nil {
			return fmt.Errorf("issueTaskCritical: escalate priority: %w", err)
		}
	}

	payload := SLAEventPayload{EntityType: model.SLAEntityTypeTask, EntityID: c.ID, CaseID: &c.CaseID, TaskID: &c.ID, ThresholdPercent: &threshold, Action: &action}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("issueTaskCritical: marshal: %w", err)
	}
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{CaseID: &c.CaseID, TaskID: &c.ID, EventType: EventTypeSLACritical, Payload: raw, Status: model.EventStatusPending}); err != nil {
		return fmt.Errorf("issueTaskCritical: publish: %w", err)
	}
	return nil
}

func (j *SLASweepJob) breachTask(ctx context.Context, tx *sqlx.Tx, c taskSweepCandidate, elapsed time.Duration, duration time.Duration, action model.SLABreachAction) error {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET sla_breach_detected_at = $1,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $2::uuid
		  AND sla_breach_detected_at IS NULL
	`, now, c.ID)
	if err != nil {
		return fmt.Errorf("breachTask: update breach marker: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	severity := computeBreachSeverity(elapsed, duration)
	elapsedMinutes := int(elapsed / time.Minute)
	var assignee *string
	if c.AssigneeID.Valid {
		assignee = &c.AssigneeID.String
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sla_breach_log (
			entity_type,
			entity_id,
			breach_detected_at,
			original_due_at,
			assignee_at_breach,
			elapsed_time_minutes,
			breach_severity,
			breach_action_taken,
			sla_cycle,
			task_id,
			case_id
		) VALUES (
			'TASK',
			$1::uuid,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$1::uuid,
			$9::uuid
		)
		ON CONFLICT (entity_type, entity_id, sla_cycle) WHERE entity_id IS NOT NULL DO NOTHING
	`, c.ID, now, c.DueAt, assignee, elapsedMinutes, string(severity), string(action), c.SLACycle, c.CaseID)
	if err != nil {
		return fmt.Errorf("breachTask: insert breach log: %w", err)
	}

	if err := j.executeTaskBreachAction(ctx, tx, c, action); err != nil {
		j.logger.Error("task breach action failed", "task_id", c.ID, "action", action, "error", err)
	}

	payload := SLAEventPayload{EntityType: model.SLAEntityTypeTask, EntityID: c.ID, CaseID: &c.CaseID, TaskID: &c.ID, BreachSeverity: &severity, Action: &action}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("breachTask: marshal payload: %w", err)
	}
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{CaseID: &c.CaseID, TaskID: &c.ID, EventType: EventTypeSLABreached, Payload: raw, Status: model.EventStatusPending}); err != nil {
		return fmt.Errorf("breachTask: publish event: %w", err)
	}
	return nil
}

func (j *SLASweepJob) issueCaseWarning(ctx context.Context, tx *sqlx.Tx, c caseSweepCandidate, threshold float64) error {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET case_sla_warning_issued_at = $1,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $2::uuid
		  AND case_sla_warning_issued_at IS NULL
	`, now, c.ID)
	if err != nil {
		return fmt.Errorf("issueCaseWarning: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	payload := SLAEventPayload{EntityType: model.SLAEntityTypeCase, EntityID: c.ID, CaseID: &c.ID, ThresholdPercent: &threshold}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("issueCaseWarning: marshal: %w", err)
	}
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{CaseID: &c.ID, EventType: EventTypeSLAWarning, Payload: raw, Status: model.EventStatusPending}); err != nil {
		return fmt.Errorf("issueCaseWarning: publish: %w", err)
	}
	return nil
}

func (j *SLASweepJob) issueCaseCritical(ctx context.Context, tx *sqlx.Tx, c caseSweepCandidate, threshold float64, action model.SLABreachAction) error {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET case_sla_critical_issued_at = $1,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $2::uuid
		  AND case_sla_critical_issued_at IS NULL
	`, now, c.ID)
	if err != nil {
		return fmt.Errorf("issueCaseCritical: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	payload := SLAEventPayload{EntityType: model.SLAEntityTypeCase, EntityID: c.ID, CaseID: &c.ID, ThresholdPercent: &threshold, Action: &action}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("issueCaseCritical: marshal: %w", err)
	}
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{CaseID: &c.ID, EventType: EventTypeSLACritical, Payload: raw, Status: model.EventStatusPending}); err != nil {
		return fmt.Errorf("issueCaseCritical: publish: %w", err)
	}
	return nil
}

func (j *SLASweepJob) breachCase(ctx context.Context, tx *sqlx.Tx, c caseSweepCandidate, elapsed time.Duration, duration time.Duration, action model.SLABreachAction) error {
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET case_sla_breach_detected_at = $1,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $2::uuid
		  AND case_sla_breach_detected_at IS NULL
	`, now, c.ID)
	if err != nil {
		return fmt.Errorf("breachCase: update marker: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return nil
	}

	severity := computeBreachSeverity(elapsed, duration)
	elapsedMinutes := int(elapsed / time.Minute)
	var assignee *string
	if c.AssignedTo.Valid {
		assignee = &c.AssignedTo.String
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sla_breach_log (
			entity_type,
			entity_id,
			breach_detected_at,
			original_due_at,
			assignee_at_breach,
			elapsed_time_minutes,
			breach_severity,
			breach_action_taken,
			sla_cycle,
			case_id
		) VALUES (
			'CASE',
			$1::uuid,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$1::uuid
		)
		ON CONFLICT (entity_type, entity_id, sla_cycle) WHERE entity_id IS NOT NULL DO NOTHING
	`, c.ID, now, c.DueAt, assignee, elapsedMinutes, string(severity), string(action), c.SLACycle)
	if err != nil {
		return fmt.Errorf("breachCase: insert breach log: %w", err)
	}

	if action == model.SLABreachActionCreateExceptionCase {
		if err := j.createExceptionCase(ctx, tx, c.ID, severity); err != nil {
			j.logger.Error("create exception case failed", "case_id", c.ID, "error", err)
		}
	}

	payload := SLAEventPayload{EntityType: model.SLAEntityTypeCase, EntityID: c.ID, CaseID: &c.ID, BreachSeverity: &severity, Action: &action}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("breachCase: marshal payload: %w", err)
	}
	if err := j.eventPublisher.PublishEvent(ctx, tx, model.Event{CaseID: &c.ID, EventType: EventTypeSLABreached, Payload: raw, Status: model.EventStatusPending}); err != nil {
		return fmt.Errorf("breachCase: publish event: %w", err)
	}

	return nil
}

func (j *SLASweepJob) executeTaskBreachAction(ctx context.Context, tx *sqlx.Tx, c taskSweepCandidate, action model.SLABreachAction) error {
	switch action {
	case model.SLABreachActionEscalateToSupervisor:
		_, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET priority = 4,
			    workbasket_id = (SELECT id FROM workbaskets WHERE type = 'ESCALATION' LIMIT 1),
			    updated_at = now(),
			    version = version + 1
			WHERE id = $1::uuid
		`, c.ID)
		if err != nil {
			return fmt.Errorf("executeTaskBreachAction: escalate: %w", err)
		}
		return nil

	case model.SLABreachActionAutoReassign:
		_, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET assignee_id = NULL,
			    assigned_service = NULL,
			    status = 'PENDING',
			    updated_at = now(),
			    version = version + 1
			WHERE id = $1::uuid
		`, c.ID)
		if err != nil {
			return fmt.Errorf("executeTaskBreachAction: auto reassign: %w", err)
		}
		return nil

	case model.SLABreachActionCreateExceptionCase:
		return j.createExceptionCase(ctx, tx, c.CaseID, model.SLABreachSeverityCritical)

	case model.SLABreachActionNotifyOnly:
		return nil

	default:
		return fmt.Errorf("executeTaskBreachAction: unsupported action %s", action)
	}
}

func (j *SLASweepJob) createExceptionCase(ctx context.Context, tx *sqlx.Tx, parentCaseID string, severity model.SLABreachSeverity) error {
	var caseTypeID string
	var caseTypeVersion int
	err := tx.QueryRowContext(ctx, `
		SELECT id::text, version
		FROM case_types
		WHERE code = 'EXCEPTION_HANDLING'
		  AND status = 'ACTIVE'
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&caseTypeID, &caseTypeVersion)
	if err != nil {
		return fmt.Errorf("createExceptionCase: find EXCEPTION_HANDLING type: %w", err)
	}

	payload := map[string]any{
		"parent_case_id": parentCaseID,
		"reason":         "SLA_BREACH",
		"severity":       string(severity),
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("createExceptionCase: marshal metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO cases (
			case_type_id,
			case_type_version,
			parent_case_id,
			status,
			metadata,
			current_stage_ordinal
		) VALUES (
			$1::uuid,
			$2,
			$3::uuid,
			'OPEN',
			$4::jsonb,
			1
		)
	`, caseTypeID, caseTypeVersion, parentCaseID, rawPayload)
	if err != nil {
		return fmt.Errorf("createExceptionCase: insert case: %w", err)
	}
	return nil
}

func (j *SLASweepJob) flushMetricsSummary(ctx context.Context, tx *sqlx.Tx, agg map[metricsKey]*metricsAccumulator) error {
	for key, bucket := range agg {
		sort.Ints(bucket.ElapsedMinutes)
		avg := 0.0
		if len(bucket.ElapsedMinutes) > 0 {
			total := int64(0)
			for _, m := range bucket.ElapsedMinutes {
				total += int64(m)
			}
			avg = float64(total) / float64(len(bucket.ElapsedMinutes))
		}
		p50 := percentile(bucket.ElapsedMinutes, 50)
		p95 := percentile(bucket.ElapsedMinutes, 95)
		p99 := percentile(bucket.ElapsedMinutes, 99)

		_, err := tx.ExecContext(ctx, `
			INSERT INTO sla_metrics_summary (
				metric_date,
				case_type_code,
				stage_code,
				activity_code,
				task_definition_code,
				total_count,
				completed_count,
				breached_count,
				avg_elapsed_minutes,
				p50_elapsed_minutes,
				p95_elapsed_minutes,
				p99_elapsed_minutes,
				total_pause_minutes,
				updated_at
			) VALUES (
				$1,
				$2,
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
				now()
			)
			ON CONFLICT (metric_date, case_type_code, stage_code, activity_code, task_definition_code)
			DO UPDATE SET
				total_count = sla_metrics_summary.total_count + EXCLUDED.total_count,
				completed_count = sla_metrics_summary.completed_count + EXCLUDED.completed_count,
				breached_count = sla_metrics_summary.breached_count + EXCLUDED.breached_count,
				avg_elapsed_minutes = EXCLUDED.avg_elapsed_minutes,
				p50_elapsed_minutes = EXCLUDED.p50_elapsed_minutes,
				p95_elapsed_minutes = EXCLUDED.p95_elapsed_minutes,
				p99_elapsed_minutes = EXCLUDED.p99_elapsed_minutes,
				total_pause_minutes = sla_metrics_summary.total_pause_minutes + EXCLUDED.total_pause_minutes,
				updated_at = now()
		`, key.MetricDate, key.CaseTypeCode, key.StageCode, key.ActivityCode, key.TaskDefinitionCode, bucket.TotalCount, bucket.CompletedCount, bucket.BreachedCount, avg, p50, p95, p99, bucket.TotalPauseMins)
		if err != nil {
			return fmt.Errorf("flushMetricsSummary: upsert %v: %w", key, err)
		}
	}
	return nil
}

func totalPauseMinutes(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string) (int64, error) {
	var mins sql.NullInt64
	err := tx.GetContext(ctx, &mins, `
		SELECT COALESCE(SUM(elapsed_before_pause_ms) / 60000, 0)
		FROM sla_pause_log
		WHERE entity_type = $1
		  AND entity_id = $2::uuid
		  AND action = 'PAUSE'
	`, string(entityType), entityID)
	if err != nil {
		return 0, fmt.Errorf("totalPauseMinutes: %w", err)
	}
	if !mins.Valid {
		return 0, nil
	}
	return mins.Int64, nil
}

func computeBreachSeverity(elapsed, target time.Duration) model.SLABreachSeverity {
	if target <= 0 {
		return model.SLABreachSeverityCritical
	}
	overrunPct := (float64(elapsed-target) / float64(target)) * 100
	if overrunPct < 10 {
		return model.SLABreachSeverityMinor
	}
	if overrunPct < 30 {
		return model.SLABreachSeverityModerate
	}
	if overrunPct < 50 {
		return model.SLABreachSeverityMajor
	}
	return model.SLABreachSeverityCritical
}

func percentile(sorted []int, p int) int {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (float64(p) / 100) * float64(len(sorted)-1)
	idx := int(math.Round(rank))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func nullableFloatOr(v sql.NullFloat64, fallback float64) float64 {
	if v.Valid {
		return v.Float64
	}
	return fallback
}

func nullableStringOr(v sql.NullString, fallback string) string {
	if v.Valid {
		return v.String
	}
	return fallback
}

func isTerminalTaskStatus(status string) bool {
	switch status {
	case "COMPLETED", "CANCELLED", "SKIPPED", "FAILED":
		return true
	default:
		return false
	}
}
