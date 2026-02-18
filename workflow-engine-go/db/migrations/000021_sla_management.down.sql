-- 000021_sla_management.down.sql
-- Rollback SLA management schema introduced in 000021.

-- ---------------------------------------------------------------------------
-- 1) Drop append-only triggers
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS sla_extension_log_no_update_delete ON sla_extension_log;
DROP TRIGGER IF EXISTS sla_reset_log_no_update_delete ON sla_reset_log;
DROP TRIGGER IF EXISTS sla_breach_log_no_update_delete ON sla_breach_log;
DROP TRIGGER IF EXISTS sla_pause_log_no_update_delete ON sla_pause_log;

-- Keep trg_reject_mutation only if no other object depends on it.
DROP FUNCTION IF EXISTS trg_reject_mutation();

-- ---------------------------------------------------------------------------
-- 2) Drop sweeper/reporting indexes
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_outbox_sla_cancellable;
DROP INDEX IF EXISTS idx_tasks_sla_threshold_scan;
DROP INDEX IF EXISTS idx_tasks_sla_active_due;
DROP INDEX IF EXISTS idx_cases_sla_active_due;
DROP INDEX IF EXISTS idx_sla_metrics_summary_metric_case_type;
DROP INDEX IF EXISTS idx_sla_extension_entity_time;
DROP INDEX IF EXISTS idx_sla_reset_entity_time;
DROP INDEX IF EXISTS uq_sla_breach_once_per_cycle;
DROP INDEX IF EXISTS idx_sla_breach_entity_detected;
DROP INDEX IF EXISTS idx_sla_pause_latest_action;
DROP INDEX IF EXISTS idx_sla_pause_entity_time;
DROP INDEX IF EXISTS idx_holiday_calendars_lookup;
DROP INDEX IF EXISTS idx_business_calendars_default_per_tenant;
DROP INDEX IF EXISTS idx_business_calendars_tenant;

-- ---------------------------------------------------------------------------
-- 3) Drop table triggers
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS sla_metrics_summary_updated_at ON sla_metrics_summary;
DROP TRIGGER IF EXISTS sla_extension_log_updated_at ON sla_extension_log;
DROP TRIGGER IF EXISTS sla_reset_log_updated_at ON sla_reset_log;
DROP TRIGGER IF EXISTS sla_breach_log_updated_at ON sla_breach_log;
DROP TRIGGER IF EXISTS sla_pause_log_updated_at ON sla_pause_log;
DROP TRIGGER IF EXISTS holiday_calendars_updated_at ON holiday_calendars;
DROP TRIGGER IF EXISTS business_calendars_updated_at ON business_calendars;

-- ---------------------------------------------------------------------------
-- 4) Drop new tables (except legacy sla_breach_log base table)
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS sla_metrics_summary;
DROP TABLE IF EXISTS sla_extension_log;
DROP TABLE IF EXISTS sla_reset_log;
DROP TABLE IF EXISTS sla_pause_log;
DROP TABLE IF EXISTS holiday_calendars;
DROP TABLE IF EXISTS business_calendars;

-- ---------------------------------------------------------------------------
-- 5) Revert sla_breach_log extensions (preserve legacy columns/table)
-- ---------------------------------------------------------------------------
ALTER TABLE sla_breach_log
    DROP COLUMN IF EXISTS entity_type,
    DROP COLUMN IF EXISTS entity_id,
    DROP COLUMN IF EXISTS elapsed_time_minutes,
    DROP COLUMN IF EXISTS breach_severity,
    DROP COLUMN IF EXISTS breach_action_taken,
    DROP COLUMN IF EXISTS sla_cycle,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS updated_at;

-- ---------------------------------------------------------------------------
-- 6) Revert events_outbox extension
-- ---------------------------------------------------------------------------
ALTER TABLE events_outbox
    DROP COLUMN IF EXISTS cancelled_at;

-- ---------------------------------------------------------------------------
-- 7) Revert task/case SLA columns
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    DROP COLUMN IF EXISTS task_due_at,
    DROP COLUMN IF EXISTS sla_warning_issued_at,
    DROP COLUMN IF EXISTS sla_critical_issued_at,
    DROP COLUMN IF EXISTS sla_breach_detected_at,
    DROP COLUMN IF EXISTS effective_start_time,
    DROP COLUMN IF EXISTS sla_duration_ms,
    DROP COLUMN IF EXISTS sla_warning_threshold_pct,
    DROP COLUMN IF EXISTS sla_critical_threshold_pct,
    DROP COLUMN IF EXISTS sla_breach_action,
    DROP COLUMN IF EXISTS sla_calendar_id,
    DROP COLUMN IF EXISTS sla_cycle;

ALTER TABLE cases
    DROP COLUMN IF EXISTS case_due_at,
    DROP COLUMN IF EXISTS case_sla_warning_issued_at,
    DROP COLUMN IF EXISTS case_sla_critical_issued_at,
    DROP COLUMN IF EXISTS case_sla_breach_detected_at,
    DROP COLUMN IF EXISTS case_effective_start_time,
    DROP COLUMN IF EXISTS case_sla_duration_ms,
    DROP COLUMN IF EXISTS case_sla_warning_threshold_pct,
    DROP COLUMN IF EXISTS case_sla_critical_threshold_pct,
    DROP COLUMN IF EXISTS case_sla_breach_action,
    DROP COLUMN IF EXISTS case_sla_calendar_id,
    DROP COLUMN IF EXISTS case_sla_cycle;
