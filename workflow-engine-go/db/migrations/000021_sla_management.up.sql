-- 000021_sla_management.up.sql
-- SLA management: hierarchical SLA state, business calendars, pause/resume,
-- breach tracking, reset/extension audit, and daily reporting summaries.

-- ---------------------------------------------------------------------------
-- 1) Business calendars (tenant-aware working schedule definition)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS business_calendars (
    id                        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                 TEXT         NOT NULL DEFAULT 'default',
    name                      TEXT         NOT NULL,
    timezone                  TEXT         NOT NULL,
    start_time                TIME         NOT NULL,
    end_time                  TIME         NOT NULL,
    working_days_bitfield     INT          NOT NULL CHECK (working_days_bitfield BETWEEN 1 AND 127),
    is_default                BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_business_calendars_tenant_name UNIQUE (tenant_id, name),
    CONSTRAINT chk_business_calendars_hours CHECK (end_time > start_time)
);

COMMENT ON TABLE business_calendars IS
'Working-time calendars used for SLA computation. One tenant may define multiple calendars.';
COMMENT ON COLUMN business_calendars.tenant_id IS
'Tenant owning this calendar. Default tenant is ''default''.';
COMMENT ON COLUMN business_calendars.timezone IS
'IANA timezone (for example: UTC, America/New_York).';
COMMENT ON COLUMN business_calendars.start_time IS
'Business-day start time in calendar timezone.';
COMMENT ON COLUMN business_calendars.end_time IS
'Business-day end time in calendar timezone. Must be after start_time.';
COMMENT ON COLUMN business_calendars.working_days_bitfield IS
'Bitfield: Mon=1, Tue=2, Wed=4, Thu=8, Fri=16, Sat=32, Sun=64.';

CREATE INDEX IF NOT EXISTS idx_business_calendars_tenant
    ON business_calendars (tenant_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_business_calendars_default_per_tenant
    ON business_calendars (tenant_id)
    WHERE is_default = TRUE;

DROP TRIGGER IF EXISTS business_calendars_updated_at ON business_calendars;
CREATE TRIGGER business_calendars_updated_at
    BEFORE UPDATE ON business_calendars
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 2) Holiday calendar rows
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS holiday_calendars (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    calendar_id       UUID         NOT NULL REFERENCES business_calendars(id) ON DELETE CASCADE,
    holiday_date      DATE         NOT NULL,
    name              TEXT         NOT NULL,
    is_recurring      BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_holiday_calendars UNIQUE (calendar_id, holiday_date, is_recurring)
);

COMMENT ON TABLE holiday_calendars IS
'Calendar-specific holidays excluded from SLA business-time clocks.';
COMMENT ON COLUMN holiday_calendars.holiday_date IS
'Holiday date in calendar local date (not timestamp).';
COMMENT ON COLUMN holiday_calendars.is_recurring IS
'True means yearly recurring date (month/day), false means one-time date.';

CREATE INDEX IF NOT EXISTS idx_holiday_calendars_lookup
    ON holiday_calendars (calendar_id, holiday_date);

DROP TRIGGER IF EXISTS holiday_calendars_updated_at ON holiday_calendars;
CREATE TRIGGER holiday_calendars_updated_at
    BEFORE UPDATE ON holiday_calendars
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 3) Extend existing runtime entities with immutable SLA snapshot state
-- ---------------------------------------------------------------------------
ALTER TABLE cases
    ADD COLUMN IF NOT EXISTS case_due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS case_sla_warning_issued_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS case_sla_critical_issued_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS case_sla_breach_detected_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS case_effective_start_time TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS case_sla_duration_ms BIGINT,
    ADD COLUMN IF NOT EXISTS case_sla_warning_threshold_pct NUMERIC(5,2),
    ADD COLUMN IF NOT EXISTS case_sla_critical_threshold_pct NUMERIC(5,2),
    ADD COLUMN IF NOT EXISTS case_sla_breach_action VARCHAR(64)
        CHECK (case_sla_breach_action IN (
            'ESCALATE_TO_SUPERVISOR',
            'AUTO_REASSIGN',
            'CREATE_EXCEPTION_CASE',
            'NOTIFY_ONLY'
        )),
    ADD COLUMN IF NOT EXISTS case_sla_calendar_id UUID REFERENCES business_calendars(id),
    ADD COLUMN IF NOT EXISTS case_sla_cycle INT NOT NULL DEFAULT 1;

COMMENT ON COLUMN cases.case_due_at IS
'Business-calendar adjusted SLA deadline for the full case.';
COMMENT ON COLUMN cases.case_effective_start_time IS
'Effective SLA start after pause/resume adjustments for case-level SLA.';
COMMENT ON COLUMN cases.case_sla_duration_ms IS
'Immutable case-level SLA duration snapshot in milliseconds.';
COMMENT ON COLUMN cases.case_sla_cycle IS
'SLA cycle counter; incremented by SLA reset to allow repeat warning/critical/breach events.';

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS task_due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sla_warning_issued_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sla_critical_issued_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sla_breach_detected_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS effective_start_time TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sla_duration_ms BIGINT,
    ADD COLUMN IF NOT EXISTS sla_warning_threshold_pct NUMERIC(5,2),
    ADD COLUMN IF NOT EXISTS sla_critical_threshold_pct NUMERIC(5,2),
    ADD COLUMN IF NOT EXISTS sla_breach_action VARCHAR(64)
        CHECK (sla_breach_action IN (
            'ESCALATE_TO_SUPERVISOR',
            'AUTO_REASSIGN',
            'CREATE_EXCEPTION_CASE',
            'NOTIFY_ONLY'
        )),
    ADD COLUMN IF NOT EXISTS sla_calendar_id UUID REFERENCES business_calendars(id),
    ADD COLUMN IF NOT EXISTS sla_cycle INT NOT NULL DEFAULT 1;

COMMENT ON COLUMN tasks.task_due_at IS
'Business-calendar adjusted task-level SLA deadline. Worker scheduling still uses due_at.';
COMMENT ON COLUMN tasks.effective_start_time IS
'Effective SLA start timestamp adjusted for pauses; used in threshold calculations.';
COMMENT ON COLUMN tasks.sla_duration_ms IS
'Immutable task-level SLA duration snapshot in milliseconds.';
COMMENT ON COLUMN tasks.sla_cycle IS
'SLA cycle counter; incremented by SLA reset to allow repeat warning/critical/breach events.';

-- Maintain compatibility with existing worker scheduler logic that uses due_at.
UPDATE tasks
SET due_at = task_due_at
WHERE task_due_at IS NOT NULL
  AND due_at IS NULL;

ALTER TABLE events_outbox
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;

COMMENT ON COLUMN events_outbox.cancelled_at IS
'Cancellation marker for pending threshold events invalidated by SLA reset/extension.';

-- ---------------------------------------------------------------------------
-- 4) Pause/resume append-only log
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sla_pause_log (
    id                        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type               VARCHAR(20)  NOT NULL CHECK (entity_type IN ('CASE', 'STAGE', 'ACTIVITY', 'TASK')),
    entity_id                 UUID         NOT NULL,
    paused_at                 TIMESTAMPTZ  NOT NULL,
    resumed_at                TIMESTAMPTZ,
    pause_reason              TEXT         NOT NULL,
    elapsed_before_pause_ms   BIGINT       NOT NULL CHECK (elapsed_before_pause_ms >= 0),
    action                    VARCHAR(10)  NOT NULL CHECK (action IN ('PAUSE', 'RESUME')),
    created_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT chk_sla_pause_log_resumed_after_paused
        CHECK (resumed_at IS NULL OR resumed_at >= paused_at)
);

COMMENT ON TABLE sla_pause_log IS
'Append-only SLA pause/resume audit for polymorphic entities.';
COMMENT ON COLUMN sla_pause_log.elapsed_before_pause_ms IS
'Business-time elapsed for the SLA before this pause action.';

CREATE INDEX IF NOT EXISTS idx_sla_pause_entity_time
    ON sla_pause_log (entity_type, entity_id, paused_at DESC);

CREATE INDEX IF NOT EXISTS idx_sla_pause_latest_action
    ON sla_pause_log (entity_type, entity_id, action, created_at DESC);

DROP TRIGGER IF EXISTS sla_pause_log_updated_at ON sla_pause_log;
CREATE TRIGGER sla_pause_log_updated_at
    BEFORE UPDATE ON sla_pause_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 5) Breach log (extend existing table non-destructively)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sla_breach_log (
    id                     UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id                UUID         REFERENCES tasks(id) ON DELETE CASCADE,
    case_id                UUID         REFERENCES cases(id) ON DELETE CASCADE,
    original_due_at        TIMESTAMPTZ,
    breach_detected_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    assignee_at_breach     TEXT,
    elapsed_percentage     INT,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now()
);

ALTER TABLE sla_breach_log
    ADD COLUMN IF NOT EXISTS entity_type VARCHAR(20)
        CHECK (entity_type IN ('CASE', 'STAGE', 'ACTIVITY', 'TASK')),
    ADD COLUMN IF NOT EXISTS entity_id UUID,
    ADD COLUMN IF NOT EXISTS elapsed_time_minutes INT,
    ADD COLUMN IF NOT EXISTS breach_severity VARCHAR(20)
        CHECK (breach_severity IN ('MINOR', 'MODERATE', 'MAJOR', 'CRITICAL')),
    ADD COLUMN IF NOT EXISTS breach_action_taken VARCHAR(64)
        CHECK (breach_action_taken IN (
            'ESCALATE_TO_SUPERVISOR',
            'AUTO_REASSIGN',
            'CREATE_EXCEPTION_CASE',
            'NOTIFY_ONLY'
        )),
    ADD COLUMN IF NOT EXISTS sla_cycle INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

COMMENT ON TABLE sla_breach_log IS
'Append-only SLA breach audit. Supports both legacy task-centric and new polymorphic entities.';
COMMENT ON COLUMN sla_breach_log.elapsed_time_minutes IS
'Actual elapsed business-time in minutes when breach was detected.';
COMMENT ON COLUMN sla_breach_log.breach_severity IS
'Computed severity by percentage overrun: MINOR/MODERATE/MAJOR/CRITICAL.';
COMMENT ON COLUMN sla_breach_log.sla_cycle IS
'SLA cycle identifier supporting multiple breaches across reset cycles.';

CREATE INDEX IF NOT EXISTS idx_sla_breach_entity_detected
    ON sla_breach_log (entity_type, entity_id, breach_detected_at DESC)
    WHERE entity_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_sla_breach_once_per_cycle
    ON sla_breach_log (entity_type, entity_id, sla_cycle)
    WHERE entity_id IS NOT NULL;

DROP TRIGGER IF EXISTS sla_breach_log_updated_at ON sla_breach_log;
CREATE TRIGGER sla_breach_log_updated_at
    BEFORE UPDATE ON sla_breach_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 6) Supervisor actions: reset and extension logs
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sla_reset_log (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type          VARCHAR(20)  NOT NULL CHECK (entity_type IN ('CASE', 'STAGE', 'ACTIVITY', 'TASK')),
    entity_id            UUID         NOT NULL,
    reset_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    previous_due_at      TIMESTAMPTZ,
    new_due_at           TIMESTAMPTZ,
    new_duration_hours   NUMERIC(10,2),
    reason               TEXT         NOT NULL,
    approved_by          TEXT         NOT NULL,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE sla_reset_log IS
'Append-only audit of supervisor-approved SLA reset operations.';

CREATE INDEX IF NOT EXISTS idx_sla_reset_entity_time
    ON sla_reset_log (entity_type, entity_id, reset_at DESC);

DROP TRIGGER IF EXISTS sla_reset_log_updated_at ON sla_reset_log;
CREATE TRIGGER sla_reset_log_updated_at
    BEFORE UPDATE ON sla_reset_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS sla_extension_log (
    id                         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type                VARCHAR(20)  NOT NULL CHECK (entity_type IN ('CASE', 'STAGE', 'ACTIVITY', 'TASK')),
    entity_id                  UUID         NOT NULL,
    extended_at                TIMESTAMPTZ  NOT NULL DEFAULT now(),
    previous_due_at            TIMESTAMPTZ,
    new_due_at                 TIMESTAMPTZ,
    extension_duration_hours   NUMERIC(10,2) NOT NULL CHECK (extension_duration_hours > 0),
    reason                     TEXT         NOT NULL,
    approved_by                TEXT         NOT NULL,
    created_at                 TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE sla_extension_log IS
'Append-only audit of supervisor-approved SLA extension operations.';

CREATE INDEX IF NOT EXISTS idx_sla_extension_entity_time
    ON sla_extension_log (entity_type, entity_id, extended_at DESC);

DROP TRIGGER IF EXISTS sla_extension_log_updated_at ON sla_extension_log;
CREATE TRIGGER sla_extension_log_updated_at
    BEFORE UPDATE ON sla_extension_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 7) SLA metrics daily summary (reporting source)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sla_metrics_summary (
    metric_date            DATE         NOT NULL,
    case_type_code         VARCHAR(100) NOT NULL,
    stage_code             VARCHAR(100) NOT NULL,
    activity_code          VARCHAR(100) NOT NULL,
    task_definition_code   VARCHAR(100) NOT NULL,
    total_count            BIGINT       NOT NULL DEFAULT 0,
    completed_count        BIGINT       NOT NULL DEFAULT 0,
    breached_count         BIGINT       NOT NULL DEFAULT 0,
    avg_elapsed_minutes    NUMERIC(12,2) NOT NULL DEFAULT 0,
    p50_elapsed_minutes    INT          NOT NULL DEFAULT 0,
    p95_elapsed_minutes    INT          NOT NULL DEFAULT 0,
    p99_elapsed_minutes    INT          NOT NULL DEFAULT 0,
    total_pause_minutes    BIGINT       NOT NULL DEFAULT 0,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (metric_date, case_type_code, stage_code, activity_code, task_definition_code)
);

COMMENT ON TABLE sla_metrics_summary IS
'Daily SLA rollups used for operational dashboards. Reporting must read this table, not live tasks.';

CREATE INDEX IF NOT EXISTS idx_sla_metrics_summary_metric_case_type
    ON sla_metrics_summary (metric_date, case_type_code);

DROP TRIGGER IF EXISTS sla_metrics_summary_updated_at ON sla_metrics_summary;
CREATE TRIGGER sla_metrics_summary_updated_at
    BEFORE UPDATE ON sla_metrics_summary
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 8) Sweeper-focused indexes (partial for large tables)
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_cases_sla_active_due
    ON cases (case_due_at, status)
    WHERE status IN ('OPEN', 'IN_PROGRESS') AND case_due_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_sla_active_due
    ON tasks (task_due_at, status)
    WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS') AND task_due_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_sla_threshold_scan
    ON tasks (status, sla_warning_issued_at, sla_critical_issued_at, sla_breach_detected_at, task_due_at)
    WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS') AND task_due_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_outbox_sla_cancellable
    ON events_outbox (event_type, status, created_at)
    WHERE event_type IN ('SLA_WARNING', 'SLA_CRITICAL')
      AND status IN ('PENDING', 'PROCESSING')
      AND cancelled_at IS NULL;

-- ---------------------------------------------------------------------------
-- 9) Append-only enforcement for SLA logs
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION trg_reject_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'append-only table: % does not allow %', TG_TABLE_NAME, TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS sla_pause_log_no_update_delete ON sla_pause_log;
CREATE TRIGGER sla_pause_log_no_update_delete
    BEFORE UPDATE OR DELETE ON sla_pause_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_mutation();

DROP TRIGGER IF EXISTS sla_breach_log_no_update_delete ON sla_breach_log;
CREATE TRIGGER sla_breach_log_no_update_delete
    BEFORE UPDATE OR DELETE ON sla_breach_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_mutation();

DROP TRIGGER IF EXISTS sla_reset_log_no_update_delete ON sla_reset_log;
CREATE TRIGGER sla_reset_log_no_update_delete
    BEFORE UPDATE OR DELETE ON sla_reset_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_mutation();

DROP TRIGGER IF EXISTS sla_extension_log_no_update_delete ON sla_extension_log;
CREATE TRIGGER sla_extension_log_no_update_delete
    BEFORE UPDATE OR DELETE ON sla_extension_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_mutation();

-- ---------------------------------------------------------------------------
-- 10) Seed default calendar
-- ---------------------------------------------------------------------------
INSERT INTO business_calendars (
    tenant_id,
    name,
    timezone,
    start_time,
    end_time,
    working_days_bitfield,
    is_default
)
VALUES (
    'default',
    'default',
    'UTC',
    '09:00',
    '17:00',
    31,
    TRUE
)
ON CONFLICT (tenant_id, name) DO NOTHING;
