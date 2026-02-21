-- 000031_scim_user_management.down.sql
--
-- Roll back SCIM user/group provisioning support.
-- Data preservation policy:
-- 1) SCIM operational tables are archived before drop.
-- 2) teams.external_id values are archived before column drop.
-- 3) users.version and teams.version values are archived before column drop.

-- ---------------------------------------------------------------------------
-- 0) Preserve data snapshots before structural rollback
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS scim_tokens_archive_000031 AS
SELECT * FROM scim_tokens WITH NO DATA;

INSERT INTO scim_tokens_archive_000031
SELECT * FROM scim_tokens;

CREATE TABLE IF NOT EXISTS scim_token_rate_limit_counters_archive_000031 AS
SELECT * FROM scim_token_rate_limit_counters WITH NO DATA;

INSERT INTO scim_token_rate_limit_counters_archive_000031
SELECT * FROM scim_token_rate_limit_counters;

CREATE TABLE IF NOT EXISTS scim_audit_log_archive_000031 AS
SELECT * FROM scim_audit_log WITH NO DATA;

INSERT INTO scim_audit_log_archive_000031
SELECT * FROM scim_audit_log;

CREATE TABLE IF NOT EXISTS teams_external_id_archive_000031 (
    team_id     UUID,
    tenant_id   UUID,
    external_id TEXT,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO teams_external_id_archive_000031 (team_id, tenant_id, external_id)
SELECT team_id, tenant_id, external_id
FROM teams
WHERE external_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS users_version_archive_000031 (
    user_id     UUID,
    tenant_id   UUID,
    version     INT,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO users_version_archive_000031 (user_id, tenant_id, version)
SELECT user_id, tenant_id, version
FROM users;

CREATE TABLE IF NOT EXISTS teams_version_archive_000031 (
    team_id     UUID,
    tenant_id   UUID,
    version     INT,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO teams_version_archive_000031 (team_id, tenant_id, version)
SELECT team_id, tenant_id, version
FROM teams;

-- ---------------------------------------------------------------------------
-- 1) Drop SCIM query indexes
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_teams_tenant_external_id_lookup_000031;
DROP INDEX IF EXISTS idx_teams_tenant_display_name_sort_000031;
DROP INDEX IF EXISTS idx_users_tenant_display_email_trgm_000031;
DROP INDEX IF EXISTS idx_users_tenant_email_sort_000031;
DROP INDEX IF EXISTS idx_users_tenant_display_name_sort_000031;
DROP INDEX IF EXISTS idx_users_tenant_username_sort_000031;

-- ---------------------------------------------------------------------------
-- 2) Drop SCIM audit and token tables (dependency-safe order)
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_scim_audit_log_tenant_resource_lookup_000031;
DROP INDEX IF EXISTS idx_scim_audit_log_tenant_operation_occurred_000031;
DROP INDEX IF EXISTS idx_scim_audit_log_tenant_token_occurred_000031;
DROP INDEX IF EXISTS idx_scim_audit_log_tenant_occurred_000031;
DROP TABLE IF EXISTS scim_audit_log;

DROP INDEX IF EXISTS idx_scim_token_rate_limit_counters_window_cleanup_000031;
DROP INDEX IF EXISTS idx_scim_token_rate_limit_counters_token_window_000031;
DROP TRIGGER IF EXISTS scim_token_rate_limit_counters_updated_at_000031 ON scim_token_rate_limit_counters;
DROP TABLE IF EXISTS scim_token_rate_limit_counters;

DROP INDEX IF EXISTS idx_scim_tokens_tenant_expiry_000031;
DROP INDEX IF EXISTS idx_scim_tokens_tenant_active_000031;
DROP INDEX IF EXISTS idx_scim_tokens_tenant_status_000031;
DROP TRIGGER IF EXISTS scim_tokens_updated_at_000031 ON scim_tokens;
DROP TABLE IF EXISTS scim_tokens;

-- ---------------------------------------------------------------------------
-- 3) Revert team external correlation field
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS uq_teams_tenant_external_id_000031;
ALTER TABLE teams DROP COLUMN IF EXISTS external_id;

-- ---------------------------------------------------------------------------
-- 4) Revert SCIM ETag versioning columns and triggers
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS users_version_bump_000031 ON users;
DROP TRIGGER IF EXISTS teams_version_bump_000031 ON teams;
DROP FUNCTION IF EXISTS trg_bump_version_000031();

ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_version_positive_000031;
ALTER TABLE teams DROP CONSTRAINT IF EXISTS chk_teams_version_positive_000031;

ALTER TABLE users DROP COLUMN IF EXISTS version;
ALTER TABLE teams DROP COLUMN IF EXISTS version;
