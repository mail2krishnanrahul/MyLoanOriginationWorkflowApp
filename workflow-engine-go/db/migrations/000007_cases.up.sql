-- 007-cases.sql
-- Runtime instance of a case_type blueprint.
-- A case tracks a single loan application (or sub-process like CREDIT_CHECK)
-- through its stage progression.

-- Sequence for human-readable reference numbers (LOAN-2025-00001)
CREATE SEQUENCE IF NOT EXISTS case_ref_seq START 1 INCREMENT 1;

-- -------------------------------------------------------------------
-- cases table
-- -------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS cases (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    reference_number        VARCHAR(30)     NOT NULL UNIQUE,

    -- Blueprint link
    -- Each case_types row has a unique UUID (one per code+version),
    -- so we FK to case_types(id) directly.
    -- case_type_version is denormalized for query convenience.
    case_type_id            UUID            NOT NULL REFERENCES case_types(id),
    case_type_version       INT             NOT NULL,

    -- Hierarchy: CREDIT_CHECK is a child of HOME_LOAN
    parent_case_id          UUID            REFERENCES cases(id) ON DELETE SET NULL,

    -- Stage progression
    current_stage_code      VARCHAR(100),
    current_stage_ordinal   INT             NOT NULL DEFAULT 1,

    -- Lifecycle
    status                  VARCHAR(20)     NOT NULL DEFAULT 'OPEN'
                                CHECK (status IN (
                                    'OPEN',
                                    'IN_PROGRESS',
                                    'COMPLETED',
                                    'CANCELLED',
                                    'SUSPENDED'
                                )),

    -- Flexible bag: borrower_id, product_id, channel, officer_id, etc.
    metadata                JSONB           NOT NULL DEFAULT '{}',

    -- External assignment (user or team reference)
    assigned_to             TEXT,

    -- Optimistic locking
    row_version             INT             NOT NULL DEFAULT 1,

    -- Timestamps
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    completed_at            TIMESTAMPTZ
);

-- -------------------------------------------------------------------
-- Indexes
-- -------------------------------------------------------------------

-- Sub-case lookups (find all children of a parent case)
CREATE INDEX IF NOT EXISTS idx_cases_parent
    ON cases (parent_case_id)
    WHERE parent_case_id IS NOT NULL;

-- Dashboard queries: active cases sorted by creation time
CREATE INDEX IF NOT EXISTS idx_cases_status_created
    ON cases (status, created_at DESC);

-- Stage ordinal queries (e.g. "all cases in stage 2")
CREATE INDEX IF NOT EXISTS idx_cases_stage_ordinal
    ON cases (current_stage_ordinal);

-- Reference number lookup (already UNIQUE, but explicit for clarity)
-- The UNIQUE constraint creates an implicit index, so this is not needed.
-- Kept as a comment for documentation.
-- CREATE INDEX idx_cases_ref_number ON cases (reference_number);

-- Case type lookup (find all cases of a given type)
CREATE INDEX IF NOT EXISTS idx_cases_case_type
    ON cases (case_type_id, case_type_version);

-- -------------------------------------------------------------------
-- Auto-generate reference_number: LOAN-YYYY-NNNNN
-- -------------------------------------------------------------------
CREATE OR REPLACE FUNCTION trg_generate_case_ref()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.reference_number IS NULL OR NEW.reference_number = '' THEN
        NEW.reference_number := 'LOAN-'
            || to_char(now(), 'YYYY') || '-'
            || lpad(nextval('case_ref_seq')::text, 5, '0');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS cases_generate_ref ON cases;
CREATE TRIGGER cases_generate_ref
    BEFORE INSERT ON cases
    FOR EACH ROW
    EXECUTE FUNCTION trg_generate_case_ref();

-- -------------------------------------------------------------------
-- Auto-update updated_at on modification
-- -------------------------------------------------------------------
-- Reuses trg_set_updated_at() from 006-case-types.sql if it exists,
-- otherwise creates it.
CREATE OR REPLACE FUNCTION trg_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS cases_updated_at ON cases;
CREATE TRIGGER cases_updated_at
    BEFORE UPDATE ON cases
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- -------------------------------------------------------------------
-- Add delayed foreign key for component_instances (from 000005)
-- -------------------------------------------------------------------
ALTER TABLE component_instances
    ADD CONSTRAINT fk_component_instances_cases
    FOREIGN KEY (case_id) REFERENCES cases(id) ON DELETE CASCADE;
