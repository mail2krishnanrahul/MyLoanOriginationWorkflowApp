-- 000035_getnext.up.sql
-- GetNext Intelligent Work Distribution Engine
--
-- Index strategy for performance at 100k cases:
--   1. idx_cases_getnext_allocatable_035: leads with (tenant_id, status, current_stage_code)
--      so the planner prunes to ALLOCATABLE + ALLOCATION rows immediately.
--      Includes case_due_at for SLA sort and assigned_user_id (= NULL filter) as predicate.
--      This makes the WHERE clause fully index-covered before the scoring CTE runs.
--   2. idx_getnext_claims_user_035: enables O(1) session skip-count checks.
--   3. idx_case_user_affinity on the materialised view: makes per-user affinity lookups O(1).
--
-- All scoring is performed inside a single CTE in SQL — nothing is loaded to Go for sorting.

-- ---------------------------------------------------------------------------
-- 1) Extend cases.status to include ALLOCATABLE
--    (drop inline CHECK and re-add with the new value)
-- ---------------------------------------------------------------------------
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases
    ADD CONSTRAINT cases_status_check CHECK (status IN (
        'OPEN', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED', 'SUSPENDED', 'ALLOCATABLE'
    ));

-- ---------------------------------------------------------------------------
-- 2) Performance index for the GetNext scoring query
--    Only ALLOCATABLE cases in ALLOCATION stage with no assignee are candidates.
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_cases_getnext_allocatable_035
    ON cases (tenant_id, current_stage_code, case_due_at ASC NULLS LAST, created_at ASC)
    WHERE status = 'ALLOCATABLE' AND assigned_user_id IS NULL;

-- GIN index on required_skills supports array overlap operator (&& and <@)
CREATE INDEX IF NOT EXISTS idx_cases_required_skills_gin_035
    ON cases USING GIN (required_skills)
    WHERE status = 'ALLOCATABLE' AND assigned_user_id IS NULL;

-- ---------------------------------------------------------------------------
-- 3) getnext_weights — per-tenant, per-case-type weight config
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS getnext_weights (
    weight_id       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_type_code  TEXT         NOT NULL,
    w_sla           NUMERIC(5,4) NOT NULL DEFAULT 0.35,
    w_skill         NUMERIC(5,4) NOT NULL DEFAULT 0.25,
    w_age           NUMERIC(5,4) NOT NULL DEFAULT 0.10,
    w_complexity    NUMERIC(5,4) NOT NULL DEFAULT 0.10,
    w_value         NUMERIC(5,4) NOT NULL DEFAULT 0.10,
    w_affinity      NUMERIC(5,4) NOT NULL DEFAULT 0.05,
    w_workload      NUMERIC(5,4) NOT NULL DEFAULT 0.05,
    CONSTRAINT getnext_weights_sum_check CHECK (
        ABS((w_sla + w_skill + w_age + w_complexity + w_value + w_affinity + w_workload) - 1.0) < 0.001
    ),
    effective_from  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by      TEXT         NOT NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_getnext_weights_tenant_type_035 UNIQUE (tenant_id, case_type_code)
);

COMMENT ON TABLE getnext_weights IS
'Per-tenant, per-case-type scoring weights for GetNext algorithm. DB CHECK enforces sum=1.0.';

DROP TRIGGER IF EXISTS getnext_weights_updated_at_035 ON getnext_weights;
CREATE TRIGGER getnext_weights_updated_at_035
    BEFORE UPDATE ON getnext_weights
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) getnext_claims — append-only audit trail for every GetNext action
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS getnext_claims (
    claim_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID        NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    user_id           UUID        NOT NULL,
    case_id           UUID        REFERENCES cases(id) ON DELETE SET NULL,
    action            TEXT        NOT NULL
                      CHECK (action IN (
                          'CLAIMED', 'SKIPPED', 'PREVIEW',
                          'CAPACITY_BLOCKED', 'NO_ELIGIBLE_CASES'
                      )),
    composite_score   NUMERIC(8,4),
    score_breakdown   JSONB,
    -- { sla, skill, age, complexity, value, affinity, workload, weights_used }
    skip_reason       TEXT
                      CHECK (skip_reason IS NULL OR skip_reason IN (
                          'FREE_TEXT', 'CONFLICT_OF_INTEREST',
                          'TOO_COMPLEX', 'WRONG_SKILL', 'OTHER'
                      )),
    skip_notes        TEXT,
    queue_depth       INTEGER,
    user_active_cases INTEGER,
    claimed_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE getnext_claims IS
'Append-only audit of every GetNext action. Never update or delete rows.';

-- Append-only enforcement
CREATE OR REPLACE FUNCTION trg_getnext_claims_immutable_035()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'getnext_claims is append-only: % is not permitted', TG_OP;
END;
$$;

DROP TRIGGER IF EXISTS getnext_claims_no_update_delete_035 ON getnext_claims;
CREATE TRIGGER getnext_claims_no_update_delete_035
    BEFORE UPDATE OR DELETE ON getnext_claims
    FOR EACH ROW EXECUTE FUNCTION trg_getnext_claims_immutable_035();

CREATE INDEX IF NOT EXISTS idx_getnext_claims_user_035
    ON getnext_claims (tenant_id, user_id, claimed_at DESC);

CREATE INDEX IF NOT EXISTS idx_getnext_claims_case_035
    ON getnext_claims (tenant_id, case_id, claimed_at DESC)
    WHERE case_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_getnext_claims_session_skips_035
    ON getnext_claims (tenant_id, user_id, action, claimed_at DESC)
    WHERE action = 'SKIPPED';

-- ---------------------------------------------------------------------------
-- 5) case_allocation_transitions — tracks when each case entered ALLOCATION
--    stage. Used to compute Age_Score. Append-only, one row per transition.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_allocation_transitions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_id     UUID        NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    entered_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    exited_at   TIMESTAMPTZ,                 -- set when case leaves ALLOCATION
    is_current  BOOLEAN     NOT NULL DEFAULT TRUE,
    entered_by  TEXT        NOT NULL DEFAULT 'system'
);

COMMENT ON TABLE case_allocation_transitions IS
'Tracks when cases enter/exit the ALLOCATION stage for Age_Score computation.
 One row per allocation period. is_current=true means still awaiting pickup.';

-- Only one current row per case
CREATE UNIQUE INDEX IF NOT EXISTS uq_case_allocation_current_035
    ON case_allocation_transitions (case_id)
    WHERE is_current = TRUE;

CREATE INDEX IF NOT EXISTS idx_case_allocation_tenant_current_035
    ON case_allocation_transitions (tenant_id, is_current, entered_at ASC)
    WHERE is_current = TRUE;

-- ---------------------------------------------------------------------------
-- 6) case_user_affinity — materialised view for affinity scoring
--    Refresh every 5 minutes with CONCURRENTLY to avoid read blocking.
--    Scores reflect prior involvement of a user with an allocatable case.
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW IF NOT EXISTS case_user_affinity AS
SELECT
    c.tenant_id,
    c.id          AS case_id,
    u.user_id,
    CASE
        -- User was previously the direct assignee of this case
        WHEN c.assigned_user_id = u.user_id THEN 30
        -- User completed a task on this case  
        WHEN EXISTS (
            SELECT 1 FROM tasks t
            WHERE t.case_id = c.id
              AND t.assigned_user_id = u.user_id
              AND t.status = 'COMPLETED'
        ) THEN 20
        -- A teammate of the user completed a task on this case
        WHEN EXISTS (
            SELECT 1
            FROM tasks t
            JOIN team_members tm ON tm.user_id = t.assigned_user_id
                                 AND tm.tenant_id = u.tenant_id
            JOIN team_members my_tm ON my_tm.team_id = tm.team_id
                                    AND my_tm.user_id = u.user_id
            WHERE t.case_id = c.id
              AND t.status = 'COMPLETED'
        ) THEN 10
        ELSE 0
    END AS affinity_score
FROM cases c
CROSS JOIN users u
WHERE c.tenant_id = u.tenant_id
  AND c.status = 'ALLOCATABLE'
  AND c.assigned_user_id IS NULL
  AND u.status = 'ACTIVE';

COMMENT ON MATERIALIZED VIEW case_user_affinity IS
'Pre-computed affinity scores for user-case pairs. Refresh every 5 minutes.
 Use REFRESH MATERIALIZED VIEW CONCURRENTLY to avoid blocking reads.';

-- Required for CONCURRENTLY refresh
CREATE UNIQUE INDEX IF NOT EXISTS idx_case_user_affinity_pk_035
    ON case_user_affinity (tenant_id, case_id, user_id);

-- ---------------------------------------------------------------------------
-- 7) getnext_queue_snapshots — point-in-time queue analytics
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS getnext_queue_snapshots (
    snapshot_id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID        NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_type_code       TEXT        NOT NULL,
    captured_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    total_allocatable    INTEGER     NOT NULL DEFAULT 0,
    avg_wait_hours       NUMERIC(8,2),
    max_wait_hours       NUMERIC(8,2),
    sla_breached_count   INTEGER     NOT NULL DEFAULT 0,
    sla_at_risk_count    INTEGER     NOT NULL DEFAULT 0,
    skill_breakdown      JSONB,
    -- { "RESIDENTIAL_LENDING": 12, "SMSF_LENDING": 3, ... }
    complexity_breakdown JSONB
    -- { "SIMPLE": 8, "STANDARD_1": 4, ... }
);

CREATE INDEX IF NOT EXISTS idx_getnext_queue_snapshots_tenant_035
    ON getnext_queue_snapshots (tenant_id, captured_at DESC);
