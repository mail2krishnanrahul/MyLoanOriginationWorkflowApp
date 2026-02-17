-- 013-audit-trail.sql
-- Unified audit trail for tracking all case mutations.
-- Records: who changed what, when, and the delta.

CREATE TABLE IF NOT EXISTS audit_trail (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id         UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,

    -- What happened
    action          VARCHAR(100)    NOT NULL,  -- e.g. CASE_CREATED, TASK_COMPLETED, STAGE_CHANGED
    entity_type     VARCHAR(50)     NOT NULL,  -- e.g. CASE, TASK, STAGE, ACTIVITY
    entity_id       UUID,                      -- the specific entity that changed (nullable for case-level)

    -- Who did it
    actor_id        TEXT            NOT NULL,  -- user ID or service name
    actor_type      VARCHAR(20)    NOT NULL DEFAULT 'SYSTEM'
                        CHECK (actor_type IN ('USER', 'SYSTEM', 'API')),

    -- What changed
    change_delta    JSONB           NOT NULL DEFAULT '{}', -- before/after or event payload
    metadata        JSONB,                                 -- extra context (IP, user-agent, etc.)

    -- When
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now()
);

-- Recent audit entries for a case (timeline view)
CREATE INDEX IF NOT EXISTS idx_audit_trail_case
    ON audit_trail (case_id, created_at DESC);

-- Filter by action type (e.g. "show me all TASK_COMPLETED events")
CREATE INDEX IF NOT EXISTS idx_audit_trail_action
    ON audit_trail (action, created_at DESC);

-- Filter by actor (e.g. "what did user X do?")
CREATE INDEX IF NOT EXISTS idx_audit_trail_actor
    ON audit_trail (actor_id, created_at DESC);

-- Filter by entity (e.g. "history of task Y")
CREATE INDEX IF NOT EXISTS idx_audit_trail_entity
    ON audit_trail (entity_type, entity_id)
    WHERE entity_id IS NOT NULL;

COMMENT ON TABLE audit_trail IS 'Unified audit log for all case-level mutations';
