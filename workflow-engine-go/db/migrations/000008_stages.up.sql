-- 008-stages.sql
-- Design-time stage definitions and runtime stage-transition audit log.

-- ===================================================================
-- 1. stage_definitions — one row per stage per case_type version
-- ===================================================================
CREATE TABLE IF NOT EXISTS stage_definitions (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_type_id        UUID            NOT NULL REFERENCES case_types(id) ON DELETE CASCADE,
    case_type_version   INT             NOT NULL,
    stage_code          VARCHAR(100)    NOT NULL,
    stage_name          VARCHAR(255)    NOT NULL,
    ordinal             INT             NOT NULL,          -- 1-based, determines order
    stage_type          VARCHAR(20)     NOT NULL DEFAULT 'NORMAL'
                            CHECK (stage_type IN ('ENTRY', 'NORMAL', 'EXIT', 'TERMINAL')),
    description         TEXT,
    is_skippable        BOOLEAN         NOT NULL DEFAULT false,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),

    -- One stage_code per case_type version
    CONSTRAINT uq_stage_def_code UNIQUE (case_type_id, case_type_version, stage_code),
    -- One ordinal per case_type version
    CONSTRAINT uq_stage_def_ordinal UNIQUE (case_type_id, case_type_version, ordinal)
);

-- Fast lookup: all stages for a given case_type version, ordered
CREATE INDEX IF NOT EXISTS idx_stage_defs_type_version
    ON stage_definitions (case_type_id, case_type_version, ordinal);

-- ===================================================================
-- 2. case_stage_transitions — runtime audit trail
-- ===================================================================
CREATE TABLE IF NOT EXISTS case_stage_transitions (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id             UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,

    -- Source (nullable for initial transition into the first stage)
    from_stage_code     VARCHAR(100),
    from_stage_ordinal  INT,

    -- Target
    to_stage_code       VARCHAR(100)    NOT NULL,
    to_stage_ordinal    INT             NOT NULL,

    -- Regression tracking
    is_regression       BOOLEAN         NOT NULL DEFAULT false,
    regression_reason   TEXT,

    -- Audit
    transitioned_at     TIMESTAMPTZ     NOT NULL DEFAULT now(),
    triggered_by        TEXT            NOT NULL,          -- service name or user ID

    -- If regression, reason is required
    CONSTRAINT chk_regression_reason
        CHECK (is_regression = false OR regression_reason IS NOT NULL)
);

-- Recent transitions for a case (dashboard, timeline view)
CREATE INDEX IF NOT EXISTS idx_stage_transitions_case
    ON case_stage_transitions (case_id, transitioned_at DESC);

-- Regression analysis queries
CREATE INDEX IF NOT EXISTS idx_stage_transitions_regression
    ON case_stage_transitions (is_regression)
    WHERE is_regression = true;
