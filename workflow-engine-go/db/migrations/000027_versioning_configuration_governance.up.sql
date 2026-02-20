-- 000027_versioning_configuration_governance.up.sql
-- Versioning & configuration governance capability:
-- lifecycle gating, immutable audit trails, stored version diffs,
-- and explicit case pinning to a concrete case_type version row.

-- ---------------------------------------------------------------------------
-- 1) Extend case_types lifecycle metadata
-- ---------------------------------------------------------------------------
ALTER TABLE case_types
    ADD COLUMN IF NOT EXISTS activated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS activated_by TEXT,
    ADD COLUMN IF NOT EXISTS deprecated_by TEXT;

COMMENT ON COLUMN case_types.activated_at IS
'UTC timestamp when this version was activated.';
COMMENT ON COLUMN case_types.activated_by IS
'Actor identity (user/service) that activated this case_type version.';
COMMENT ON COLUMN case_types.deprecated_by IS
'Actor identity (user/service) that deprecated this case_type version.';

-- Defensive duplicate of the active-version uniqueness rule.
CREATE UNIQUE INDEX IF NOT EXISTS idx_case_types_one_active_per_code_000027
    ON case_types (code)
    WHERE status = 'ACTIVE';

-- Enforce monotonic contiguous version assignment per case_type_code.
CREATE OR REPLACE FUNCTION trg_enforce_case_type_version_sequence_000027()
RETURNS TRIGGER AS $$
DECLARE
    expected_next INT;
BEGIN
    SELECT COALESCE(MAX(version), 0) + 1
      INTO expected_next
      FROM case_types
     WHERE code = NEW.code;

    IF NEW.version IS NULL OR NEW.version <= 0 THEN
        NEW.version := expected_next;
    ELSIF NEW.version <> expected_next THEN
        RAISE EXCEPTION
            'case_types version must be contiguous for code % (expected %, got %)',
            NEW.code, expected_next, NEW.version;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS case_types_enforce_version_sequence_000027 ON case_types;
CREATE TRIGGER case_types_enforce_version_sequence_000027
    BEFORE INSERT ON case_types
    FOR EACH ROW
    EXECUTE FUNCTION trg_enforce_case_type_version_sequence_000027();

-- ---------------------------------------------------------------------------
-- 2) Explicit case pinning column (non-breaking alongside existing case_type_id)
-- ---------------------------------------------------------------------------
ALTER TABLE cases
    ADD COLUMN IF NOT EXISTS case_type_version_id UUID;

-- Backfill existing rows from the already-pinned case_type_id.
UPDATE cases
   SET case_type_version_id = case_type_id
 WHERE case_type_version_id IS NULL;

-- Keep both columns aligned for backwards compatibility with existing write paths.
CREATE OR REPLACE FUNCTION trg_sync_case_type_version_id_000027()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.case_type_version_id IS NULL THEN
        NEW.case_type_version_id := NEW.case_type_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS cases_sync_case_type_version_id_000027 ON cases;
CREATE TRIGGER cases_sync_case_type_version_id_000027
    BEFORE INSERT OR UPDATE OF case_type_id, case_type_version_id ON cases
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_case_type_version_id_000027();

ALTER TABLE cases
    ALTER COLUMN case_type_version_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'fk_cases_case_type_version_id'
    ) THEN
        ALTER TABLE cases
            ADD CONSTRAINT fk_cases_case_type_version_id
                FOREIGN KEY (case_type_version_id)
                REFERENCES case_types(id)
                ON DELETE RESTRICT;
    END IF;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'chk_cases_case_type_pin_consistency_000027'
    ) THEN
        ALTER TABLE cases
            ADD CONSTRAINT chk_cases_case_type_pin_consistency_000027
                CHECK (case_type_id = case_type_version_id);
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_cases_case_type_version_id_000027
    ON cases (case_type_version_id);

COMMENT ON COLUMN cases.case_type_version_id IS
'Pinned case_type version row for immutable config resolution throughout case lifetime.';

-- ---------------------------------------------------------------------------
-- 3) Stored immutable diffs between case_type versions
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_type_version_diffs (
    diff_id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    case_type_code        VARCHAR(100) NOT NULL,
    from_case_type_id     UUID         NOT NULL REFERENCES case_types(id) ON DELETE RESTRICT,
    to_case_type_id       UUID         NOT NULL REFERENCES case_types(id) ON DELETE RESTRICT,
    from_version          INT          NOT NULL CHECK (from_version > 0),
    to_version            INT          NOT NULL CHECK (to_version > 0),
    diff_json             JSONB        NOT NULL DEFAULT '{}',
    computed_by           TEXT         NOT NULL,
    computed_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    archived_at           TIMESTAMPTZ,
    CONSTRAINT uq_case_type_version_diffs_pair UNIQUE (from_case_type_id, to_case_type_id),
    CONSTRAINT chk_case_type_version_diffs_not_same CHECK (from_case_type_id <> to_case_type_id)
);

COMMENT ON TABLE case_type_version_diffs IS
'Immutable stored diffs between case_type versions, computed once at activation time.';
COMMENT ON COLUMN case_type_version_diffs.diff_json IS
'Structured human-readable diff payload (stages/activities/tasks/retry/metadata changes).';

CREATE INDEX IF NOT EXISTS idx_case_type_version_diffs_lookup_000027
    ON case_type_version_diffs (from_case_type_id, to_case_type_id);

CREATE INDEX IF NOT EXISTS idx_case_type_version_diffs_code_to_version_000027
    ON case_type_version_diffs (case_type_code, to_version DESC);

CREATE OR REPLACE FUNCTION trg_block_case_type_version_diffs_mutations_000027()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'case_type_version_diffs is immutable; updates/deletes are not allowed';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS case_type_version_diffs_block_mutation_000027 ON case_type_version_diffs;
CREATE TRIGGER case_type_version_diffs_block_mutation_000027
    BEFORE UPDATE OR DELETE ON case_type_version_diffs
    FOR EACH ROW
    EXECUTE FUNCTION trg_block_case_type_version_diffs_mutations_000027();

-- ---------------------------------------------------------------------------
-- 4) Append-only case_type audit trail
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_type_audit_log (
    audit_id         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    case_type_id     UUID         NOT NULL REFERENCES case_types(id) ON DELETE RESTRICT,
    action           VARCHAR(40)  NOT NULL
                        CHECK (action IN (
                            'CREATED',
                            'CONFIG_UPDATED',
                            'ACTIVATED',
                            'DEPRECATED',
                            'VALIDATION_FAILED',
                            'CASE_MIGRATED'
                        )),
    actor            TEXT         NOT NULL,
    changed_fields   JSONB,
    previous_value   JSONB,
    new_value        JSONB,
    occurred_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    archived_at      TIMESTAMPTZ
);

COMMENT ON TABLE case_type_audit_log IS
'Append-only audit trail for every case_type mutation and governance decision.';
COMMENT ON COLUMN case_type_audit_log.changed_fields IS
'JSON array/object describing changed paths or migration from/to version metadata.';

CREATE INDEX IF NOT EXISTS idx_case_type_audit_log_case_time_000027
    ON case_type_audit_log (case_type_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_case_type_audit_log_action_time_000027
    ON case_type_audit_log (action, occurred_at DESC);

CREATE OR REPLACE FUNCTION trg_block_case_type_audit_log_mutations_000027()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'case_type_audit_log is append-only; updates/deletes are not allowed';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS case_type_audit_log_block_mutation_000027 ON case_type_audit_log;
CREATE TRIGGER case_type_audit_log_block_mutation_000027
    BEFORE UPDATE OR DELETE ON case_type_audit_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_block_case_type_audit_log_mutations_000027();
