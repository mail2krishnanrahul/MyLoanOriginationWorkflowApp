package sla

import (
	"context"
	"fmt"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ResolveEffectiveSLADefinition resolves SLA hierarchy with lower-level overrides.
func ResolveEffectiveSLADefinition(
	config model.CaseTypeConfig,
	stageCode string,
	activityCode string,
	taskCode string,
) (*model.SLADefinition, error) {
	var resolved model.SLADefinition
	found := false

	if config.SLA != nil && config.SLA.Case != nil {
		resolved = *config.SLA.Case
		found = true
	}

	stageDef := findStageDef(config.Stages, stageCode)
	if stageDef != nil {
		if stageDef.SLA != nil {
			resolved = mergeSLADefinitions(resolved, *stageDef.SLA)
			found = true
		}
		if config.SLA != nil && config.SLA.Stages != nil {
			if s, ok := config.SLA.Stages[stageCode]; ok && s != nil {
				resolved = mergeSLADefinitions(resolved, *s)
				found = true
			}
		}
	}

	if stageDef != nil {
		activityDef := findActivityDef(stageDef.Activities, activityCode)
		if activityDef != nil {
			if activityDef.SLA != nil {
				resolved = mergeSLADefinitions(resolved, *activityDef.SLA)
				found = true
			}
			if config.SLA != nil && config.SLA.Activities != nil {
				if a, ok := config.SLA.Activities[activityCode]; ok && a != nil {
					resolved = mergeSLADefinitions(resolved, *a)
					found = true
				}
			}

			taskDef := findTaskDef(activityDef.TaskDefs, taskCode)
			if taskDef != nil {
				if taskDef.SLA != nil {
					resolved = mergeSLADefinitions(resolved, *taskDef.SLA)
					found = true
				}
				if config.SLA != nil && config.SLA.Tasks != nil {
					if t, ok := config.SLA.Tasks[taskCode]; ok && t != nil {
						resolved = mergeSLADefinitions(resolved, *t)
						found = true
					}
				}
			}
		}
	}

	if !found {
		return nil, nil
	}
	if resolved.DurationHours <= 0 {
		return nil, fmt.Errorf("ResolveEffectiveSLADefinition: duration_hours must be > 0 for resolved SLA")
	}

	if resolved.WarningThresholdPct <= 0 {
		resolved.WarningThresholdPct = 80
	}
	if resolved.CriticalThresholdPct <= 0 {
		resolved.CriticalThresholdPct = 95
	}
	if resolved.BreachAction == "" {
		resolved.BreachAction = model.SLABreachActionNotifyOnly
	}

	return &resolved, nil
}

// ComputeSLADeadline computes due_at from start + business duration.
func ComputeSLADeadline(ctx context.Context, db *sqlx.DB, start time.Time, defaultCalendarID string, def model.SLADefinition) (time.Time, int64, string, error) {
	if def.DurationHours <= 0 {
		return time.Time{}, 0, "", fmt.Errorf("ComputeSLADeadline: duration_hours must be > 0")
	}

	calendarID := def.CalendarID
	if calendarID == "" {
		calendarID = defaultCalendarID
	}
	if calendarID == "" {
		return time.Time{}, 0, "", fmt.Errorf("ComputeSLADeadline: calendar_id is required")
	}

	duration := time.Duration(def.DurationHours * float64(time.Hour))
	dueAt, err := AddBusinessHours(ctx, db, start.UTC(), duration, calendarID)
	if err != nil {
		return time.Time{}, 0, "", fmt.Errorf("ComputeSLADeadline: %w", err)
	}

	return dueAt, duration.Milliseconds(), calendarID, nil
}

// InitializeCaseSLA writes initial immutable case-level SLA snapshot columns.
func InitializeCaseSLA(ctx context.Context, tx *sqlx.Tx, caseID string, dueAt time.Time, durationMS int64, calendarID string, warningPct, criticalPct float64, action model.SLABreachAction) error {
	if tx == nil {
		return fmt.Errorf("InitializeCaseSLA: tx is nil")
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET case_due_at = $1,
		    case_effective_start_time = now(),
		    case_sla_duration_ms = $2,
		    case_sla_calendar_id = $3::uuid,
		    case_sla_warning_threshold_pct = $4,
		    case_sla_critical_threshold_pct = $5,
		    case_sla_breach_action = $6,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $7::uuid
	`, dueAt.UTC(), durationMS, calendarID, warningPct, criticalPct, string(action), caseID)
	if err != nil {
		return fmt.Errorf("InitializeCaseSLA: %w", err)
	}
	return nil
}

// InitializeTaskSLA writes initial immutable task-level SLA snapshot columns.
func InitializeTaskSLA(ctx context.Context, tx *sqlx.Tx, taskID string, dueAt time.Time, durationMS int64, calendarID string, warningPct, criticalPct float64, action model.SLABreachAction) error {
	if tx == nil {
		return fmt.Errorf("InitializeTaskSLA: tx is nil")
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET task_due_at = $1,
		    due_at = $1,
		    effective_start_time = now(),
		    sla_duration_ms = $2,
		    sla_calendar_id = $3::uuid,
		    sla_warning_threshold_pct = $4,
		    sla_critical_threshold_pct = $5,
		    sla_breach_action = $6,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $7::uuid
	`, dueAt.UTC(), durationMS, calendarID, warningPct, criticalPct, string(action), taskID)
	if err != nil {
		return fmt.Errorf("InitializeTaskSLA: %w", err)
	}
	return nil
}

func mergeSLADefinitions(base model.SLADefinition, override model.SLADefinition) model.SLADefinition {
	out := base
	if override.DurationHours > 0 {
		out.DurationHours = override.DurationHours
	}
	if override.WarningThresholdPct > 0 {
		out.WarningThresholdPct = override.WarningThresholdPct
	}
	if override.CriticalThresholdPct > 0 {
		out.CriticalThresholdPct = override.CriticalThresholdPct
	}
	if override.BreachAction != "" {
		out.BreachAction = override.BreachAction
	}
	if override.CalendarID != "" {
		out.CalendarID = override.CalendarID
	}
	return out
}

func findStageDef(stages []model.StageDefinitionV2, code string) *model.StageDefinitionV2 {
	for i := range stages {
		if stages[i].Code == code {
			return &stages[i]
		}
	}
	return nil
}

func findActivityDef(activities []model.ActivityConfig, code string) *model.ActivityConfig {
	for i := range activities {
		if activities[i].Code == code {
			return &activities[i]
		}
	}
	return nil
}

func findTaskDef(tasks []model.TaskDefinitionV2, code string) *model.TaskDefinitionV2 {
	for i := range tasks {
		if tasks[i].Code == code {
			return &tasks[i]
		}
	}
	return nil
}
