-- 000028_multi_tenancy_partitioning.up.sql
--
-- Multi-tenancy & partitioning (Option B selected):
-- We keep single logical tables and place tenant_id as the leading key in
-- operational indexes. At 100k cases / 1M events per day this avoids the
-- operational overhead of per-tenant declarative partitions while preserving
-- predictable index pruning on every tenant-scoped query.

-- ---------------------------------------------------------------------------
-- 0) Tenants registry
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenants (
    tenant_id        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_code      VARCHAR(20)  NOT NULL,
    name             VARCHAR(255) NOT NULL,
    status           VARCHAR(20)  NOT NULL
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'OFFBOARDED')),
    tier             VARCHAR(20)  NOT NULL
                    CHECK (tier IN ('STANDARD', 'PREMIUM', 'ENTERPRISE')),
    config           JSONB        NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_tenants_code UNIQUE (tenant_code)
);

COMMENT ON TABLE tenants IS
'Logical tenant registry used for strict row-level isolation in the shared Postgres cluster.';
COMMENT ON COLUMN tenants.config IS
'Tenant overrides: capacity limits, allowed case types, feature_flags, SLA multipliers.';

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants (status);
CREATE INDEX IF NOT EXISTS idx_tenants_tier ON tenants (tier);

DROP TRIGGER IF EXISTS tenants_updated_at ON tenants;
CREATE TRIGGER tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- Default tenant for pre-multitenant rows and backward-compatible paths.
INSERT INTO tenants (tenant_id, tenant_code, name, status, tier, config)
VALUES (
    '00000000-0000-0000-0000-000000000000'::uuid,
    'DEFAULT',
    'Default Tenant',
    'ACTIVE',
    'STANDARD',
    '{"max_active_cases":1000000,"max_concurrent_tasks":1000000,"max_cases_per_minute":1000000,"feature_flags":{}}'::jsonb
)
ON CONFLICT (tenant_code) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 1) Tenant-scoped case type catalog
-- NULL tenant_id = GLOBAL case type; non-null = tenant-specific
-- ---------------------------------------------------------------------------
ALTER TABLE case_types
    ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(tenant_id) ON DELETE RESTRICT;

COMMENT ON COLUMN case_types.tenant_id IS
'NULL means GLOBAL case type visible to all tenants. Non-NULL means tenant-owned case type.';

DROP INDEX IF EXISTS idx_case_types_active_code;
DROP INDEX IF EXISTS idx_case_types_one_active_per_code_000027;

CREATE UNIQUE INDEX IF NOT EXISTS idx_case_types_active_code_tenant
    ON case_types (code, COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_case_types_visibility_code_status
    ON case_types (code, status, tenant_id, version DESC);

CREATE INDEX IF NOT EXISTS idx_case_types_tenant_status
    ON case_types (tenant_id, status, code, version DESC)
    WHERE tenant_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2) Core table tenant_id columns with backfill + NOT NULL + FK
-- ---------------------------------------------------------------------------
ALTER TABLE cases ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE cases
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE cases ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE cases ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_cases_tenant_id_000028'
    ) THEN
        ALTER TABLE cases
            ADD CONSTRAINT fk_cases_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE tasks t
   SET tenant_id = c.tenant_id
  FROM cases c
 WHERE t.case_id = c.id
   AND t.tenant_id IS NULL;
UPDATE tasks
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE tasks ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE tasks ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_tasks_tenant_id_000028'
    ) THEN
        ALTER TABLE tasks
            ADD CONSTRAINT fk_tasks_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;

ALTER TABLE case_stage_transitions ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE case_stage_transitions cst
   SET tenant_id = c.tenant_id
  FROM cases c
 WHERE cst.case_id = c.id
   AND cst.tenant_id IS NULL;
UPDATE case_stage_transitions
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE case_stage_transitions ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE case_stage_transitions ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_case_stage_transitions_tenant_id_000028'
    ) THEN
        ALTER TABLE case_stage_transitions
            ADD CONSTRAINT fk_case_stage_transitions_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;

ALTER TABLE events_outbox ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE events_outbox eo
   SET tenant_id = c.tenant_id
  FROM cases c
 WHERE eo.case_id = c.id
   AND eo.tenant_id IS NULL;
UPDATE events_outbox eo
   SET tenant_id = t.tenant_id
  FROM tasks t
 WHERE eo.task_id = t.id
   AND eo.tenant_id IS NULL;
UPDATE events_outbox
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE events_outbox ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE events_outbox ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_events_outbox_tenant_id_000028'
    ) THEN
        ALTER TABLE events_outbox
            ADD CONSTRAINT fk_events_outbox_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;

ALTER TABLE notification_queue ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE notification_queue nq
   SET tenant_id = c.tenant_id
  FROM cases c
 WHERE nq.case_id = c.id
   AND nq.tenant_id IS NULL;
UPDATE notification_queue nq
   SET tenant_id = t.tenant_id
  FROM tasks t
 WHERE nq.task_id = t.id
   AND nq.tenant_id IS NULL;
UPDATE notification_queue
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE notification_queue ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE notification_queue ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_notification_queue_tenant_id_000028'
    ) THEN
        ALTER TABLE notification_queue
            ADD CONSTRAINT fk_notification_queue_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;

ALTER TABLE task_dlq ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE task_dlq dlq
   SET tenant_id = c.tenant_id
  FROM cases c
 WHERE dlq.case_id = c.id
   AND dlq.tenant_id IS NULL;
UPDATE task_dlq
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE task_dlq ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE task_dlq ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_task_dlq_tenant_id_000028'
    ) THEN
        ALTER TABLE task_dlq
            ADD CONSTRAINT fk_task_dlq_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;

-- Reporting snapshot tables
ALTER TABLE case_throughput_snapshots ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE case_throughput_snapshots
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE case_throughput_snapshots ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE case_throughput_snapshots ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_case_throughput_snapshots_tenant_id_000028'
    ) THEN
        ALTER TABLE case_throughput_snapshots
            ADD CONSTRAINT fk_case_throughput_snapshots_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;
ALTER TABLE case_throughput_snapshots DROP CONSTRAINT IF EXISTS case_throughput_snapshots_pkey;
ALTER TABLE case_throughput_snapshots
    ADD CONSTRAINT case_throughput_snapshots_pkey PRIMARY KEY (tenant_id, bucket, bucket_start, case_type_code);

ALTER TABLE task_metrics_snapshots ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE task_metrics_snapshots
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE task_metrics_snapshots ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE task_metrics_snapshots ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_task_metrics_snapshots_tenant_id_000028'
    ) THEN
        ALTER TABLE task_metrics_snapshots
            ADD CONSTRAINT fk_task_metrics_snapshots_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;
ALTER TABLE task_metrics_snapshots DROP CONSTRAINT IF EXISTS task_metrics_snapshots_pkey;
ALTER TABLE task_metrics_snapshots
    ADD CONSTRAINT task_metrics_snapshots_pkey PRIMARY KEY (tenant_id, bucket, bucket_start, task_definition_code, assigned_service);

ALTER TABLE regression_metrics_snapshots ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE regression_metrics_snapshots
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE regression_metrics_snapshots ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE regression_metrics_snapshots ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_regression_metrics_snapshots_tenant_id_000028'
    ) THEN
        ALTER TABLE regression_metrics_snapshots
            ADD CONSTRAINT fk_regression_metrics_snapshots_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;
ALTER TABLE regression_metrics_snapshots DROP CONSTRAINT IF EXISTS regression_metrics_snapshots_pkey;
ALTER TABLE regression_metrics_snapshots
    ADD CONSTRAINT regression_metrics_snapshots_pkey PRIMARY KEY (tenant_id, snapshot_date, case_type_code);

ALTER TABLE service_health_snapshots ADD COLUMN IF NOT EXISTS tenant_id UUID;
UPDATE service_health_snapshots
   SET tenant_id = '00000000-0000-0000-0000-000000000000'::uuid
 WHERE tenant_id IS NULL;
ALTER TABLE service_health_snapshots ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000'::uuid;
ALTER TABLE service_health_snapshots ALTER COLUMN tenant_id SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_service_health_snapshots_tenant_id_000028'
    ) THEN
        ALTER TABLE service_health_snapshots
            ADD CONSTRAINT fk_service_health_snapshots_tenant_id_000028
            FOREIGN KEY (tenant_id)
            REFERENCES tenants(tenant_id)
            ON DELETE RESTRICT;
    END IF;
END;
$$;
ALTER TABLE service_health_snapshots DROP CONSTRAINT IF EXISTS service_health_snapshots_pkey;
ALTER TABLE service_health_snapshots
    ADD CONSTRAINT service_health_snapshots_pkey PRIMARY KEY (tenant_id, bucket, bucket_start, assigned_service);

-- ---------------------------------------------------------------------------
-- 3) Tenant consistency triggers for non-breaking write paths
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION trg_sync_tasks_tenant_000028()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    case_tenant_id UUID;
BEGIN
    SELECT tenant_id INTO case_tenant_id FROM cases WHERE id = NEW.case_id;

    IF NEW.tenant_id IS NULL THEN
        NEW.tenant_id := COALESCE(case_tenant_id, '00000000-0000-0000-0000-000000000000'::uuid);
    END IF;

    IF case_tenant_id IS NOT NULL AND NEW.tenant_id <> case_tenant_id THEN
        RAISE EXCEPTION 'tasks tenant mismatch: task tenant % does not match case tenant %', NEW.tenant_id, case_tenant_id;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS tasks_sync_tenant_000028 ON tasks;
CREATE TRIGGER tasks_sync_tenant_000028
    BEFORE INSERT OR UPDATE OF case_id, tenant_id ON tasks
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_tasks_tenant_000028();

CREATE OR REPLACE FUNCTION trg_sync_case_stage_transitions_tenant_000028()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    case_tenant_id UUID;
BEGIN
    SELECT tenant_id INTO case_tenant_id FROM cases WHERE id = NEW.case_id;
    NEW.tenant_id := COALESCE(NEW.tenant_id, case_tenant_id, '00000000-0000-0000-0000-000000000000'::uuid);

    IF case_tenant_id IS NOT NULL AND NEW.tenant_id <> case_tenant_id THEN
        RAISE EXCEPTION 'case_stage_transitions tenant mismatch: transition tenant % does not match case tenant %', NEW.tenant_id, case_tenant_id;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS case_stage_transitions_sync_tenant_000028 ON case_stage_transitions;
CREATE TRIGGER case_stage_transitions_sync_tenant_000028
    BEFORE INSERT OR UPDATE OF case_id, tenant_id ON case_stage_transitions
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_case_stage_transitions_tenant_000028();

CREATE OR REPLACE FUNCTION trg_sync_events_outbox_tenant_000028()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    case_tenant_id UUID;
    task_tenant_id UUID;
BEGIN
    IF NEW.case_id IS NOT NULL THEN
        SELECT tenant_id INTO case_tenant_id FROM cases WHERE id = NEW.case_id;
    END IF;
    IF NEW.task_id IS NOT NULL THEN
        SELECT tenant_id INTO task_tenant_id FROM tasks WHERE id = NEW.task_id;
    END IF;

    NEW.tenant_id := COALESCE(NEW.tenant_id, case_tenant_id, task_tenant_id, '00000000-0000-0000-0000-000000000000'::uuid);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS events_outbox_sync_tenant_000028 ON events_outbox;
CREATE TRIGGER events_outbox_sync_tenant_000028
    BEFORE INSERT OR UPDATE OF case_id, task_id, tenant_id ON events_outbox
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_events_outbox_tenant_000028();

CREATE OR REPLACE FUNCTION trg_sync_notification_queue_tenant_000028()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    case_tenant_id UUID;
    task_tenant_id UUID;
BEGIN
    IF NEW.case_id IS NOT NULL THEN
        SELECT tenant_id INTO case_tenant_id FROM cases WHERE id = NEW.case_id;
    END IF;
    IF NEW.task_id IS NOT NULL THEN
        SELECT tenant_id INTO task_tenant_id FROM tasks WHERE id = NEW.task_id;
    END IF;

    NEW.tenant_id := COALESCE(NEW.tenant_id, case_tenant_id, task_tenant_id, '00000000-0000-0000-0000-000000000000'::uuid);
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS notification_queue_sync_tenant_000028 ON notification_queue;
CREATE TRIGGER notification_queue_sync_tenant_000028
    BEFORE INSERT OR UPDATE OF case_id, task_id, tenant_id ON notification_queue
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_notification_queue_tenant_000028();

CREATE OR REPLACE FUNCTION trg_sync_task_dlq_tenant_000028()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    case_tenant_id UUID;
BEGIN
    SELECT tenant_id INTO case_tenant_id FROM cases WHERE id = NEW.case_id;
    NEW.tenant_id := COALESCE(NEW.tenant_id, case_tenant_id, '00000000-0000-0000-0000-000000000000'::uuid);

    IF case_tenant_id IS NOT NULL AND NEW.tenant_id <> case_tenant_id THEN
        RAISE EXCEPTION 'task_dlq tenant mismatch: dlq tenant % does not match case tenant %', NEW.tenant_id, case_tenant_id;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS task_dlq_sync_tenant_000028 ON task_dlq;
CREATE TRIGGER task_dlq_sync_tenant_000028
    BEFORE INSERT OR UPDATE OF case_id, tenant_id ON task_dlq
    FOR EACH ROW
    EXECUTE FUNCTION trg_sync_task_dlq_tenant_000028();

-- ---------------------------------------------------------------------------
-- 4) Option B index strategy (tenant_id leading)
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_cases_tenant_status_created
    ON cases (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cases_tenant_case_type
    ON cases (tenant_id, case_type_id, case_type_version);

CREATE INDEX IF NOT EXISTS idx_cases_tenant_parent
    ON cases (tenant_id, parent_case_id)
    WHERE parent_case_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cases_tenant_stage_ordinal
    ON cases (tenant_id, current_stage_ordinal);

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_case_status
    ON tasks (tenant_id, case_id, status);

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_worker_poll
    ON tasks (tenant_id, assigned_service, status, priority DESC, due_at ASC NULLS LAST);

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_activity_status
    ON tasks (tenant_id, stage_code, activity_code, status);

CREATE INDEX IF NOT EXISTS idx_tasks_tenant_case_stage
    ON tasks (tenant_id, case_id, stage_code);

CREATE INDEX IF NOT EXISTS idx_stage_transitions_tenant_case
    ON case_stage_transitions (tenant_id, case_id, transitioned_at DESC);

CREATE INDEX IF NOT EXISTS idx_outbox_tenant_poll
    ON events_outbox (tenant_id, target_service, status, created_at ASC)
    WHERE status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_outbox_tenant_case_type
    ON events_outbox (tenant_id, case_id, event_type);

CREATE INDEX IF NOT EXISTS idx_notification_queue_tenant_poll
    ON notification_queue (tenant_id, status, scheduled_at, priority DESC);

CREATE INDEX IF NOT EXISTS idx_task_dlq_tenant_case_oldest
    ON task_dlq (tenant_id, case_id, moved_at ASC);

CREATE INDEX IF NOT EXISTS idx_case_throughput_snapshots_tenant_lookup
    ON case_throughput_snapshots (tenant_id, case_type_code, bucket, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_task_metrics_snapshots_tenant_lookup
    ON task_metrics_snapshots (tenant_id, task_definition_code, assigned_service, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_regression_metrics_snapshots_tenant_lookup
    ON regression_metrics_snapshots (tenant_id, case_type_code, snapshot_date DESC);

CREATE INDEX IF NOT EXISTS idx_service_health_snapshots_tenant_rank
    ON service_health_snapshots (tenant_id, bucket_start DESC, failure_rate_percent DESC, dlq_contribution_rate_percent DESC);

-- ---------------------------------------------------------------------------
-- 5) Tenant persistent rate-limit counters
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_rate_limit_counters (
    tenant_id      UUID        NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    window_start   TIMESTAMPTZ NOT NULL,
    case_count     INT         NOT NULL DEFAULT 0 CHECK (case_count >= 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, window_start)
);

COMMENT ON TABLE tenant_rate_limit_counters IS
'Durable minute-level counters for tenant case-creation rate limits; survives restarts and supports multi-instance coordination.';

CREATE INDEX IF NOT EXISTS idx_tenant_rate_limit_counters_window
    ON tenant_rate_limit_counters (window_start);

DROP TRIGGER IF EXISTS tenant_rate_limit_counters_updated_at ON tenant_rate_limit_counters;
CREATE TRIGGER tenant_rate_limit_counters_updated_at
    BEFORE UPDATE ON tenant_rate_limit_counters
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();
