-- 000031_scim_user_management.up.sql
--
-- SCIM 2.0 user/group provisioning support.
-- Non-obvious design decisions:
-- 1) Raw SCIM bearer tokens are never persisted; only SHA-256 token hashes are stored.
-- 2) users.version and teams.version are introduced for SCIM ETag / optimistic concurrency.
-- 3) teams.external_id is nullable and unique per-tenant for IdP correlation.
-- 4) All SCIM operational indexes lead with tenant_id or token_id for tenant-pruned scans.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;

-- ---------------------------------------------------------------------------
-- 1) Version columns used for SCIM ETag handling
-- ---------------------------------------------------------------------------
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS version INT;

UPDATE users
SET version = 1
WHERE version IS NULL OR version <= 0;

ALTER TABLE users
    ALTER COLUMN version SET DEFAULT 1,
    ALTER COLUMN version SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_users_version_positive_000031'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT chk_users_version_positive_000031
            CHECK (version > 0);
    END IF;
END;
$$;

ALTER TABLE teams
    ADD COLUMN IF NOT EXISTS version INT;

UPDATE teams
SET version = 1
WHERE version IS NULL OR version <= 0;

ALTER TABLE teams
    ALTER COLUMN version SET DEFAULT 1,
    ALTER COLUMN version SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_teams_version_positive_000031'
    ) THEN
        ALTER TABLE teams
            ADD CONSTRAINT chk_teams_version_positive_000031
            CHECK (version > 0);
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION trg_bump_version_000031()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.version IS NULL OR NEW.version <= OLD.version THEN
            NEW.version := OLD.version + 1;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS users_version_bump_000031 ON users;
CREATE TRIGGER users_version_bump_000031
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION trg_bump_version_000031();

DROP TRIGGER IF EXISTS teams_version_bump_000031 ON teams;
CREATE TRIGGER teams_version_bump_000031
    BEFORE UPDATE ON teams
    FOR EACH ROW
    EXECUTE FUNCTION trg_bump_version_000031();

COMMENT ON COLUMN users.version IS
'Optimistic lock / SCIM ETag source. Auto-incremented on each update.';
COMMENT ON COLUMN teams.version IS
'Optimistic lock / SCIM Group ETag source. Auto-incremented on each update.';

-- ---------------------------------------------------------------------------
-- 2) Team external identity correlation for SCIM Group.externalId
-- ---------------------------------------------------------------------------
ALTER TABLE teams
    ADD COLUMN IF NOT EXISTS external_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_teams_tenant_external_id_000031
    ON teams (tenant_id, external_id)
    WHERE external_id IS NOT NULL;

COMMENT ON COLUMN teams.external_id IS
'External IdP group identifier used to correlate SCIM Group resources.';

-- ---------------------------------------------------------------------------
-- 3) SCIM bearer token registry
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS scim_tokens (
    token_id       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    token_hash     TEXT         NOT NULL,
    description    TEXT         NOT NULL,
    scopes         TEXT[]       NOT NULL DEFAULT '{}',
    status         VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE'
                  CHECK (status IN ('ACTIVE', 'REVOKED')),
    metadata       JSONB        NOT NULL DEFAULT '{}',
    last_used_at   TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ,
    created_by     TEXT         NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_scim_tokens_token_hash_000031 UNIQUE (token_hash)
);

COMMENT ON TABLE scim_tokens IS
'Tenant-scoped SCIM bearer token registry. Only SHA-256 hashes are persisted.';
COMMENT ON COLUMN scim_tokens.token_hash IS
'SHA-256 hash of the raw bearer token. Raw token is never stored.';
COMMENT ON COLUMN scim_tokens.metadata IS
'Per-token configuration such as max_requests_per_minute overrides.';

DROP TRIGGER IF EXISTS scim_tokens_updated_at_000031 ON scim_tokens;
CREATE TRIGGER scim_tokens_updated_at_000031
    BEFORE UPDATE ON scim_tokens
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

CREATE INDEX IF NOT EXISTS idx_scim_tokens_tenant_status_000031
    ON scim_tokens (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_scim_tokens_tenant_active_000031
    ON scim_tokens (tenant_id, token_id)
    WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_scim_tokens_tenant_expiry_000031
    ON scim_tokens (tenant_id, expires_at)
    WHERE status = 'ACTIVE';

-- ---------------------------------------------------------------------------
-- 4) Per-token SCIM minute-window rate counters
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS scim_token_rate_limit_counters (
    token_id        UUID        NOT NULL REFERENCES scim_tokens(token_id) ON DELETE CASCADE,
    window_start    TIMESTAMPTZ NOT NULL,
    request_count   INT         NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (token_id, window_start)
);

COMMENT ON TABLE scim_token_rate_limit_counters IS
'Durable minute-level SCIM token counters used for distributed rate limiting.';

DROP TRIGGER IF EXISTS scim_token_rate_limit_counters_updated_at_000031 ON scim_token_rate_limit_counters;
CREATE TRIGGER scim_token_rate_limit_counters_updated_at_000031
    BEFORE UPDATE ON scim_token_rate_limit_counters
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

CREATE INDEX IF NOT EXISTS idx_scim_token_rate_limit_counters_token_window_000031
    ON scim_token_rate_limit_counters (token_id, window_start DESC);

CREATE INDEX IF NOT EXISTS idx_scim_token_rate_limit_counters_window_cleanup_000031
    ON scim_token_rate_limit_counters (window_start)
    WHERE request_count > 0;

-- ---------------------------------------------------------------------------
-- 5) SCIM audit log (append only)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS scim_audit_log (
    audit_id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    token_id             UUID         REFERENCES scim_tokens(token_id) ON DELETE SET NULL,
    operation            VARCHAR(10)  NOT NULL
                       CHECK (operation IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'BULK')),
    resource_type        VARCHAR(40)  NOT NULL
                       CHECK (resource_type IN ('USER', 'GROUP', 'SCHEMA', 'RESOURCE_TYPE', 'SERVICE_PROVIDER_CONFIG')),
    resource_id          TEXT,
    http_status          INT          NOT NULL,
    filter_expression    TEXT,
    request_attributes   TEXT[]       NOT NULL DEFAULT '{}',
    operations_count     INT          NOT NULL DEFAULT 0 CHECK (operations_count >= 0),
    duration_ms          INT          NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    ip_address           TEXT,
    user_agent           TEXT,
    occurred_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE scim_audit_log IS
'Best-effort append-only SCIM access and mutation audit trail.';

CREATE INDEX IF NOT EXISTS idx_scim_audit_log_tenant_occurred_000031
    ON scim_audit_log (tenant_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_scim_audit_log_tenant_token_occurred_000031
    ON scim_audit_log (tenant_id, token_id, occurred_at DESC)
    WHERE token_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_scim_audit_log_tenant_operation_occurred_000031
    ON scim_audit_log (tenant_id, operation, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_scim_audit_log_tenant_resource_lookup_000031
    ON scim_audit_log (tenant_id, resource_type, resource_id, occurred_at DESC);

-- ---------------------------------------------------------------------------
-- 6) SCIM query-support indexes
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_users_tenant_username_sort_000031
    ON users (tenant_id, LOWER(username));

CREATE INDEX IF NOT EXISTS idx_users_tenant_display_name_sort_000031
    ON users (tenant_id, display_name);

CREATE INDEX IF NOT EXISTS idx_users_tenant_email_sort_000031
    ON users (tenant_id, LOWER(email));

CREATE INDEX IF NOT EXISTS idx_users_tenant_display_email_trgm_000031
    ON users USING GIN (tenant_id, display_name gin_trgm_ops, email gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_teams_tenant_display_name_sort_000031
    ON teams (tenant_id, display_name);

CREATE INDEX IF NOT EXISTS idx_teams_tenant_external_id_lookup_000031
    ON teams (tenant_id, external_id)
    WHERE external_id IS NOT NULL;
