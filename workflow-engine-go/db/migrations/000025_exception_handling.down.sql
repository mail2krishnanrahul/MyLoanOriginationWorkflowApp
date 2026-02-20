-- 000025_exception_handling.down.sql
-- Rollback exception & error handling capability without data loss.
-- Strategy: archive data into rollback tables, then drop runtime tables/columns.

-- ---------------------------------------------------------------------------
-- 1) Archive runtime tables before dropping schema objects
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF to_regclass('task_dlq') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS task_dlq_archive_000025 (LIKE task_dlq INCLUDING ALL)';
        EXECUTE '
            INSERT INTO task_dlq_archive_000025
            SELECT * FROM task_dlq
            ON CONFLICT (dlq_id) DO NOTHING
        ';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('task_retry_history') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS task_retry_history_archive_000025 (LIKE task_retry_history INCLUDING ALL)';
        EXECUTE '
            INSERT INTO task_retry_history_archive_000025
            SELECT * FROM task_retry_history
            ON CONFLICT (attempt_id) DO NOTHING
        ';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('task_compensations') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS task_compensations_archive_000025 (LIKE task_compensations INCLUDING ALL)';
        EXECUTE '
            INSERT INTO task_compensations_archive_000025
            SELECT * FROM task_compensations
            ON CONFLICT (compensation_id) DO NOTHING
        ';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS tasks_exception_columns_archive_000025 (
    task_id                     UUID PRIMARY KEY,
    total_failure_count         INT,
    is_poison_pill              BOOLEAN,
    poison_pill_quarantined_at  TIMESTAMPTZ,
    poison_pill_reason          TEXT,
    last_error_code             VARCHAR(100),
    last_error_class            VARCHAR(20),
    archived_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO tasks_exception_columns_archive_000025 (
    task_id,
    total_failure_count,
    is_poison_pill,
    poison_pill_quarantined_at,
    poison_pill_reason,
    last_error_code,
    last_error_class
)
SELECT
    id,
    total_failure_count,
    is_poison_pill,
    poison_pill_quarantined_at,
    poison_pill_reason,
    last_error_code,
    last_error_class
FROM tasks
ON CONFLICT (task_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS cases_exception_columns_archive_000025 (
    case_id            UUID PRIMARY KEY,
    exception_at       TIMESTAMPTZ,
    exception_reason   TEXT,
    exception_task_id  UUID,
    exception_severity VARCHAR(20),
    archived_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO cases_exception_columns_archive_000025 (
    case_id,
    exception_at,
    exception_reason,
    exception_task_id,
    exception_severity
)
SELECT
    id,
    exception_at,
    exception_reason,
    exception_task_id,
    exception_severity
FROM cases
ON CONFLICT (case_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2) Drop triggers and indexes
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS task_compensations_updated_at ON task_compensations;
DROP TRIGGER IF EXISTS task_dlq_updated_at ON task_dlq;

DROP INDEX IF EXISTS idx_task_compensations_comp_task;
DROP INDEX IF EXISTS idx_task_compensations_case_status;
DROP INDEX IF EXISTS idx_task_retry_history_case_time;
DROP INDEX IF EXISTS idx_task_retry_history_task_attempt;
DROP INDEX IF EXISTS idx_task_dlq_active;
DROP INDEX IF EXISTS idx_task_dlq_task_history;
DROP INDEX IF EXISTS idx_task_dlq_case_oldest;
DROP INDEX IF EXISTS idx_cases_exception_task;
DROP INDEX IF EXISTS idx_cases_exception_oldest;
DROP INDEX IF EXISTS idx_tasks_last_error_code;
DROP INDEX IF EXISTS idx_tasks_poison_pill;
DROP INDEX IF EXISTS idx_tasks_pending_retry_window;

-- ---------------------------------------------------------------------------
-- 3) Drop runtime tables introduced by 000025
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS task_compensations;
DROP TABLE IF EXISTS task_retry_history;
DROP TABLE IF EXISTS task_dlq;

-- ---------------------------------------------------------------------------
-- 4) Revert case status constraint and remove added columns
-- ---------------------------------------------------------------------------
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check
    CHECK (status IN ('OPEN', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED', 'SUSPENDED', 'REJECTED', 'CLONED'));

ALTER TABLE cases
    DROP COLUMN IF EXISTS exception_severity,
    DROP COLUMN IF EXISTS exception_task_id,
    DROP COLUMN IF EXISTS exception_reason,
    DROP COLUMN IF EXISTS exception_at;

-- ---------------------------------------------------------------------------
-- 5) Remove task columns introduced by 000025
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    DROP COLUMN IF EXISTS last_error_class,
    DROP COLUMN IF EXISTS last_error_code,
    DROP COLUMN IF EXISTS poison_pill_reason,
    DROP COLUMN IF EXISTS poison_pill_quarantined_at,
    DROP COLUMN IF EXISTS is_poison_pill,
    DROP COLUMN IF EXISTS total_failure_count;
