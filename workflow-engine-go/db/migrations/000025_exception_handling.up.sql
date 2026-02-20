-- 000025_exception_handling.up.sql
-- Exception & error handling capability:
-- deterministic error classification, retry policy tracking,
-- dead-letter queue support, poison-pill quarantine,
-- compensation tracking, and case-level exception escalation.

-- ---------------------------------------------------------------------------
-- 1) Extend tasks with failure telemetry and poison-pill controls
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS total_failure_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS is_poison_pill BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS poison_pill_quarantined_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS poison_pill_reason TEXT,
    ADD COLUMN IF NOT EXISTS last_error_code VARCHAR(100),
    ADD COLUMN IF NOT EXISTS last_error_class VARCHAR(20)
        CHECK (last_error_class IN ('TRANSIENT', 'PERMANENT', 'UNKNOWN'));

COMMENT ON COLUMN tasks.total_failure_count IS
'Running count of failures across all attempts/requeues for poison-pill detection.';
COMMENT ON COLUMN tasks.is_poison_pill IS
'True when task exceeded poison-pill threshold and is quarantined from automatic retry.';
COMMENT ON COLUMN tasks.poison_pill_quarantined_at IS
'UTC timestamp when task was quarantined as poison pill.';
COMMENT ON COLUMN tasks.poison_pill_reason IS
'Human-readable quarantine reason (threshold reached, repeated invariant failure, etc.).';
COMMENT ON COLUMN tasks.last_error_code IS
'Most recent normalized error code captured on failure.';
COMMENT ON COLUMN tasks.last_error_class IS
'Most recent deterministic error class: TRANSIENT, PERMANENT, or UNKNOWN.';

-- Worker polling relies on SELECT FOR UPDATE SKIP LOCKED + retry window constraints.
CREATE INDEX IF NOT EXISTS idx_tasks_pending_retry_window
    ON tasks (status, next_retry_at, priority DESC, due_at ASC NULLS LAST)
    WHERE status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_tasks_poison_pill
    ON tasks (is_poison_pill, status)
    WHERE is_poison_pill = TRUE;

CREATE INDEX IF NOT EXISTS idx_tasks_last_error_code
    ON tasks (last_error_code)
    WHERE last_error_code IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2) Extend cases with EXCEPTION lifecycle fields
-- ---------------------------------------------------------------------------
ALTER TABLE cases
    ADD COLUMN IF NOT EXISTS exception_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS exception_reason TEXT,
    ADD COLUMN IF NOT EXISTS exception_task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS exception_severity VARCHAR(20)
        CHECK (exception_severity IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL', 'BLOCKING'));

COMMENT ON COLUMN cases.exception_at IS
'UTC timestamp when case moved into EXCEPTION status.';
COMMENT ON COLUMN cases.exception_reason IS
'Primary failure reason that triggered case exception escalation.';
COMMENT ON COLUMN cases.exception_task_id IS
'Task that triggered the current exception state for this case.';
COMMENT ON COLUMN cases.exception_severity IS
'Failure severity used for escalation policy and dashboard triage.';

ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check
    CHECK (status IN ('OPEN', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED', 'SUSPENDED', 'REJECTED', 'CLONED', 'EXCEPTION'));

CREATE INDEX IF NOT EXISTS idx_cases_exception_oldest
    ON cases (status, exception_at ASC, created_at ASC)
    WHERE status = 'EXCEPTION';

CREATE INDEX IF NOT EXISTS idx_cases_exception_task
    ON cases (exception_task_id)
    WHERE exception_task_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 3) Dead letter queue (append entries, soft-delete on requeue)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_dlq (
    dlq_id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id              UUID         NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    case_id              UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    failure_reason       TEXT         NOT NULL,
    error_detail         JSONB        NOT NULL DEFAULT '{}',
    moved_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    requeue_count        INT          NOT NULL DEFAULT 0 CHECK (requeue_count >= 0),
    last_requeue_at      TIMESTAMPTZ,
    is_poison_pill       BOOLEAN      NOT NULL DEFAULT FALSE,
    quarantine_released_at TIMESTAMPTZ,
    soft_deleted_at      TIMESTAMPTZ,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT chk_task_dlq_soft_delete_order
        CHECK (soft_deleted_at IS NULL OR soft_deleted_at >= moved_at)
);

COMMENT ON TABLE task_dlq IS
'Dead-letter queue entries for tasks that reached terminal failure (retries exhausted/permanent/poison-pill).';
COMMENT ON COLUMN task_dlq.error_detail IS
'Structured failure context captured from worker runtime and downstream responses.';
COMMENT ON COLUMN task_dlq.soft_deleted_at IS
'Soft-delete marker set when operator requeues the DLQ entry.';

CREATE INDEX IF NOT EXISTS idx_task_dlq_case_oldest
    ON task_dlq (case_id, moved_at ASC);

CREATE INDEX IF NOT EXISTS idx_task_dlq_task_history
    ON task_dlq (task_id, moved_at DESC);

CREATE INDEX IF NOT EXISTS idx_task_dlq_active
    ON task_dlq (moved_at ASC)
    WHERE soft_deleted_at IS NULL;

DROP TRIGGER IF EXISTS task_dlq_updated_at ON task_dlq;
CREATE TRIGGER task_dlq_updated_at
    BEFORE UPDATE ON task_dlq
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) Retry history for operator diagnostics and audit
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_retry_history (
    attempt_id                 UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id                    UUID         NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    case_id                    UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    attempt_number             INT          NOT NULL CHECK (attempt_number >= 0),
    retry_count_before         INT          NOT NULL CHECK (retry_count_before >= 0),
    max_retries                INT          NOT NULL CHECK (max_retries >= 0),
    backoff_strategy           VARCHAR(20)  NOT NULL
        CHECK (backoff_strategy IN ('FIXED', 'LINEAR', 'EXPONENTIAL')),
    base_interval_seconds      INT          NOT NULL CHECK (base_interval_seconds > 0),
    max_interval_seconds       INT          NOT NULL CHECK (max_interval_seconds > 0),
    computed_interval_seconds  INT          NOT NULL CHECK (computed_interval_seconds >= 0),
    scheduled_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    next_attempt_at            TIMESTAMPTZ,
    error_code                 VARCHAR(100) NOT NULL,
    error_class                VARCHAR(20)  NOT NULL
        CHECK (error_class IN ('TRANSIENT', 'PERMANENT', 'UNKNOWN')),
    error_detail               JSONB        NOT NULL DEFAULT '{}',
    source_service             TEXT         NOT NULL,
    outcome                    VARCHAR(30)  NOT NULL
        CHECK (outcome IN ('RETRY_SCHEDULED', 'FAILED_TERMINAL', 'DLQ_REQUEUED')),
    created_at                 TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE task_retry_history IS
'Immutable retry attempt history used by operator dashboards and forensic analysis.';
COMMENT ON COLUMN task_retry_history.computed_interval_seconds IS
'Backoff interval actually computed for this attempt (capped by policy).';

CREATE INDEX IF NOT EXISTS idx_task_retry_history_task_attempt
    ON task_retry_history (task_id, attempt_number ASC);

CREATE INDEX IF NOT EXISTS idx_task_retry_history_case_time
    ON task_retry_history (case_id, scheduled_at DESC);

-- ---------------------------------------------------------------------------
-- 5) Compensation state tracking for saga rollback patterns
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_compensations (
    compensation_id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id                     UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    failed_task_id              UUID         NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    failed_task_definition_code VARCHAR(100) NOT NULL,
    compensating_task_code      VARCHAR(100) NOT NULL,
    compensating_task_id        UUID         REFERENCES tasks(id) ON DELETE SET NULL,
    status                      VARCHAR(20)  NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'IN_PROGRESS', 'COMPLETED', 'FAILED')),
    started_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    completed_at                TIMESTAMPTZ,
    error_detail                JSONB,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_compensations_failed UNIQUE (failed_task_id, compensating_task_code)
);

COMMENT ON TABLE task_compensations IS
'Saga compensation tracking for failed forward tasks that require rollback actions.';
COMMENT ON COLUMN task_compensations.status IS
'Compensation lifecycle: PENDING, IN_PROGRESS, COMPLETED, FAILED.';

CREATE INDEX IF NOT EXISTS idx_task_compensations_case_status
    ON task_compensations (case_id, status, started_at ASC);

CREATE INDEX IF NOT EXISTS idx_task_compensations_comp_task
    ON task_compensations (compensating_task_id)
    WHERE compensating_task_id IS NOT NULL;

DROP TRIGGER IF EXISTS task_compensations_updated_at ON task_compensations;
CREATE TRIGGER task_compensations_updated_at
    BEFORE UPDATE ON task_compensations
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();
