-- 010-tasks.sql
-- The central operational table: atomic units of work consumed by workers.

CREATE TABLE IF NOT EXISTS tasks (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id                 UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,

    -- Definition references (denormalized from case_type config for queries)
    task_definition_code    VARCHAR(100)    NOT NULL,
    activity_code           VARCHAR(100)    NOT NULL,
    stage_code              VARCHAR(100)    NOT NULL,

    -- Lifecycle
    status                  VARCHAR(25)     NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN (
                                    'PENDING',
                                    'ASSIGNED',
                                    'IN_PROGRESS',
                                    'AWAITING_EXTERNAL',
                                    'COMPLETED',
                                    'FAILED',
                                    'CANCELLED',
                                    'SKIPPED'
                                )),

    -- Priority (integer for sort ordering: 1=LOW, 2=NORMAL, 3=HIGH, 4=CRITICAL)
    priority                INT             NOT NULL DEFAULT 2
                                CHECK (priority BETWEEN 1 AND 4),

    -- Assignment
    assigned_service        TEXT,                           -- which worker/microservice owns this
    assigned_at             TIMESTAMPTZ,
    started_at              TIMESTAMPTZ,
    completed_at            TIMESTAMPTZ,
    due_at                  TIMESTAMPTZ,

    -- Retry management
    retry_count             INT             NOT NULL DEFAULT 0,
    max_retries             INT             NOT NULL DEFAULT 3,

    -- Payloads (JSONB)
    input_payload           JSONB           NOT NULL DEFAULT '{}',
    output_payload          JSONB           NOT NULL DEFAULT '{}',
    metadata                JSONB           NOT NULL DEFAULT '{}',
    error_detail            JSONB,

    -- Idempotency / deduplication
    idempotency_key         TEXT            NOT NULL,

    -- Optimistic locking
    version                 INT             NOT NULL DEFAULT 1,

    -- Timestamps
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),

    -- Unique key for safe retries
    CONSTRAINT uq_tasks_idempotency_key UNIQUE (idempotency_key)
);

-- ===================================================================
-- Indexes
-- ===================================================================

-- 1. Case progress queries: "show me all tasks for this case by status"
CREATE INDEX IF NOT EXISTS idx_tasks_case_status
    ON tasks (case_id, status);

-- 2. Worker polling: "give me the highest-priority, most-urgent task I own"
CREATE INDEX IF NOT EXISTS idx_tasks_worker_poll
    ON tasks (assigned_service, status, priority DESC, due_at ASC NULLS LAST);

-- 3. Activity completion checks: "are all tasks for this activity done?"
CREATE INDEX IF NOT EXISTS idx_tasks_activity_status
    ON tasks (stage_code, activity_code, status);

-- 4. Stage rollup reporting: "per-stage task breakdown for a case"
CREATE INDEX IF NOT EXISTS idx_tasks_case_stage
    ON tasks (case_id, stage_code);

-- ===================================================================
-- Auto-update updated_at (reuse function from 006-case-types.sql)
-- ===================================================================
DROP TRIGGER IF EXISTS tasks_updated_at ON tasks;
CREATE TRIGGER tasks_updated_at
    BEFORE UPDATE ON tasks
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();
