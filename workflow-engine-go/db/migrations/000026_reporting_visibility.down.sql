-- 000026_reporting_visibility.down.sql
-- Rollback reporting & operational visibility objects without data loss.
-- Strategy: archive snapshot rows, then drop runtime reporting objects/indexes.

-- ---------------------------------------------------------------------------
-- 1) Archive snapshot tables before dropping
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF to_regclass('case_throughput_snapshots') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS case_throughput_snapshots_archive_000026 (LIKE case_throughput_snapshots INCLUDING ALL)';
        EXECUTE '
            INSERT INTO case_throughput_snapshots_archive_000026
            SELECT * FROM case_throughput_snapshots
            ON CONFLICT (bucket, bucket_start, case_type_code) DO NOTHING
        ';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('task_metrics_snapshots') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS task_metrics_snapshots_archive_000026 (LIKE task_metrics_snapshots INCLUDING ALL)';
        EXECUTE '
            INSERT INTO task_metrics_snapshots_archive_000026
            SELECT * FROM task_metrics_snapshots
            ON CONFLICT (bucket, bucket_start, task_definition_code, assigned_service) DO NOTHING
        ';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('regression_metrics_snapshots') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS regression_metrics_snapshots_archive_000026 (LIKE regression_metrics_snapshots INCLUDING ALL)';
        EXECUTE '
            INSERT INTO regression_metrics_snapshots_archive_000026
            SELECT * FROM regression_metrics_snapshots
            ON CONFLICT (snapshot_date, case_type_code) DO NOTHING
        ';
    END IF;
END $$;

DO $$
BEGIN
    IF to_regclass('service_health_snapshots') IS NOT NULL THEN
        EXECUTE 'CREATE TABLE IF NOT EXISTS service_health_snapshots_archive_000026 (LIKE service_health_snapshots INCLUDING ALL)';
        EXECUTE '
            INSERT INTO service_health_snapshots_archive_000026
            SELECT * FROM service_health_snapshots
            ON CONFLICT (bucket, bucket_start, assigned_service) DO NOTHING
        ';
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 2) Drop triggers for reporting tables
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS service_health_snapshots_updated_at ON service_health_snapshots;
DROP TRIGGER IF EXISTS regression_metrics_snapshots_updated_at ON regression_metrics_snapshots;
DROP TRIGGER IF EXISTS task_metrics_snapshots_updated_at ON task_metrics_snapshots;
DROP TRIGGER IF EXISTS case_throughput_snapshots_updated_at ON case_throughput_snapshots;

-- ---------------------------------------------------------------------------
-- 3) Drop reporting-only indexes
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_events_outbox_reporting_type_hour;
DROP INDEX IF EXISTS idx_events_outbox_reporting_timeline;
DROP INDEX IF EXISTS idx_task_dlq_reporting_active_depth;
DROP INDEX IF EXISTS idx_tasks_reporting_retry_depth;
DROP INDEX IF EXISTS idx_tasks_reporting_pending_depth;
DROP INDEX IF EXISTS idx_tasks_reporting_execution;
DROP INDEX IF EXISTS idx_case_stage_transitions_reporting_path;
DROP INDEX IF EXISTS idx_case_stage_transitions_reporting_time;
DROP INDEX IF EXISTS idx_cases_reporting_stage_status;
DROP INDEX IF EXISTS idx_cases_reporting_completed_type;
DROP INDEX IF EXISTS idx_cases_reporting_created_type;

DROP INDEX IF EXISTS idx_service_health_snapshots_rank;
DROP INDEX IF EXISTS idx_regression_metrics_snapshots_lookup;
DROP INDEX IF EXISTS idx_task_metrics_snapshots_bucket_time;
DROP INDEX IF EXISTS idx_task_metrics_snapshots_lookup;
DROP INDEX IF EXISTS idx_case_throughput_snapshots_bucket_time;
DROP INDEX IF EXISTS idx_case_throughput_snapshots_lookup;

-- ---------------------------------------------------------------------------
-- 4) Drop runtime snapshot tables
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS service_health_snapshots;
DROP TABLE IF EXISTS regression_metrics_snapshots;
DROP TABLE IF EXISTS task_metrics_snapshots;
DROP TABLE IF EXISTS case_throughput_snapshots;
