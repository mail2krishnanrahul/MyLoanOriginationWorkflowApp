-- 000027_versioning_configuration_governance.down.sql
-- Rollback versioning/configuration governance objects while preserving data.

-- ---------------------------------------------------------------------------
-- 1) Archive immutable governance tables before dropping runtime objects
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF to_regclass('case_type_version_diffs') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS case_type_version_diffs_archive_000027 (LIKE case_type_version_diffs INCLUDING ALL)';
        EXECUTE '
            INSERT INTO case_type_version_diffs_archive_000027
            SELECT *
            FROM case_type_version_diffs
            ON CONFLICT (diff_id) DO NOTHING
        ';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('case_type_audit_log') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS case_type_audit_log_archive_000027 (LIKE case_type_audit_log INCLUDING ALL)';
        EXECUTE '
            INSERT INTO case_type_audit_log_archive_000027
            SELECT *
            FROM case_type_audit_log
            ON CONFLICT (audit_id) DO NOTHING
        ';
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS cases_case_type_version_pin_archive_000027 (
    case_id               UUID PRIMARY KEY,
    case_type_id          UUID NOT NULL,
    case_type_version_id  UUID NOT NULL,
    archived_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO cases_case_type_version_pin_archive_000027 (
    case_id,
    case_type_id,
    case_type_version_id
)
SELECT
    id,
    case_type_id,
    case_type_version_id
FROM cases
WHERE case_type_version_id IS NOT NULL
ON CONFLICT (case_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2) Drop append-only triggers/functions and indexes
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS case_type_audit_log_block_mutation_000027 ON case_type_audit_log;
DROP TRIGGER IF EXISTS case_type_version_diffs_block_mutation_000027 ON case_type_version_diffs;

DROP FUNCTION IF EXISTS trg_block_case_type_audit_log_mutations_000027();
DROP FUNCTION IF EXISTS trg_block_case_type_version_diffs_mutations_000027();

DROP INDEX IF EXISTS idx_case_type_audit_log_action_time_000027;
DROP INDEX IF EXISTS idx_case_type_audit_log_case_time_000027;
DROP INDEX IF EXISTS idx_case_type_version_diffs_code_to_version_000027;
DROP INDEX IF EXISTS idx_case_type_version_diffs_lookup_000027;
DROP INDEX IF EXISTS idx_cases_case_type_version_id_000027;
DROP INDEX IF EXISTS idx_case_types_one_active_per_code_000027;

-- ---------------------------------------------------------------------------
-- 3) Drop governance tables
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS case_type_audit_log;
DROP TABLE IF EXISTS case_type_version_diffs;

-- ---------------------------------------------------------------------------
-- 4) Remove explicit case pinning column (data already archived)
-- ---------------------------------------------------------------------------
ALTER TABLE cases
    DROP CONSTRAINT IF EXISTS chk_cases_case_type_pin_consistency_000027;
ALTER TABLE cases
    DROP CONSTRAINT IF EXISTS fk_cases_case_type_version_id;

DROP TRIGGER IF EXISTS cases_sync_case_type_version_id_000027 ON cases;
DROP FUNCTION IF EXISTS trg_sync_case_type_version_id_000027();

ALTER TABLE cases
    DROP COLUMN IF EXISTS case_type_version_id;

-- ---------------------------------------------------------------------------
-- 5) Remove lifecycle metadata extensions and version-sequence trigger
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS case_types_enforce_version_sequence_000027 ON case_types;
DROP FUNCTION IF EXISTS trg_enforce_case_type_version_sequence_000027();

ALTER TABLE case_types
    DROP COLUMN IF EXISTS deprecated_by,
    DROP COLUMN IF EXISTS activated_by,
    DROP COLUMN IF EXISTS activated_at;
