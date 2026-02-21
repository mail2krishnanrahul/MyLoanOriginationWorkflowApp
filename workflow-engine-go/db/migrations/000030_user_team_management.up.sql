-- 000030_user_team_management.up.sql
--
-- Enterprise user and team management.
-- Non-obvious design decisions:
-- 1) We evolve the legacy users table in place to avoid breaking approval routing code paths.
-- 2) Legacy users.id/text is retained as a compatibility key, while users.user_id/uuid becomes the primary key.
-- 3) All operational indexes lead with tenant_id to preserve tenant pruning in shared-table multi-tenancy.
-- 4) Team hierarchy max depth is enforced in Go service logic (business-rule rich), not as a rigid DB CHECK.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;

-- ---------------------------------------------------------------------------
-- 1) Evolve legacy users directory into tenant-scoped enterprise user registry
-- ---------------------------------------------------------------------------
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS user_id UUID,
    ADD COLUMN IF NOT EXISTS tenant_id UUID,
    ADD COLUMN IF NOT EXISTS username TEXT,
    ADD COLUMN IF NOT EXISTS email TEXT,
    ADD COLUMN IF NOT EXISTS display_name TEXT,
    ADD COLUMN IF NOT EXISTS auth_provider VARCHAR(20),
    ADD COLUMN IF NOT EXISTS external_id TEXT,
    ADD COLUMN IF NOT EXISTS timezone TEXT,
    ADD COLUMN IF NOT EXISTS locale TEXT,
    ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS metadata JSONB;

-- Backfill user_id using legacy id when it is UUID-shaped; otherwise generate.
UPDATE users
SET user_id = CASE
    WHEN id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' THEN id::uuid
    ELSE gen_random_uuid()
END
WHERE user_id IS NULL;

UPDATE users
SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
WHERE tenant_id IS NULL;

UPDATE users
SET
    display_name = COALESCE(NULLIF(display_name, ''), NULLIF(full_name, ''), id),
    username = COALESCE(NULLIF(username, ''), lower(id)),
    email = COALESCE(NULLIF(email, ''), lower(id) || '@local.invalid'),
    auth_provider = COALESCE(NULLIF(auth_provider, ''), 'LOCAL'),
    timezone = COALESCE(NULLIF(timezone, ''), 'UTC'),
    locale = COALESCE(NULLIF(locale, ''), 'en-US'),
    metadata = COALESCE(metadata, '{}'::jsonb),
    full_name = COALESCE(NULLIF(full_name, ''), COALESCE(NULLIF(display_name, ''), id)),
    role_code = COALESCE(NULLIF(role_code, ''), 'USER'),
    status = CASE WHEN status = 'INACTIVE' THEN 'SUSPENDED' ELSE status END;

-- Canonicalize legacy id to user_id text for compatibility queries.
UPDATE users
SET id = user_id::text
WHERE id IS DISTINCT FROM user_id::text;

ALTER TABLE users
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN user_id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN tenant_id SET NOT NULL,
    ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid,
    ALTER COLUMN username SET NOT NULL,
    ALTER COLUMN email SET NOT NULL,
    ALTER COLUMN display_name SET NOT NULL,
    ALTER COLUMN auth_provider SET NOT NULL,
    ALTER COLUMN auth_provider SET DEFAULT 'LOCAL',
    ALTER COLUMN timezone SET NOT NULL,
    ALTER COLUMN timezone SET DEFAULT 'UTC',
    ALTER COLUMN locale SET NOT NULL,
    ALTER COLUMN locale SET DEFAULT 'en-US',
    ALTER COLUMN metadata SET NOT NULL,
    ALTER COLUMN metadata SET DEFAULT '{}'::jsonb,
    ALTER COLUMN full_name SET NOT NULL,
    ALTER COLUMN role_code SET NOT NULL,
    ALTER COLUMN role_code SET DEFAULT 'USER';

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_status_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_pkey;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_manager_id_fkey;
ALTER TABLE users DROP CONSTRAINT IF EXISTS fk_users_tenant_id_000030;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_auth_provider_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS uq_users_legacy_id_000030;

ALTER TABLE users
    ADD CONSTRAINT users_pkey PRIMARY KEY (user_id),
    ADD CONSTRAINT fk_users_tenant_id_000030
        FOREIGN KEY (tenant_id)
        REFERENCES tenants(tenant_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT users_status_check
        CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DEACTIVATED')),
    ADD CONSTRAINT users_auth_provider_check
        CHECK (auth_provider IN ('LOCAL', 'OIDC', 'SAML')),
    ADD CONSTRAINT uq_users_legacy_id_000030 UNIQUE (id),
    ADD CONSTRAINT users_manager_id_fkey
        FOREIGN KEY (manager_id)
        REFERENCES users(id)
        ON DELETE SET NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_lower_username_000030
    ON users (tenant_id, LOWER(username));

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_lower_email_000030
    ON users (tenant_id, LOWER(email));

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_external_id_000030
    ON users (tenant_id, external_id)
    WHERE external_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_user_id_000030
    ON users (tenant_id, user_id);

DROP INDEX IF EXISTS idx_users_role_status;
DROP INDEX IF EXISTS idx_users_manager;

CREATE INDEX IF NOT EXISTS idx_users_tenant_status_000030
    ON users (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_users_tenant_active_000030
    ON users (tenant_id, updated_at DESC)
    WHERE status = 'ACTIVE';

-- Keep legacy approval-role lookups tenant-pruned.
CREATE INDEX IF NOT EXISTS idx_users_tenant_role_status_legacy_000030
    ON users (tenant_id, role_code, status)
    WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_users_tenant_manager_legacy_000030
    ON users (tenant_id, manager_id)
    WHERE manager_id IS NOT NULL;

-- Tenant-aware search index for ILIKE on display_name/email.
CREATE INDEX IF NOT EXISTS idx_users_tenant_search_000030
    ON users
    USING GIN (
        tenant_id,
        (COALESCE(display_name, '') || ' ' || COALESCE(email, '')) gin_trgm_ops
    );

COMMENT ON COLUMN users.user_id IS
'Canonical immutable UUID identity for user/team management.';
COMMENT ON COLUMN users.id IS
'Legacy compatibility identifier retained for approval routing; synchronized to user_id::text.';
COMMENT ON COLUMN users.external_id IS
'Identity provider subject/nameID used to correlate OIDC/SAML principals.';
COMMENT ON COLUMN users.metadata IS
'Tenant-extensible user attributes (employee_id, branch_code, cost_center, etc.).';

CREATE OR REPLACE FUNCTION trg_sync_users_identity_000030()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.user_id IS NULL THEN
        IF NEW.id IS NOT NULL
           AND NEW.id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' THEN
            NEW.user_id := NEW.id::uuid;
        ELSE
            NEW.user_id := gen_random_uuid();
        END IF;
    END IF;

    NEW.id := NEW.user_id::text;
    NEW.display_name := COALESCE(NULLIF(NEW.display_name, ''), NULLIF(NEW.full_name, ''), NEW.id);
    NEW.full_name := COALESCE(NULLIF(NEW.full_name, ''), NEW.display_name);
    NEW.username := COALESCE(NULLIF(NEW.username, ''), lower(NEW.id));
    NEW.email := COALESCE(NULLIF(NEW.email, ''), lower(NEW.id) || '@local.invalid');
    NEW.role_code := COALESCE(NULLIF(NEW.role_code, ''), 'USER');
    NEW.timezone := COALESCE(NULLIF(NEW.timezone, ''), 'UTC');
    NEW.locale := COALESCE(NULLIF(NEW.locale, ''), 'en-US');
    NEW.auth_provider := COALESCE(NULLIF(NEW.auth_provider, ''), 'LOCAL');
    NEW.metadata := COALESCE(NEW.metadata, '{}'::jsonb);
    NEW.tenant_id := COALESCE(NEW.tenant_id, '00000000-0000-0000-0000-000000000000'::uuid);

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS users_identity_sync_000030 ON users;
CREATE TRIGGER users_identity_sync_000030
    BEFORE INSERT OR UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_users_identity_000030();

-- ---------------------------------------------------------------------------
-- 2) Roles, user_roles, teams, team_members
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS roles (
    role_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    role_code        VARCHAR(120) NOT NULL,
    display_name     TEXT         NOT NULL,
    description      TEXT,
    is_system_role   BOOLEAN      NOT NULL DEFAULT FALSE,
    permissions      TEXT[]       NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_roles_tenant_code_000030 UNIQUE (tenant_id, role_code),
    CONSTRAINT uq_roles_tenant_role_id_000030 UNIQUE (tenant_id, role_id)
);

COMMENT ON TABLE roles IS
'Tenant-scoped role catalogue with explicit permission sets (text[] of permission codes).';

CREATE INDEX IF NOT EXISTS idx_roles_tenant_system_000030
    ON roles (tenant_id, is_system_role, role_code);

CREATE INDEX IF NOT EXISTS idx_roles_tenant_created_000030
    ON roles (tenant_id, created_at DESC);

DROP TRIGGER IF EXISTS roles_updated_at_000030 ON roles;
CREATE TRIGGER roles_updated_at_000030
    BEFORE UPDATE ON roles
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS user_roles (
    user_id      UUID        NOT NULL,
    role_id      UUID        NOT NULL,
    tenant_id    UUID        NOT NULL,
    assigned_by  UUID        NOT NULL,
    assigned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id),
    CONSTRAINT fk_user_roles_user_000030
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES users(tenant_id, user_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_user_roles_role_000030
        FOREIGN KEY (tenant_id, role_id)
        REFERENCES roles(tenant_id, role_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_user_roles_assigned_by_000030
        FOREIGN KEY (tenant_id, assigned_by)
        REFERENCES users(tenant_id, user_id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_user_roles_tenant_user_000030
    ON user_roles (tenant_id, user_id, assigned_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_roles_tenant_role_000030
    ON user_roles (tenant_id, role_id, assigned_at DESC);

CREATE TABLE IF NOT EXISTS teams (
    team_id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    team_code        VARCHAR(120) NOT NULL,
    display_name     TEXT         NOT NULL,
    team_type        VARCHAR(30)  NOT NULL
                     CHECK (team_type IN ('PROCESSING', 'UNDERWRITING', 'APPROVAL', 'OPERATIONS', 'MIXED')),
    parent_team_id   UUID,
    manager_user_id  UUID,
    status           VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE'
                     CHECK (status IN ('ACTIVE', 'DISBANDED')),
    metadata         JSONB        NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_teams_tenant_code_000030 UNIQUE (tenant_id, team_code),
    CONSTRAINT uq_teams_tenant_team_id_000030 UNIQUE (tenant_id, team_id),
    CONSTRAINT fk_teams_parent_000030
        FOREIGN KEY (parent_team_id)
        REFERENCES teams(team_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_teams_manager_000030
        FOREIGN KEY (tenant_id, manager_user_id)
        REFERENCES users(tenant_id, user_id)
        ON DELETE SET NULL
);

COMMENT ON TABLE teams IS
'Tenant-scoped operational teams used for workload pools and reporting segmentation.';

CREATE INDEX IF NOT EXISTS idx_teams_tenant_status_000030
    ON teams (tenant_id, status, team_type, team_code);

CREATE INDEX IF NOT EXISTS idx_teams_tenant_active_000030
    ON teams (tenant_id, team_code)
    WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_teams_tenant_parent_000030
    ON teams (tenant_id, parent_team_id)
    WHERE parent_team_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_teams_tenant_manager_000030
    ON teams (tenant_id, manager_user_id)
    WHERE manager_user_id IS NOT NULL;

DROP TRIGGER IF EXISTS teams_updated_at_000030 ON teams;
CREATE TRIGGER teams_updated_at_000030
    BEFORE UPDATE ON teams
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS team_members (
    team_id        UUID        NOT NULL,
    user_id        UUID        NOT NULL,
    tenant_id      UUID        NOT NULL,
    role_in_team   VARCHAR(20) NOT NULL
                   CHECK (role_in_team IN ('MEMBER', 'LEAD', 'MANAGER')),
    joined_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by       UUID        NOT NULL,
    PRIMARY KEY (team_id, user_id),
    CONSTRAINT fk_team_members_team_000030
        FOREIGN KEY (tenant_id, team_id)
        REFERENCES teams(tenant_id, team_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_team_members_user_000030
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES users(tenant_id, user_id)
        ON DELETE CASCADE,
    CONSTRAINT fk_team_members_added_by_000030
        FOREIGN KEY (tenant_id, added_by)
        REFERENCES users(tenant_id, user_id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_team_members_tenant_team_000030
    ON team_members (tenant_id, team_id, role_in_team, joined_at DESC);

CREATE INDEX IF NOT EXISTS idx_team_members_tenant_user_000030
    ON team_members (tenant_id, user_id, joined_at DESC);

-- ---------------------------------------------------------------------------
-- 3) Task assignment extensions (user/team)
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS assigned_user_id UUID,
    ADD COLUMN IF NOT EXISTS assigned_team_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_assigned_user_000030'
    ) THEN
        ALTER TABLE tasks
            ADD CONSTRAINT fk_tasks_assigned_user_000030
            FOREIGN KEY (tenant_id, assigned_user_id)
            REFERENCES users(tenant_id, user_id)
            ON DELETE SET NULL;
    END IF;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_assigned_team_000030'
    ) THEN
        ALTER TABLE tasks
            ADD CONSTRAINT fk_tasks_assigned_team_000030
            FOREIGN KEY (tenant_id, assigned_team_id)
            REFERENCES teams(tenant_id, team_id)
            ON DELETE SET NULL;
    END IF;
END;
$$;

-- Query-optimized assignment indexes with tenant-leading predicates.
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_assigned_user_open_000030
    ON tasks (tenant_id, assigned_user_id, status, priority DESC, created_at ASC)
    WHERE assigned_user_id IS NOT NULL
      AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL');

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_assigned_team_open_000030
    ON tasks (tenant_id, assigned_team_id, status, priority DESC, created_at ASC)
    WHERE assigned_team_id IS NOT NULL
      AND status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS', 'AWAITING_EXTERNAL');

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_unassigned_queue_000030
    ON tasks (tenant_id, assigned_team_id, status, priority DESC, created_at ASC)
    WHERE assigned_user_id IS NULL
      AND status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_claim_version_000030
    ON tasks (tenant_id, id, version, assigned_team_id, assigned_user_id);

COMMENT ON COLUMN tasks.assigned_user_id IS
'Nullable direct assignee. When NULL and assigned_team_id is set, task is in team pool.';
COMMENT ON COLUMN tasks.assigned_team_id IS
'Nullable team pool assignment. Preserved when unassigning user back to team queue.';
