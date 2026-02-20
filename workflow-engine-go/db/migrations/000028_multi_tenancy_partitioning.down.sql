-- 000028_multi_tenancy_partitioning.down.sql
-- Rollback preserves tenant registry data by archiving tenant-owned tables.

-- ---------------------------------------------------------------------------
-- 0) Archive tenant tables (no data loss) before dropping capability tables.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'tenants') THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS tenants_archive_000028 AS SELECT * FROM tenants';
    END IF;
END;
$$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'tenant_rate_limit_counters') THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS tenant_rate_limit_counters_archive_000028 AS SELECT * FROM tenant_rate_limit_counters';
    END IF;
END;
$$;

-- ---------------------------------------------------------------------------
-- 1) Drop tenant sync triggers/functions
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS tasks_sync_tenant_000028 ON tasks;
DROP FUNCTION IF EXISTS trg_sync_tasks_tenant_000028();

DROP TRIGGER IF EXISTS case_stage_transitions_sync_tenant_000028 ON case_stage_transitions;
DROP FUNCTION IF EXISTS trg_sync_case_stage_transitions_tenant_000028();

DROP TRIGGER IF EXISTS events_outbox_sync_tenant_000028 ON events_outbox;
DROP FUNCTION IF EXISTS trg_sync_events_outbox_tenant_000028();

DROP TRIGGER IF EXISTS notification_queue_sync_tenant_000028 ON notification_queue;
DROP FUNCTION IF EXISTS trg_sync_notification_queue_tenant_000028();

DROP TRIGGER IF EXISTS task_dlq_sync_tenant_000028 ON task_dlq;
DROP FUNCTION IF EXISTS trg_sync_task_dlq_tenant_000028();

DROP TRIGGER IF EXISTS tenant_rate_limit_counters_updated_at ON tenant_rate_limit_counters;

-- ---------------------------------------------------------------------------
-- 2) Drop tenant-aware indexes
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_cases_tenant_status_created;
DROP INDEX IF EXISTS idx_cases_tenant_case_type;
DROP INDEX IF EXISTS idx_cases_tenant_parent;
DROP INDEX IF EXISTS idx_cases_tenant_stage_ordinal;
DROP INDEX IF EXISTS idx_tasks_tenant_case_status;
DROP INDEX IF EXISTS idx_tasks_tenant_worker_poll;
DROP INDEX IF EXISTS idx_tasks_tenant_activity_status;
DROP INDEX IF EXISTS idx_tasks_tenant_case_stage;
DROP INDEX IF EXISTS idx_stage_transitions_tenant_case;
DROP INDEX IF EXISTS idx_outbox_tenant_poll;
DROP INDEX IF EXISTS idx_outbox_tenant_case_type;
DROP INDEX IF EXISTS idx_notification_queue_tenant_poll;
DROP INDEX IF EXISTS idx_task_dlq_tenant_case_oldest;
DROP INDEX IF EXISTS idx_case_throughput_snapshots_tenant_lookup;
DROP INDEX IF EXISTS idx_task_metrics_snapshots_tenant_lookup;
DROP INDEX IF EXISTS idx_regression_metrics_snapshots_tenant_lookup;
DROP INDEX IF EXISTS idx_service_health_snapshots_tenant_rank;
DROP INDEX IF EXISTS idx_case_types_visibility_code_status;
DROP INDEX IF EXISTS idx_case_types_tenant_status;
DROP INDEX IF EXISTS idx_case_types_active_code_tenant;

-- ---------------------------------------------------------------------------
-- 3) Restore snapshot primary keys, then remove tenant_id columns
-- ---------------------------------------------------------------------------
ALTER TABLE case_throughput_snapshots DROP CONSTRAINT IF EXISTS case_throughput_snapshots_pkey;
ALTER TABLE case_throughput_snapshots
    ADD CONSTRAINT case_throughput_snapshots_pkey PRIMARY KEY (bucket, bucket_start, case_type_code);
ALTER TABLE case_throughput_snapshots DROP CONSTRAINT IF EXISTS fk_case_throughput_snapshots_tenant_id_000028;
ALTER TABLE case_throughput_snapshots DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE task_metrics_snapshots DROP CONSTRAINT IF EXISTS task_metrics_snapshots_pkey;
ALTER TABLE task_metrics_snapshots
    ADD CONSTRAINT task_metrics_snapshots_pkey PRIMARY KEY (bucket, bucket_start, task_definition_code, assigned_service);
ALTER TABLE task_metrics_snapshots DROP CONSTRAINT IF EXISTS fk_task_metrics_snapshots_tenant_id_000028;
ALTER TABLE task_metrics_snapshots DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE regression_metrics_snapshots DROP CONSTRAINT IF EXISTS regression_metrics_snapshots_pkey;
ALTER TABLE regression_metrics_snapshots
    ADD CONSTRAINT regression_metrics_snapshots_pkey PRIMARY KEY (snapshot_date, case_type_code);
ALTER TABLE regression_metrics_snapshots DROP CONSTRAINT IF EXISTS fk_regression_metrics_snapshots_tenant_id_000028;
ALTER TABLE regression_metrics_snapshots DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE service_health_snapshots DROP CONSTRAINT IF EXISTS service_health_snapshots_pkey;
ALTER TABLE service_health_snapshots
    ADD CONSTRAINT service_health_snapshots_pkey PRIMARY KEY (bucket, bucket_start, assigned_service);
ALTER TABLE service_health_snapshots DROP CONSTRAINT IF EXISTS fk_service_health_snapshots_tenant_id_000028;
ALTER TABLE service_health_snapshots DROP COLUMN IF EXISTS tenant_id;

-- ---------------------------------------------------------------------------
-- 4) Drop tenant_id columns from core tables
-- ---------------------------------------------------------------------------
ALTER TABLE task_dlq DROP CONSTRAINT IF EXISTS fk_task_dlq_tenant_id_000028;
ALTER TABLE task_dlq DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE notification_queue DROP CONSTRAINT IF EXISTS fk_notification_queue_tenant_id_000028;
ALTER TABLE notification_queue DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE events_outbox DROP CONSTRAINT IF EXISTS fk_events_outbox_tenant_id_000028;
ALTER TABLE events_outbox DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE case_stage_transitions DROP CONSTRAINT IF EXISTS fk_case_stage_transitions_tenant_id_000028;
ALTER TABLE case_stage_transitions DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS fk_tasks_tenant_id_000028;
ALTER TABLE tasks DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE cases DROP CONSTRAINT IF EXISTS fk_cases_tenant_id_000028;
ALTER TABLE cases DROP COLUMN IF EXISTS tenant_id;

ALTER TABLE case_types DROP COLUMN IF EXISTS tenant_id;

-- Restore pre-000028 active-version indexes.
CREATE UNIQUE INDEX IF NOT EXISTS idx_case_types_active_code
    ON case_types (code)
    WHERE status = 'ACTIVE';

CREATE UNIQUE INDEX IF NOT EXISTS idx_case_types_one_active_per_code_000027
    ON case_types (code)
    WHERE status = 'ACTIVE';

-- ---------------------------------------------------------------------------
-- 5) Drop tenant capability tables (data preserved in *_archive_000028)
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS tenant_rate_limit_counters;
DROP TABLE IF EXISTS tenants;
