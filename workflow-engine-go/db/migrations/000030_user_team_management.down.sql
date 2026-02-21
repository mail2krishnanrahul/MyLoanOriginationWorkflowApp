-- 000030_user_team_management.down.sql
--
-- Roll back user/team management capability.
-- Order is dependency-safe: task FKs/columns -> membership tables -> users schema reshape.

-- ---------------------------------------------------------------------------
-- 1) Drop task assignment extensions
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_tasks_tenant_claim_version_000030;
DROP INDEX IF EXISTS idx_tasks_tenant_unassigned_queue_000030;
DROP INDEX IF EXISTS idx_tasks_tenant_assigned_team_open_000030;
DROP INDEX IF EXISTS idx_tasks_tenant_assigned_user_open_000030;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS fk_tasks_assigned_team_000030;
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS fk_tasks_assigned_user_000030;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS assigned_team_id,
    DROP COLUMN IF EXISTS assigned_user_id;

-- ---------------------------------------------------------------------------
-- 2) Drop team/role assignment tables
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS team_members;

DROP TRIGGER IF EXISTS teams_updated_at_000030 ON teams;
DROP TABLE IF EXISTS teams;

DROP TABLE IF EXISTS user_roles;

DROP TRIGGER IF EXISTS roles_updated_at_000030 ON roles;
DROP TABLE IF EXISTS roles;

-- ---------------------------------------------------------------------------
-- 3) Revert users table to legacy shape used before 000030
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS users_identity_sync_000030 ON users;
DROP FUNCTION IF EXISTS trg_sync_users_identity_000030();

DROP INDEX IF EXISTS idx_users_tenant_search_000030;
DROP INDEX IF EXISTS idx_users_tenant_manager_legacy_000030;
DROP INDEX IF EXISTS idx_users_tenant_role_status_legacy_000030;
DROP INDEX IF EXISTS idx_users_tenant_active_000030;
DROP INDEX IF EXISTS idx_users_tenant_status_000030;
DROP INDEX IF EXISTS uq_users_tenant_user_id_000030;
DROP INDEX IF EXISTS uq_users_tenant_external_id_000030;
DROP INDEX IF EXISTS uq_users_tenant_lower_email_000030;
DROP INDEX IF EXISTS uq_users_tenant_lower_username_000030;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_manager_id_fkey;
ALTER TABLE users DROP CONSTRAINT IF EXISTS fk_users_tenant_id_000030;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_auth_provider_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_status_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS uq_users_legacy_id_000030;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_pkey;

UPDATE users
SET status = CASE
    WHEN status IN ('SUSPENDED', 'DEACTIVATED') THEN 'INACTIVE'
    ELSE status
END;

ALTER TABLE users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE users
    ADD CONSTRAINT users_manager_id_fkey
        FOREIGN KEY (manager_id)
        REFERENCES users(id)
        ON DELETE SET NULL;

ALTER TABLE users
    ADD CONSTRAINT users_status_check
        CHECK (status IN ('ACTIVE', 'INACTIVE'));

ALTER TABLE users
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS last_login_at,
    DROP COLUMN IF EXISTS locale,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS external_id,
    DROP COLUMN IF EXISTS auth_provider,
    DROP COLUMN IF EXISTS display_name,
    DROP COLUMN IF EXISTS email,
    DROP COLUMN IF EXISTS username,
    DROP COLUMN IF EXISTS tenant_id,
    DROP COLUMN IF EXISTS user_id;

-- Recreate legacy indexes from 000022 for approval routing.
CREATE INDEX IF NOT EXISTS idx_users_role_status
    ON users (role_code, status)
    WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_users_manager
    ON users (manager_id)
    WHERE manager_id IS NOT NULL;
