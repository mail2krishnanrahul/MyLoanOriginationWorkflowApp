-- 006-case-types.sql
-- Versioned blueprint table for case types (HOME_LOAN, SME_CREDIT, etc.)
-- Each row is one version of a definition; the full stage→activity→task
-- tree lives in the JSONB config column.

CREATE TABLE IF NOT EXISTS case_types (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    code            VARCHAR(100)    NOT NULL,
    version         INT             NOT NULL DEFAULT 1,
    name            VARCHAR(255)    NOT NULL,
    description     TEXT,
    config          JSONB           NOT NULL DEFAULT '{}',
    status          VARCHAR(20)     NOT NULL DEFAULT 'DRAFT'
                        CHECK (status IN ('DRAFT', 'ACTIVE', 'DEPRECATED')),
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    deprecated_at   TIMESTAMPTZ,

    -- One version number per code
    CONSTRAINT uq_case_types_code_version UNIQUE (code, version)
);

-- At most one ACTIVE version per code
CREATE UNIQUE INDEX IF NOT EXISTS idx_case_types_active_code
    ON case_types (code)
    WHERE status = 'ACTIVE';

-- Fast lookup by status (e.g. find all ACTIVE definitions)
CREATE INDEX IF NOT EXISTS idx_case_types_status
    ON case_types (status);

-- GIN index for @> containment queries into the config blob
CREATE INDEX IF NOT EXISTS idx_case_types_config
    ON case_types USING GIN (config jsonb_path_ops);

-- Auto-update updated_at on row modification
CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS case_types_updated_at ON case_types;
CREATE TRIGGER case_types_updated_at
    BEFORE UPDATE ON case_types
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();
