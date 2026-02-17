-- 011-events-outbox.sql
-- Transactional outbox: all cross-service communication is written here
-- inside the same DB transaction as the business operation, then polled
-- and delivered by a relay worker.

CREATE TABLE IF NOT EXISTS events_outbox (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Context (both nullable — some events are system-level)
    case_id             UUID            REFERENCES cases(id) ON DELETE SET NULL,
    task_id             UUID            REFERENCES tasks(id) ON DELETE SET NULL,

    -- Event classification
    event_type          VARCHAR(100)    NOT NULL,
    payload             JSONB           NOT NULL DEFAULT '{}',

    -- Delivery lifecycle
    status              VARCHAR(20)     NOT NULL DEFAULT 'PENDING'
                            CHECK (status IN ('PENDING', 'DELIVERED', 'FAILED')),
    target_service      TEXT            NOT NULL,

    -- Retry management
    attempts            INT             NOT NULL DEFAULT 0,
    max_attempts        INT             NOT NULL DEFAULT 5,
    last_attempted_at   TIMESTAMPTZ,

    -- Ordering & tracing
    partition_key       TEXT,           -- typically case_id::text for ordered-per-case delivery
    trace_id            TEXT,           -- distributed tracing correlation

    -- Timestamps
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    delivered_at        TIMESTAMPTZ
);

-- ===================================================================
-- Indexes
-- ===================================================================

-- 1. Relay worker polling: "give me pending events for my service, oldest first"
CREATE INDEX IF NOT EXISTS idx_outbox_poll
    ON events_outbox (target_service, status, created_at ASC)
    WHERE status = 'PENDING';

-- 2. Ordered-per-case delivery (partition_key = case_id::text)
CREATE INDEX IF NOT EXISTS idx_outbox_partition
    ON events_outbox (partition_key, created_at ASC);

-- 3. Case event history queries
CREATE INDEX IF NOT EXISTS idx_outbox_case_type
    ON events_outbox (case_id, event_type);
