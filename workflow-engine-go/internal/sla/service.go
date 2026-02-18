package sla

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// Actor describes the caller identity used by SLA authorization guards.
type Actor struct {
	ID           string
	IsSupervisor bool
	IsSystem     bool
}

// SLAEntity represents the current SLA runtime state for transition validation.
type SLAEntity struct {
	EntityType         model.SLAEntityType
	EntityID           string
	State              model.SLAState
	IsBreached         bool
	ExtensionDuration  time.Duration
}

// PauseSLARequest captures a pause operation input.
type PauseSLARequest struct {
	EntityType model.SLAEntityType
	EntityID   string
	Reason     string
	Actor      Actor
}

// ResumeSLARequest captures a resume operation input.
type ResumeSLARequest struct {
	EntityType model.SLAEntityType
	EntityID   string
	Reason     string
	Actor      Actor
}

// ResetSLARequest captures an SLA reset operation.
type ResetSLARequest struct {
	EntityType       model.SLAEntityType
	EntityID         string
	Reason           string
	ApprovedBy       string
	NewDurationHours *float64
	Actor            Actor
}

// ExtendSLARequest captures an SLA extension operation.
type ExtendSLARequest struct {
	EntityType              model.SLAEntityType
	EntityID                string
	ExtensionDurationHours  float64
	Reason                  string
	ApprovedBy              string
	Actor                   Actor
}

type slaEntitySnapshot struct {
	EntityType             model.SLAEntityType
	EntityID               string
	CaseID                 *string
	TaskID                 *string
	Status                 string
	DueAt                  *time.Time
	EffectiveStartTime     *time.Time
	CreatedAt              time.Time
	DurationMS             *int64
	WarningThresholdPct    *float64
	CriticalThresholdPct   *float64
	BreachAction           *model.SLABreachAction
	CalendarID             *string
	BreachDetectedAt       *time.Time
	SLACycle               int
}

type pauseMarker struct {
	PausedAt             time.Time `db:"paused_at"`
	ElapsedBeforePauseMS int64     `db:"elapsed_before_pause_ms"`
}

// ValidateSLAOperation enforces the SLA lifecycle rules.
func ValidateSLAOperation(
	ctx context.Context,
	operation model.SLAOperation,
	entity SLAEntity,
	actor Actor,
) error {
	_ = ctx
	if entity.EntityID == "" {
		return fmt.Errorf("ValidateSLAOperation: entity id is required")
	}

	switch operation {
	case model.SLAOperationPause:
		if entity.State == model.SLAStatePaused {
			return fmt.Errorf("ValidateSLAOperation: entity %s already paused", entity.EntityID)
		}
		if entity.State == model.SLAStateBreached || entity.IsBreached {
			return fmt.Errorf("ValidateSLAOperation: breached entity %s cannot be paused", entity.EntityID)
		}
		return nil

	case model.SLAOperationResume:
		if entity.State != model.SLAStatePaused {
			return fmt.Errorf("ValidateSLAOperation: entity %s is not paused", entity.EntityID)
		}
		if entity.State == model.SLAStateBreached || entity.IsBreached {
			return fmt.Errorf("ValidateSLAOperation: breached entity %s cannot resume", entity.EntityID)
		}
		return nil

	case model.SLAOperationReset:
		if !actor.IsSupervisor {
			return fmt.Errorf("ValidateSLAOperation: reset requires supervisor")
		}
		if entity.State == model.SLAStateBreached || entity.IsBreached {
			return nil
		}
		if entity.State == model.SLAStateActive || entity.State == model.SLAStatePaused {
			return nil
		}
		return fmt.Errorf("ValidateSLAOperation: reset not allowed for state %s", entity.State)

	case model.SLAOperationExtend:
		if !actor.IsSupervisor {
			return fmt.Errorf("ValidateSLAOperation: extension requires supervisor")
		}
		if entity.State == model.SLAStateBreached || entity.IsBreached {
			return fmt.Errorf("ValidateSLAOperation: breached entity %s must be reset, not extended", entity.EntityID)
		}
		if entity.ExtensionDuration <= 0 {
			return fmt.Errorf("ValidateSLAOperation: extension duration must be positive")
		}
		if entity.State != model.SLAStateActive {
			return fmt.Errorf("ValidateSLAOperation: extension allowed only for ACTIVE SLA")
		}
		return nil

	case model.SLAOperationBreach:
		if !actor.IsSystem {
			return fmt.Errorf("ValidateSLAOperation: breach transition is system-only")
		}
		return nil

	default:
		return fmt.Errorf("ValidateSLAOperation: unsupported operation %s", operation)
	}
}

// PauseSLA appends a pause marker and emits SLA_PAUSED.
func PauseSLA(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, req PauseSLARequest, publisher EventPublisher) error {
	if db == nil {
		return fmt.Errorf("PauseSLA: db is nil")
	}
	if tx == nil {
		return fmt.Errorf("PauseSLA: tx is nil")
	}
	if req.Reason == "" {
		return fmt.Errorf("PauseSLA: reason is required")
	}

	snapshot, err := loadEntitySnapshot(ctx, tx, req.EntityType, req.EntityID)
	if err != nil {
		return fmt.Errorf("PauseSLA: %w", err)
	}

	state, err := currentEntityState(ctx, tx, req.EntityType, req.EntityID, snapshot.BreachDetectedAt != nil)
	if err != nil {
		return fmt.Errorf("PauseSLA: %w", err)
	}

	if err := ValidateSLAOperation(ctx, model.SLAOperationPause, SLAEntity{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		State:      state,
		IsBreached: snapshot.BreachDetectedAt != nil,
	}, req.Actor); err != nil {
		return fmt.Errorf("PauseSLA: %w", err)
	}

	if snapshot.CalendarID == nil || *snapshot.CalendarID == "" {
		return fmt.Errorf("PauseSLA: missing calendar id on entity %s", req.EntityID)
	}

	start := snapshot.CreatedAt
	if snapshot.EffectiveStartTime != nil {
		start = *snapshot.EffectiveStartTime
	}
	now := time.Now().UTC()
	elapsed, err := BusinessHoursElapsed(ctx, db, start, now, *snapshot.CalendarID)
	if err != nil {
		return fmt.Errorf("PauseSLA: compute elapsed business time: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sla_pause_log (
			entity_type,
			entity_id,
			paused_at,
			resumed_at,
			pause_reason,
			elapsed_before_pause_ms,
			action
		) VALUES (
			$1,
			$2::uuid,
			$3,
			NULL,
			$4,
			$5,
			'PAUSE'
		)
	`, string(req.EntityType), req.EntityID, now, req.Reason, elapsed.Milliseconds())
	if err != nil {
		return fmt.Errorf("PauseSLA: insert pause log: %w", err)
	}

	payloadBytes, err := json.Marshal(SLAEventPayload{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		CaseID:     snapshot.CaseID,
		TaskID:     snapshot.TaskID,
		Reason:     &req.Reason,
	})
	if err != nil {
		return fmt.Errorf("PauseSLA: marshal event payload: %w", err)
	}

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    snapshot.CaseID,
		TaskID:    snapshot.TaskID,
		EventType: EventTypeSLAPaused,
		Payload:   payloadBytes,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("PauseSLA: publish event: %w", err)
	}

	return nil
}

// ResumeSLA appends a resume marker and recomputes due_at from remaining duration.
func ResumeSLA(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, req ResumeSLARequest, publisher EventPublisher) error {
	if db == nil {
		return fmt.Errorf("ResumeSLA: db is nil")
	}
	if tx == nil {
		return fmt.Errorf("ResumeSLA: tx is nil")
	}

	snapshot, err := loadEntitySnapshot(ctx, tx, req.EntityType, req.EntityID)
	if err != nil {
		return fmt.Errorf("ResumeSLA: %w", err)
	}
	if snapshot.DurationMS == nil || *snapshot.DurationMS <= 0 {
		return fmt.Errorf("ResumeSLA: missing positive duration for entity %s", req.EntityID)
	}
	if snapshot.CalendarID == nil || *snapshot.CalendarID == "" {
		return fmt.Errorf("ResumeSLA: missing calendar id for entity %s", req.EntityID)
	}

	state, err := currentEntityState(ctx, tx, req.EntityType, req.EntityID, snapshot.BreachDetectedAt != nil)
	if err != nil {
		return fmt.Errorf("ResumeSLA: %w", err)
	}

	if err := ValidateSLAOperation(ctx, model.SLAOperationResume, SLAEntity{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		State:      state,
		IsBreached: snapshot.BreachDetectedAt != nil,
	}, req.Actor); err != nil {
		return fmt.Errorf("ResumeSLA: %w", err)
	}

	lastPause, err := latestPauseMarker(ctx, tx, req.EntityType, req.EntityID)
	if err != nil {
		return fmt.Errorf("ResumeSLA: %w", err)
	}

	remaining := time.Duration(*snapshot.DurationMS-lastPause.ElapsedBeforePauseMS) * time.Millisecond
	if remaining < 0 {
		remaining = 0
	}

	now := time.Now().UTC()
	newDueAt, err := AddBusinessHours(ctx, db, now, remaining, *snapshot.CalendarID)
	if err != nil {
		return fmt.Errorf("ResumeSLA: recompute due_at: %w", err)
	}

	if err := updateEntitySLAOnResume(ctx, tx, req.EntityType, req.EntityID, now, newDueAt); err != nil {
		return fmt.Errorf("ResumeSLA: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sla_pause_log (
			entity_type,
			entity_id,
			paused_at,
			resumed_at,
			pause_reason,
			elapsed_before_pause_ms,
			action
		) VALUES (
			$1,
			$2::uuid,
			$3,
			$4,
			$5,
			$6,
			'RESUME'
		)
	`, string(req.EntityType), req.EntityID, lastPause.PausedAt, now, req.Reason, lastPause.ElapsedBeforePauseMS)
	if err != nil {
		return fmt.Errorf("ResumeSLA: insert resume log: %w", err)
	}

	payloadBytes, err := json.Marshal(SLAEventPayload{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		CaseID:     snapshot.CaseID,
		TaskID:     snapshot.TaskID,
		Reason:     &req.Reason,
	})
	if err != nil {
		return fmt.Errorf("ResumeSLA: marshal event payload: %w", err)
	}

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    snapshot.CaseID,
		TaskID:    snapshot.TaskID,
		EventType: EventTypeSLAResumed,
		Payload:   payloadBytes,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("ResumeSLA: publish event: %w", err)
	}

	return nil
}

// ResetSLA clears issued threshold markers and starts a new SLA cycle.
func ResetSLA(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, req ResetSLARequest, publisher EventPublisher) error {
	if db == nil {
		return fmt.Errorf("ResetSLA: db is nil")
	}
	if tx == nil {
		return fmt.Errorf("ResetSLA: tx is nil")
	}
	if req.Reason == "" {
		return fmt.Errorf("ResetSLA: reason is required")
	}
	if req.ApprovedBy == "" {
		return fmt.Errorf("ResetSLA: approved_by is required")
	}

	snapshot, err := loadEntitySnapshot(ctx, tx, req.EntityType, req.EntityID)
	if err != nil {
		return fmt.Errorf("ResetSLA: %w", err)
	}
	if snapshot.CalendarID == nil || *snapshot.CalendarID == "" {
		return fmt.Errorf("ResetSLA: missing calendar id for entity %s", req.EntityID)
	}

	state, err := currentEntityState(ctx, tx, req.EntityType, req.EntityID, snapshot.BreachDetectedAt != nil)
	if err != nil {
		return fmt.Errorf("ResetSLA: %w", err)
	}

	if err := ValidateSLAOperation(ctx, model.SLAOperationReset, SLAEntity{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		State:      state,
		IsBreached: snapshot.BreachDetectedAt != nil,
	}, req.Actor); err != nil {
		return fmt.Errorf("ResetSLA: %w", err)
	}

	if err := cancelPendingThresholdEvents(ctx, tx, req.EntityType, req.EntityID); err != nil {
		return fmt.Errorf("ResetSLA: %w", err)
	}

	var durationMS int64
	if req.NewDurationHours != nil {
		if *req.NewDurationHours <= 0 {
			return fmt.Errorf("ResetSLA: new duration must be positive")
		}
		durationMS, err = hoursToMilliseconds(*req.NewDurationHours)
		if err != nil {
			return fmt.Errorf("ResetSLA: %w", err)
		}
	} else if snapshot.DurationMS != nil {
		durationMS = *snapshot.DurationMS
	} else {
		return fmt.Errorf("ResetSLA: missing duration; pass new_duration_hours")
	}

	now := time.Now().UTC()
	newDueAt, err := AddBusinessHours(ctx, db, now, time.Duration(durationMS)*time.Millisecond, *snapshot.CalendarID)
	if err != nil {
		return fmt.Errorf("ResetSLA: recompute due_at: %w", err)
	}

	if err := updateEntitySLAOnReset(ctx, tx, req.EntityType, req.EntityID, now, newDueAt, durationMS); err != nil {
		return fmt.Errorf("ResetSLA: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sla_reset_log (
			entity_type,
			entity_id,
			reset_at,
			previous_due_at,
			new_due_at,
			new_duration_hours,
			reason,
			approved_by
		) VALUES (
			$1,
			$2::uuid,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8
		)
	`, string(req.EntityType), req.EntityID, now, snapshot.DueAt, newDueAt, float64(durationMS)/float64(time.Hour/time.Millisecond), req.Reason, req.ApprovedBy)
	if err != nil {
		return fmt.Errorf("ResetSLA: insert reset log: %w", err)
	}

	payloadBytes, err := json.Marshal(SLAEventPayload{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		CaseID:     snapshot.CaseID,
		TaskID:     snapshot.TaskID,
		Reason:     &req.Reason,
		ApprovedBy: &req.ApprovedBy,
	})
	if err != nil {
		return fmt.Errorf("ResetSLA: marshal event payload: %w", err)
	}

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    snapshot.CaseID,
		TaskID:    snapshot.TaskID,
		EventType: EventTypeSLAReset,
		Payload:   payloadBytes,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("ResetSLA: publish event: %w", err)
	}

	return nil
}

// ExtendSLA adds more business duration on top of the existing due_at.
func ExtendSLA(ctx context.Context, db *sqlx.DB, tx *sqlx.Tx, req ExtendSLARequest, publisher EventPublisher) error {
	if db == nil {
		return fmt.Errorf("ExtendSLA: db is nil")
	}
	if tx == nil {
		return fmt.Errorf("ExtendSLA: tx is nil")
	}
	if req.Reason == "" {
		return fmt.Errorf("ExtendSLA: reason is required")
	}
	if req.ApprovedBy == "" {
		return fmt.Errorf("ExtendSLA: approved_by is required")
	}

	extensionDurationMS, err := hoursToMilliseconds(req.ExtensionDurationHours)
	if err != nil {
		return fmt.Errorf("ExtendSLA: %w", err)
	}

	snapshot, err := loadEntitySnapshot(ctx, tx, req.EntityType, req.EntityID)
	if err != nil {
		return fmt.Errorf("ExtendSLA: %w", err)
	}
	if snapshot.CalendarID == nil || *snapshot.CalendarID == "" {
		return fmt.Errorf("ExtendSLA: missing calendar id for entity %s", req.EntityID)
	}
	if snapshot.DueAt == nil {
		return fmt.Errorf("ExtendSLA: due_at is missing for entity %s", req.EntityID)
	}

	state, err := currentEntityState(ctx, tx, req.EntityType, req.EntityID, snapshot.BreachDetectedAt != nil)
	if err != nil {
		return fmt.Errorf("ExtendSLA: %w", err)
	}

	if err := ValidateSLAOperation(ctx, model.SLAOperationExtend, SLAEntity{
		EntityType:        req.EntityType,
		EntityID:          req.EntityID,
		State:             state,
		IsBreached:        snapshot.BreachDetectedAt != nil,
		ExtensionDuration: time.Duration(extensionDurationMS) * time.Millisecond,
	}, req.Actor); err != nil {
		return fmt.Errorf("ExtendSLA: %w", err)
	}

	if err := cancelPendingThresholdEvents(ctx, tx, req.EntityType, req.EntityID); err != nil {
		return fmt.Errorf("ExtendSLA: %w", err)
	}

	newDueAt, err := AddBusinessHours(ctx, db, snapshot.DueAt.UTC(), time.Duration(extensionDurationMS)*time.Millisecond, *snapshot.CalendarID)
	if err != nil {
		return fmt.Errorf("ExtendSLA: recompute due_at: %w", err)
	}

	if err := updateEntitySLAOnExtend(ctx, tx, req.EntityType, req.EntityID, newDueAt, extensionDurationMS); err != nil {
		return fmt.Errorf("ExtendSLA: %w", err)
	}

	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sla_extension_log (
			entity_type,
			entity_id,
			extended_at,
			previous_due_at,
			new_due_at,
			extension_duration_hours,
			reason,
			approved_by
		) VALUES (
			$1,
			$2::uuid,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8
		)
	`, string(req.EntityType), req.EntityID, now, snapshot.DueAt, newDueAt, req.ExtensionDurationHours, req.Reason, req.ApprovedBy)
	if err != nil {
		return fmt.Errorf("ExtendSLA: insert extension log: %w", err)
	}

	payloadBytes, err := json.Marshal(SLAEventPayload{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		CaseID:     snapshot.CaseID,
		TaskID:     snapshot.TaskID,
		Reason:     &req.Reason,
		ApprovedBy: &req.ApprovedBy,
	})
	if err != nil {
		return fmt.Errorf("ExtendSLA: marshal event payload: %w", err)
	}

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    snapshot.CaseID,
		TaskID:    snapshot.TaskID,
		EventType: EventTypeSLAExtended,
		Payload:   payloadBytes,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("ExtendSLA: publish event: %w", err)
	}

	return nil
}

func loadEntitySnapshot(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string) (slaEntitySnapshot, error) {
	if entityID == "" {
		return slaEntitySnapshot{}, fmt.Errorf("loadEntitySnapshot: entity id is required")
	}

	switch entityType {
	case model.SLAEntityTypeTask:
		var row struct {
			ID                     string          `db:"id"`
			CaseID                 string          `db:"case_id"`
			Status                 string          `db:"status"`
			TaskDueAt              sql.NullTime    `db:"task_due_at"`
			DueAt                  sql.NullTime    `db:"due_at"`
			EffectiveStartTime     sql.NullTime    `db:"effective_start_time"`
			CreatedAt              time.Time       `db:"created_at"`
			DurationMS             sql.NullInt64   `db:"sla_duration_ms"`
			WarningThresholdPct    sql.NullFloat64 `db:"sla_warning_threshold_pct"`
			CriticalThresholdPct   sql.NullFloat64 `db:"sla_critical_threshold_pct"`
			BreachAction           sql.NullString  `db:"sla_breach_action"`
			CalendarID             sql.NullString  `db:"sla_calendar_id"`
			BreachDetectedAt       sql.NullTime    `db:"sla_breach_detected_at"`
			SLACycle               int             `db:"sla_cycle"`
		}

		err := tx.GetContext(ctx, &row, `
			SELECT
				id,
				case_id::text,
				status,
				task_due_at,
				due_at,
				effective_start_time,
				created_at,
				sla_duration_ms,
				sla_warning_threshold_pct,
				sla_critical_threshold_pct,
				sla_breach_action,
				sla_calendar_id::text AS sla_calendar_id,
				sla_breach_detected_at,
				sla_cycle
			FROM tasks
			WHERE id = $1::uuid
		`, entityID)
		if err != nil {
			return slaEntitySnapshot{}, fmt.Errorf("loadEntitySnapshot: task %s: %w", entityID, err)
		}

		caseID := row.CaseID
		var dueAt *time.Time
		if row.TaskDueAt.Valid {
			t := row.TaskDueAt.Time.UTC()
			dueAt = &t
		} else if row.DueAt.Valid {
			t := row.DueAt.Time.UTC()
			dueAt = &t
		}
		var effectiveStart *time.Time
		if row.EffectiveStartTime.Valid {
			t := row.EffectiveStartTime.Time.UTC()
			effectiveStart = &t
		}
		var durationMS *int64
		if row.DurationMS.Valid {
			v := row.DurationMS.Int64
			durationMS = &v
		}
		var warningPct *float64
		if row.WarningThresholdPct.Valid {
			v := row.WarningThresholdPct.Float64
			warningPct = &v
		}
		var criticalPct *float64
		if row.CriticalThresholdPct.Valid {
			v := row.CriticalThresholdPct.Float64
			criticalPct = &v
		}
		var action *model.SLABreachAction
		if row.BreachAction.Valid {
			v := model.SLABreachAction(row.BreachAction.String)
			action = &v
		}
		var calendarID *string
		if row.CalendarID.Valid {
			v := row.CalendarID.String
			calendarID = &v
		}
		var breachAt *time.Time
		if row.BreachDetectedAt.Valid {
			v := row.BreachDetectedAt.Time.UTC()
			breachAt = &v
		}

		return slaEntitySnapshot{
			EntityType:           entityType,
			EntityID:             entityID,
			CaseID:               &caseID,
			TaskID:               &row.ID,
			Status:               row.Status,
			DueAt:                dueAt,
			EffectiveStartTime:   effectiveStart,
			CreatedAt:            row.CreatedAt.UTC(),
			DurationMS:           durationMS,
			WarningThresholdPct:  warningPct,
			CriticalThresholdPct: criticalPct,
			BreachAction:         action,
			CalendarID:           calendarID,
			BreachDetectedAt:     breachAt,
			SLACycle:             row.SLACycle,
		}, nil

	case model.SLAEntityTypeCase:
		var row struct {
			ID                     string          `db:"id"`
			Status                 string          `db:"status"`
			CaseDueAt              sql.NullTime    `db:"case_due_at"`
			EffectiveStartTime     sql.NullTime    `db:"case_effective_start_time"`
			CreatedAt              time.Time       `db:"created_at"`
			DurationMS             sql.NullInt64   `db:"case_sla_duration_ms"`
			WarningThresholdPct    sql.NullFloat64 `db:"case_sla_warning_threshold_pct"`
			CriticalThresholdPct   sql.NullFloat64 `db:"case_sla_critical_threshold_pct"`
			BreachAction           sql.NullString  `db:"case_sla_breach_action"`
			CalendarID             sql.NullString  `db:"case_sla_calendar_id"`
			BreachDetectedAt       sql.NullTime    `db:"case_sla_breach_detected_at"`
			SLACycle               int             `db:"case_sla_cycle"`
		}

		err := tx.GetContext(ctx, &row, `
			SELECT
				id,
				status,
				case_due_at,
				case_effective_start_time,
				created_at,
				case_sla_duration_ms,
				case_sla_warning_threshold_pct,
				case_sla_critical_threshold_pct,
				case_sla_breach_action,
				case_sla_calendar_id::text AS case_sla_calendar_id,
				case_sla_breach_detected_at,
				case_sla_cycle
			FROM cases
			WHERE id = $1::uuid
		`, entityID)
		if err != nil {
			return slaEntitySnapshot{}, fmt.Errorf("loadEntitySnapshot: case %s: %w", entityID, err)
		}

		caseID := row.ID
		var dueAt *time.Time
		if row.CaseDueAt.Valid {
			t := row.CaseDueAt.Time.UTC()
			dueAt = &t
		}
		var effectiveStart *time.Time
		if row.EffectiveStartTime.Valid {
			t := row.EffectiveStartTime.Time.UTC()
			effectiveStart = &t
		}
		var durationMS *int64
		if row.DurationMS.Valid {
			v := row.DurationMS.Int64
			durationMS = &v
		}
		var warningPct *float64
		if row.WarningThresholdPct.Valid {
			v := row.WarningThresholdPct.Float64
			warningPct = &v
		}
		var criticalPct *float64
		if row.CriticalThresholdPct.Valid {
			v := row.CriticalThresholdPct.Float64
			criticalPct = &v
		}
		var action *model.SLABreachAction
		if row.BreachAction.Valid {
			v := model.SLABreachAction(row.BreachAction.String)
			action = &v
		}
		var calendarID *string
		if row.CalendarID.Valid {
			v := row.CalendarID.String
			calendarID = &v
		}
		var breachAt *time.Time
		if row.BreachDetectedAt.Valid {
			v := row.BreachDetectedAt.Time.UTC()
			breachAt = &v
		}

		return slaEntitySnapshot{
			EntityType:           entityType,
			EntityID:             row.ID,
			CaseID:               &caseID,
			TaskID:               nil,
			Status:               row.Status,
			DueAt:                dueAt,
			EffectiveStartTime:   effectiveStart,
			CreatedAt:            row.CreatedAt.UTC(),
			DurationMS:           durationMS,
			WarningThresholdPct:  warningPct,
			CriticalThresholdPct: criticalPct,
			BreachAction:         action,
			CalendarID:           calendarID,
			BreachDetectedAt:     breachAt,
			SLACycle:             row.SLACycle,
		}, nil

	default:
		return slaEntitySnapshot{}, fmt.Errorf("loadEntitySnapshot: entity type %s is not supported by runtime tables", entityType)
	}
}

func currentEntityState(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string, breached bool) (model.SLAState, error) {
	if breached {
		return model.SLAStateBreached, nil
	}

	var action sql.NullString
	err := tx.GetContext(ctx, &action, `
		SELECT action
		FROM sla_pause_log
		WHERE entity_type = $1
		  AND entity_id = $2::uuid
		ORDER BY created_at DESC
		LIMIT 1
	`, string(entityType), entityID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.SLAStateActive, nil
		}
		return "", fmt.Errorf("currentEntityState: load pause log: %w", err)
	}
	if action.Valid && action.String == "PAUSE" {
		return model.SLAStatePaused, nil
	}
	return model.SLAStateActive, nil
}

func latestPauseMarker(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string) (pauseMarker, error) {
	var marker pauseMarker
	err := tx.GetContext(ctx, &marker, `
		SELECT paused_at, elapsed_before_pause_ms
		FROM sla_pause_log
		WHERE entity_type = $1
		  AND entity_id = $2::uuid
		  AND action = 'PAUSE'
		ORDER BY created_at DESC
		LIMIT 1
	`, string(entityType), entityID)
	if err != nil {
		return pauseMarker{}, fmt.Errorf("latestPauseMarker: %w", err)
	}
	return marker, nil
}

func updateEntitySLAOnResume(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string, now time.Time, newDueAt time.Time) error {
	switch entityType {
	case model.SLAEntityTypeTask:
		_, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET effective_start_time = $1,
			    task_due_at = $2,
			    due_at = $2,
			    sla_warning_issued_at = NULL,
			    sla_critical_issued_at = NULL,
			    updated_at = now(),
			    version = version + 1
			WHERE id = $3::uuid
		`, now, newDueAt, entityID)
		if err != nil {
			return fmt.Errorf("updateEntitySLAOnResume: task: %w", err)
		}
		return nil
	case model.SLAEntityTypeCase:
		_, err := tx.ExecContext(ctx, `
			UPDATE cases
			SET case_effective_start_time = $1,
			    case_due_at = $2,
			    case_sla_warning_issued_at = NULL,
			    case_sla_critical_issued_at = NULL,
			    updated_at = now(),
			    row_version = row_version + 1
			WHERE id = $3::uuid
		`, now, newDueAt, entityID)
		if err != nil {
			return fmt.Errorf("updateEntitySLAOnResume: case: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("updateEntitySLAOnResume: unsupported entity type %s", entityType)
	}
}

func updateEntitySLAOnReset(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string, now time.Time, newDueAt time.Time, durationMS int64) error {
	switch entityType {
	case model.SLAEntityTypeTask:
		_, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET effective_start_time = $1,
			    task_due_at = $2,
			    due_at = $2,
			    sla_duration_ms = $3,
			    sla_warning_issued_at = NULL,
			    sla_critical_issued_at = NULL,
			    sla_breach_detected_at = NULL,
			    sla_cycle = sla_cycle + 1,
			    updated_at = now(),
			    version = version + 1
			WHERE id = $4::uuid
		`, now, newDueAt, durationMS, entityID)
		if err != nil {
			return fmt.Errorf("updateEntitySLAOnReset: task: %w", err)
		}
		return nil
	case model.SLAEntityTypeCase:
		_, err := tx.ExecContext(ctx, `
			UPDATE cases
			SET case_effective_start_time = $1,
			    case_due_at = $2,
			    case_sla_duration_ms = $3,
			    case_sla_warning_issued_at = NULL,
			    case_sla_critical_issued_at = NULL,
			    case_sla_breach_detected_at = NULL,
			    case_sla_cycle = case_sla_cycle + 1,
			    updated_at = now(),
			    row_version = row_version + 1
			WHERE id = $4::uuid
		`, now, newDueAt, durationMS, entityID)
		if err != nil {
			return fmt.Errorf("updateEntitySLAOnReset: case: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("updateEntitySLAOnReset: unsupported entity type %s", entityType)
	}
}

func updateEntitySLAOnExtend(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string, newDueAt time.Time, extensionMS int64) error {
	switch entityType {
	case model.SLAEntityTypeTask:
		_, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET task_due_at = $1,
			    due_at = $1,
			    sla_duration_ms = COALESCE(sla_duration_ms, 0) + $2,
			    sla_warning_issued_at = NULL,
			    sla_critical_issued_at = NULL,
			    updated_at = now(),
			    version = version + 1
			WHERE id = $3::uuid
		`, newDueAt, extensionMS, entityID)
		if err != nil {
			return fmt.Errorf("updateEntitySLAOnExtend: task: %w", err)
		}
		return nil
	case model.SLAEntityTypeCase:
		_, err := tx.ExecContext(ctx, `
			UPDATE cases
			SET case_due_at = $1,
			    case_sla_duration_ms = COALESCE(case_sla_duration_ms, 0) + $2,
			    case_sla_warning_issued_at = NULL,
			    case_sla_critical_issued_at = NULL,
			    updated_at = now(),
			    row_version = row_version + 1
			WHERE id = $3::uuid
		`, newDueAt, extensionMS, entityID)
		if err != nil {
			return fmt.Errorf("updateEntitySLAOnExtend: case: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("updateEntitySLAOnExtend: unsupported entity type %s", entityType)
	}
}

func cancelPendingThresholdEvents(ctx context.Context, tx *sqlx.Tx, entityType model.SLAEntityType, entityID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE events_outbox
		SET cancelled_at = now()
		WHERE event_type IN ('SLA_WARNING', 'SLA_CRITICAL')
		  AND status IN ('PENDING', 'PROCESSING')
		  AND cancelled_at IS NULL
		  AND payload->>'entity_type' = $1
		  AND payload->>'entity_id' = $2
	`, string(entityType), entityID)
	if err != nil {
		return fmt.Errorf("cancelPendingThresholdEvents: %w", err)
	}
	return nil
}

func hoursToMilliseconds(hours float64) (int64, error) {
	if hours <= 0 {
		return 0, fmt.Errorf("hoursToMilliseconds: duration must be > 0")
	}
	ms := hours * float64(time.Hour/time.Millisecond)
	if ms > float64(math.MaxInt64) {
		return 0, fmt.Errorf("hoursToMilliseconds: duration overflow")
	}
	return int64(ms), nil
}
