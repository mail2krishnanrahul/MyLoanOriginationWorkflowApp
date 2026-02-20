-- 000026_reporting_visibility.up.sql
-- Reporting & operational visibility capability:
-- pre-aggregated throughput/task/regression/service-health snapshots
-- plus query-oriented indexes for low-latency dashboard reads.

-- ---------------------------------------------------------------------------
-- 1) Case throughput snapshots (hourly/daily)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_throughput_snapshots (
    bucket                  VARCHAR(10)     NOT NULL
                                CHECK (bucket IN ('HOURLY', 'DAILY')),
    bucket_start            TIMESTAMPTZ     NOT NULL,
    case_type_code          VARCHAR(100)    NOT NULL,
    created_count           BIGINT          NOT NULL DEFAULT 0,
    completed_count         BIGINT          NOT NULL DEFAULT 0,
    cancelled_count         BIGINT          NOT NULL DEFAULT 0,
    inflight_count          BIGINT          NOT NULL DEFAULT 0,
    snapshot_refreshed_at   TIMESTAMPTZ     NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (bucket, bucket_start, case_type_code)
);

COMMENT ON TABLE case_throughput_snapshots IS
'Pre-aggregated case throughput by bucket/type. Read path must query this table, not full cases scans.';
COMMENT ON COLUMN case_throughput_snapshots.inflight_count IS
'Estimated open in-flight volume at end of bucket (created cumulative minus closed cumulative).';
COMMENT ON COLUMN case_throughput_snapshots.snapshot_refreshed_at IS
'UTC timestamp when this snapshot bucket was last recomputed by MetricsRefreshJob.';

CREATE INDEX IF NOT EXISTS idx_case_throughput_snapshots_lookup
    ON case_throughput_snapshots (case_type_code, bucket, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_case_throughput_snapshots_bucket_time
    ON case_throughput_snapshots (bucket, bucket_start DESC);

DROP TRIGGER IF EXISTS case_throughput_snapshots_updated_at ON case_throughput_snapshots;
CREATE TRIGGER case_throughput_snapshots_updated_at
    BEFORE UPDATE ON case_throughput_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 2) Task execution metric snapshots (hourly)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_metrics_snapshots (
    bucket                      VARCHAR(10)     NOT NULL
                                    CHECK (bucket IN ('HOURLY', 'DAILY')),
    bucket_start                TIMESTAMPTZ     NOT NULL,
    task_definition_code        VARCHAR(100)    NOT NULL,
    assigned_service            TEXT            NOT NULL,
    total_tasks                 BIGINT          NOT NULL DEFAULT 0,
    completed_tasks             BIGINT          NOT NULL DEFAULT 0,
    failed_tasks                BIGINT          NOT NULL DEFAULT 0,
    retried_tasks               BIGINT          NOT NULL DEFAULT 0,
    dlq_tasks                   BIGINT          NOT NULL DEFAULT 0,
    avg_execution_seconds       DOUBLE PRECISION NOT NULL DEFAULT 0,
    p50_execution_seconds       DOUBLE PRECISION NOT NULL DEFAULT 0,
    p95_execution_seconds       DOUBLE PRECISION NOT NULL DEFAULT 0,
    p99_execution_seconds       DOUBLE PRECISION NOT NULL DEFAULT 0,
    retry_rate_percent          NUMERIC(8,4)    NOT NULL DEFAULT 0 CHECK (retry_rate_percent >= 0 AND retry_rate_percent <= 100),
    failure_rate_percent        NUMERIC(8,4)    NOT NULL DEFAULT 0 CHECK (failure_rate_percent >= 0 AND failure_rate_percent <= 100),
    dlq_rate_percent            NUMERIC(8,4)    NOT NULL DEFAULT 0 CHECK (dlq_rate_percent >= 0 AND dlq_rate_percent <= 100),
    snapshot_refreshed_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    created_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (bucket, bucket_start, task_definition_code, assigned_service)
);

COMMENT ON TABLE task_metrics_snapshots IS
'Pre-aggregated task execution KPIs by task_definition and assigned_service.';
COMMENT ON COLUMN task_metrics_snapshots.p95_execution_seconds IS
'Latency percentile computed in SQL via percentile_cont during refresh.';
COMMENT ON COLUMN task_metrics_snapshots.dlq_rate_percent IS
'Percentage of tasks in this bucket that contributed to DLQ movement.';

CREATE INDEX IF NOT EXISTS idx_task_metrics_snapshots_lookup
    ON task_metrics_snapshots (task_definition_code, assigned_service, bucket_start DESC);

CREATE INDEX IF NOT EXISTS idx_task_metrics_snapshots_bucket_time
    ON task_metrics_snapshots (bucket, bucket_start DESC);

DROP TRIGGER IF EXISTS task_metrics_snapshots_updated_at ON task_metrics_snapshots;
CREATE TRIGGER task_metrics_snapshots_updated_at
    BEFORE UPDATE ON task_metrics_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 3) Regression/rework summary snapshots (daily)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS regression_metrics_snapshots (
    snapshot_date               DATE            NOT NULL,
    case_type_code              VARCHAR(100)    NOT NULL,
    forward_transition_count    BIGINT          NOT NULL DEFAULT 0,
    regression_count            BIGINT          NOT NULL DEFAULT 0,
    regression_rate_percent     NUMERIC(8,4)    NOT NULL DEFAULT 0 CHECK (regression_rate_percent >= 0 AND regression_rate_percent <= 100),
    most_common_paths           JSONB           NOT NULL DEFAULT '[]'::jsonb,
    cases_above_threshold       BIGINT          NOT NULL DEFAULT 0,
    flagged_cases               JSONB           NOT NULL DEFAULT '[]'::jsonb,
    regression_threshold        INT             NOT NULL DEFAULT 3 CHECK (regression_threshold > 0),
    snapshot_refreshed_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    created_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (snapshot_date, case_type_code)
);

COMMENT ON TABLE regression_metrics_snapshots IS
'Daily regression/rework rollups derived from case_stage_transitions with is_regression semantics.';
COMMENT ON COLUMN regression_metrics_snapshots.most_common_paths IS
'JSON array: [{from_stage_code,to_stage_code,count}] ranked by frequency.';
COMMENT ON COLUMN regression_metrics_snapshots.flagged_cases IS
'JSON array of cases whose regression count exceeded regression_threshold for operator review.';

CREATE INDEX IF NOT EXISTS idx_regression_metrics_snapshots_lookup
    ON regression_metrics_snapshots (case_type_code, snapshot_date DESC);

DROP TRIGGER IF EXISTS regression_metrics_snapshots_updated_at ON regression_metrics_snapshots;
CREATE TRIGGER regression_metrics_snapshots_updated_at
    BEFORE UPDATE ON regression_metrics_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) Service health leaderboard snapshots (hourly)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS service_health_snapshots (
    bucket                          VARCHAR(10)      NOT NULL
                                        CHECK (bucket IN ('HOURLY', 'DAILY')),
    bucket_start                    TIMESTAMPTZ      NOT NULL,
    assigned_service                TEXT             NOT NULL,
    total_tasks                     BIGINT           NOT NULL DEFAULT 0,
    failed_tasks                    BIGINT           NOT NULL DEFAULT 0,
    retried_tasks                   BIGINT           NOT NULL DEFAULT 0,
    dlq_tasks                       BIGINT           NOT NULL DEFAULT 0,
    failure_rate_percent            NUMERIC(8,4)     NOT NULL DEFAULT 0 CHECK (failure_rate_percent >= 0 AND failure_rate_percent <= 100),
    avg_execution_seconds           DOUBLE PRECISION NOT NULL DEFAULT 0,
    retry_rate_percent              NUMERIC(8,4)     NOT NULL DEFAULT 0 CHECK (retry_rate_percent >= 0 AND retry_rate_percent <= 100),
    dlq_rate_percent                NUMERIC(8,4)     NOT NULL DEFAULT 0 CHECK (dlq_rate_percent >= 0 AND dlq_rate_percent <= 100),
    dlq_contribution_rate_percent   NUMERIC(8,4)     NOT NULL DEFAULT 0 CHECK (dlq_contribution_rate_percent >= 0 AND dlq_contribution_rate_percent <= 100),
    snapshot_refreshed_at           TIMESTAMPTZ      NOT NULL DEFAULT now(),
    created_at                      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ      NOT NULL DEFAULT now(),
    PRIMARY KEY (bucket, bucket_start, assigned_service)
);

COMMENT ON TABLE service_health_snapshots IS
'Leaderboard-ready hourly service health rollups derived from task_metrics snapshots.';
COMMENT ON COLUMN service_health_snapshots.dlq_contribution_rate_percent IS
'Contribution share of each service to total DLQ load for the same bucket.';

CREATE INDEX IF NOT EXISTS idx_service_health_snapshots_rank
    ON service_health_snapshots (bucket_start DESC, failure_rate_percent DESC, dlq_contribution_rate_percent DESC);

DROP TRIGGER IF EXISTS service_health_snapshots_updated_at ON service_health_snapshots;
CREATE TRIGGER service_health_snapshots_updated_at
    BEFORE UPDATE ON service_health_snapshots
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 5) Source-table indexes for reporting query patterns
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_cases_reporting_created_type
    ON cases (case_type_id, created_at);

CREATE INDEX IF NOT EXISTS idx_cases_reporting_completed_type
    ON cases (case_type_id, completed_at)
    WHERE completed_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cases_reporting_stage_status
    ON cases (case_type_id, status, current_stage_code);

CREATE INDEX IF NOT EXISTS idx_case_stage_transitions_reporting_time
    ON case_stage_transitions (transitioned_at DESC, case_id, is_regression);

CREATE INDEX IF NOT EXISTS idx_case_stage_transitions_reporting_path
    ON case_stage_transitions (from_stage_code, to_stage_code, is_regression)
    WHERE from_stage_code IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_reporting_execution
    ON tasks (completed_at DESC, started_at, task_definition_code, assigned_service)
    WHERE started_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_reporting_pending_depth
    ON tasks (assigned_service, priority DESC, created_at)
    WHERE status = 'PENDING' AND is_poison_pill = FALSE;

CREATE INDEX IF NOT EXISTS idx_tasks_reporting_retry_depth
    ON tasks (next_retry_at, assigned_service)
    WHERE next_retry_at IS NOT NULL AND status IN ('PENDING', 'FAILED');

CREATE INDEX IF NOT EXISTS idx_task_dlq_reporting_active_depth
    ON task_dlq (case_id, moved_at)
    WHERE soft_deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_events_outbox_reporting_timeline
    ON events_outbox (case_id, created_at DESC, id DESC)
    WHERE case_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_events_outbox_reporting_type_hour
    ON events_outbox (event_type, created_at DESC);
