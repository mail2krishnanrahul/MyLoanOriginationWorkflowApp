package reporting

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/internal/multitenancy"

	"github.com/jmoiron/sqlx"
)

const (
	defaultMetricsRefreshInterval = 5 * time.Minute
	defaultRegressionThreshold    = 3
	metricsRefreshAdvisoryLockKey = int64(260026)
)

// MetricsRefreshJob refreshes reporting snapshots on a fixed schedule.
type MetricsRefreshJob struct {
	db                  *sqlx.DB
	interval            time.Duration
	regressionThreshold int
	logger              *slog.Logger
	advisoryLockKey     int64
	triggerCh           chan struct{}
}

// NewMetricsRefreshJob creates a snapshot refresh job.
func NewMetricsRefreshJob(
	db *sqlx.DB,
	interval time.Duration,
	regressionThreshold int,
	logger *slog.Logger,
) *MetricsRefreshJob {
	if interval <= 0 {
		interval = defaultMetricsRefreshInterval
	}
	if regressionThreshold <= 0 {
		regressionThreshold = defaultRegressionThreshold
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &MetricsRefreshJob{
		db:                  db,
		interval:            interval,
		regressionThreshold: regressionThreshold,
		logger:              logger,
		advisoryLockKey:     metricsRefreshAdvisoryLockKey,
		triggerCh:           make(chan struct{}, 1),
	}
}

// TriggerRefresh schedules an out-of-band refresh without blocking.
func (j *MetricsRefreshJob) TriggerRefresh() {
	if j == nil {
		return
	}
	select {
	case j.triggerCh <- struct{}{}:
	default:
	}
}

// Run executes one refresh cycle.
func (j *MetricsRefreshJob) Run(ctx context.Context) error {
	if j == nil {
		return fmt.Errorf("MetricsRefreshJob.Run: job is nil")
	}
	if j.db == nil {
		return fmt.Errorf("MetricsRefreshJob.Run: db is nil")
	}

	// Advisory lock avoids double-writing snapshots across concurrent engine instances.
	// Upserts stay idempotent as a secondary safety net.
	lockAcquired, err := j.tryAdvisoryLock(ctx)
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: acquire advisory lock: %w", err)
	}
	if !lockAcquired {
		j.logger.Info("metrics refresh skipped; another instance holds advisory lock")
		return nil
	}
	defer func() {
		if releaseErr := j.releaseAdvisoryLock(context.Background()); releaseErr != nil {
			j.logger.Error("failed to release metrics advisory lock", "error", releaseErr)
		}
	}()

	runStartedAt := time.Now().UTC()
	tx, err := j.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	now := time.Now().UTC()
	hourlyStart := now.Add(-72 * time.Hour).Truncate(time.Hour)
	hourlyEnd := now.Truncate(time.Hour)
	dailyStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -180)
	dailyEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	throughputStartedAt := time.Now().UTC()
	hourlyThroughputRows, err := refreshCaseThroughputSnapshotsTx(ctx, tx, MetricBucketHourly, hourlyStart, hourlyEnd)
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: refresh hourly throughput: %w", err)
	}
	dailyThroughputRows, err := refreshCaseThroughputSnapshotsTx(ctx, tx, MetricBucketDaily, dailyStart, dailyEnd)
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: refresh daily throughput: %w", err)
	}
	throughputDuration := time.Since(throughputStartedAt)

	taskMetricsStartedAt := time.Now().UTC()
	taskMetricRows, err := refreshTaskMetricSnapshotsTx(ctx, tx, hourlyStart, hourlyEnd.Add(time.Hour))
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: refresh task metrics: %w", err)
	}
	taskMetricsDuration := time.Since(taskMetricsStartedAt)

	regressionStartedAt := time.Now().UTC()
	regressionRows, err := refreshRegressionSnapshotsTx(ctx, tx, dailyStart, dailyEnd.Add(24*time.Hour), j.regressionThreshold)
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: refresh regression metrics: %w", err)
	}
	regressionDuration := time.Since(regressionStartedAt)

	serviceHealthStartedAt := time.Now().UTC()
	serviceHealthRows, err := refreshServiceHealthSnapshotsTx(ctx, tx, hourlyStart, hourlyEnd.Add(time.Hour))
	if err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: refresh service health metrics: %w", err)
	}
	serviceHealthDuration := time.Since(serviceHealthStartedAt)

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("MetricsRefreshJob.Run: commit: %w", err)
	}
	if gaugeErr := multitenancy.RefreshTenantActiveCasesGauge(ctx, j.db); gaugeErr != nil {
		j.logger.Error("failed to refresh tenant active-case gauge", "error", gaugeErr)
	}

	j.logger.Info("metrics refresh completed",
		"duration", time.Since(runStartedAt),
		"hourly_throughput_rows", hourlyThroughputRows,
		"daily_throughput_rows", dailyThroughputRows,
		"task_metric_rows", taskMetricRows,
		"regression_rows", regressionRows,
		"service_health_rows", serviceHealthRows,
		"throughput_duration", throughputDuration,
		"task_metrics_duration", taskMetricsDuration,
		"regression_duration", regressionDuration,
		"service_health_duration", serviceHealthDuration)

	return nil
}

// Start runs the refresh loop and exits on context cancellation.
func (j *MetricsRefreshJob) Start(ctx context.Context) {
	if j == nil {
		return
	}
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			j.logger.Info("metrics refresh job stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("metrics refresh failed", "error", err)
			}
		case <-j.triggerCh:
			if err := j.Run(ctx); err != nil {
				j.logger.Error("metrics refresh failed (triggered)", "error", err)
			}
		}
	}
}

func (j *MetricsRefreshJob) tryAdvisoryLock(ctx context.Context) (bool, error) {
	var locked bool
	if err := j.db.GetContext(ctx, &locked, `SELECT pg_try_advisory_lock($1)`, j.advisoryLockKey); err != nil {
		return false, fmt.Errorf("tryAdvisoryLock: %w", err)
	}
	return locked, nil
}

func (j *MetricsRefreshJob) releaseAdvisoryLock(ctx context.Context) error {
	var released bool
	if err := j.db.GetContext(ctx, &released, `SELECT pg_advisory_unlock($1)`, j.advisoryLockKey); err != nil {
		return fmt.Errorf("releaseAdvisoryLock: %w", err)
	}
	if !released {
		j.logger.Warn("metrics advisory lock was not held during release")
	}
	return nil
}

func refreshCaseThroughputSnapshotsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	bucket MetricBucket,
	start time.Time,
	end time.Time,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("refreshCaseThroughputSnapshotsTx: tx is nil")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("refreshCaseThroughputSnapshotsTx: end is before start")
	}

	var truncUnit string
	var stepInterval string
	switch bucket {
	case MetricBucketHourly:
		truncUnit = "hour"
		stepInterval = "1 hour"
	case MetricBucketDaily:
		truncUnit = "day"
		stepInterval = "1 day"
	default:
		return 0, fmt.Errorf("refreshCaseThroughputSnapshotsTx: unsupported bucket %s", bucket)
	}

	result, err := tx.ExecContext(ctx, `
		WITH case_dim AS (
			SELECT DISTINCT ct.code AS case_type_code
			FROM cases c
			JOIN case_types ct ON ct.id = c.case_type_id
		),
		series AS (
			SELECT generate_series($1::timestamptz, $2::timestamptz, $3::interval) AS bucket_start
		),
		grid AS (
			SELECT s.bucket_start, d.case_type_code
			FROM series s
			CROSS JOIN case_dim d
		),
		created_counts AS (
			SELECT
				date_trunc($4, c.created_at) AS bucket_start,
				ct.code AS case_type_code,
				COUNT(*)::bigint AS created_count
			FROM cases c
			JOIN case_types ct ON ct.id = c.case_type_id
			WHERE c.created_at >= $1::timestamptz
			  AND c.created_at < $2::timestamptz + $3::interval
			GROUP BY 1, 2
		),
		closed_base AS (
			SELECT
				ct.code AS case_type_code,
				c.status,
				COALESCE(
					c.completed_at,
					CASE
						WHEN c.status IN ('COMPLETED', 'CANCELLED', 'REJECTED') THEN c.updated_at
					END
				) AS closed_at
			FROM cases c
			JOIN case_types ct ON ct.id = c.case_type_id
		),
		closed_counts AS (
			SELECT
				date_trunc($4, cb.closed_at) AS bucket_start,
				cb.case_type_code,
				COUNT(*) FILTER (WHERE cb.status = 'COMPLETED')::bigint AS completed_count,
				COUNT(*) FILTER (WHERE cb.status = 'CANCELLED')::bigint AS cancelled_count
			FROM closed_base cb
			WHERE cb.closed_at IS NOT NULL
			  AND cb.closed_at >= $1::timestamptz
			  AND cb.closed_at < $2::timestamptz + $3::interval
			GROUP BY 1, 2
		),
		baseline AS (
			SELECT
				ct.code AS case_type_code,
				COUNT(*)::bigint AS baseline_inflight
			FROM cases c
			JOIN case_types ct ON ct.id = c.case_type_id
			WHERE c.created_at < $1::timestamptz
			  AND (
				COALESCE(
					c.completed_at,
					CASE
						WHEN c.status IN ('COMPLETED', 'CANCELLED', 'REJECTED') THEN c.updated_at
					END
				) IS NULL
				OR COALESCE(
					c.completed_at,
					CASE
						WHEN c.status IN ('COMPLETED', 'CANCELLED', 'REJECTED') THEN c.updated_at
					END
				) >= $1::timestamptz
			  )
			GROUP BY ct.code
		),
		joined AS (
			SELECT
				g.bucket_start,
				g.case_type_code,
				COALESCE(cc.created_count, 0)::bigint AS created_count,
				COALESCE(cl.completed_count, 0)::bigint AS completed_count,
				COALESCE(cl.cancelled_count, 0)::bigint AS cancelled_count,
				COALESCE(b.baseline_inflight, 0)::bigint AS baseline_inflight
			FROM grid g
			LEFT JOIN created_counts cc
			  ON cc.bucket_start = g.bucket_start
			 AND cc.case_type_code = g.case_type_code
			LEFT JOIN closed_counts cl
			  ON cl.bucket_start = g.bucket_start
			 AND cl.case_type_code = g.case_type_code
			LEFT JOIN baseline b
			  ON b.case_type_code = g.case_type_code
		),
		final_rows AS (
			SELECT
				$5::varchar(10) AS bucket,
				j.bucket_start,
				j.case_type_code,
				j.created_count,
				j.completed_count,
				j.cancelled_count,
				(
					j.baseline_inflight
					+ SUM(j.created_count - j.completed_count - j.cancelled_count)
					  OVER (
						PARTITION BY j.case_type_code
						ORDER BY j.bucket_start
						ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
					  )
				)::bigint AS inflight_count
			FROM joined j
		)
		INSERT INTO case_throughput_snapshots (
			bucket,
			bucket_start,
			case_type_code,
			created_count,
			completed_count,
			cancelled_count,
			inflight_count,
			snapshot_refreshed_at,
			created_at,
			updated_at
		)
		SELECT
			fr.bucket,
			fr.bucket_start,
			fr.case_type_code,
			fr.created_count,
			fr.completed_count,
			fr.cancelled_count,
			fr.inflight_count,
			now(),
			now(),
			now()
		FROM final_rows fr
		ON CONFLICT (bucket, bucket_start, case_type_code)
		DO UPDATE SET
			created_count = EXCLUDED.created_count,
			completed_count = EXCLUDED.completed_count,
			cancelled_count = EXCLUDED.cancelled_count,
			inflight_count = EXCLUDED.inflight_count,
			snapshot_refreshed_at = now(),
			updated_at = now()
	`, start.UTC(), end.UTC(), stepInterval, truncUnit, string(bucket))
	if err != nil {
		return 0, fmt.Errorf("refreshCaseThroughputSnapshotsTx: upsert snapshots: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

func refreshTaskMetricSnapshotsTx(ctx context.Context, tx *sqlx.Tx, start time.Time, end time.Time) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("refreshTaskMetricSnapshotsTx: tx is nil")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("refreshTaskMetricSnapshotsTx: end is before start")
	}

	result, err := tx.ExecContext(ctx, `
		WITH base AS (
			SELECT
				date_trunc('hour', COALESCE(t.completed_at, t.updated_at, t.created_at)) AS bucket_start,
				t.task_definition_code,
				COALESCE(NULLIF(t.assigned_service, ''), 'UNASSIGNED') AS assigned_service,
				COUNT(*)::bigint AS total_tasks,
				COUNT(*) FILTER (WHERE t.status = 'COMPLETED')::bigint AS completed_tasks,
				COUNT(*) FILTER (WHERE t.status = 'FAILED')::bigint AS failed_tasks,
				COUNT(*) FILTER (WHERE t.retry_count > 0)::bigint AS retried_tasks,
				COALESCE(
					AVG(EXTRACT(EPOCH FROM (t.completed_at - t.started_at)))
						FILTER (WHERE t.started_at IS NOT NULL AND t.completed_at IS NOT NULL),
					0
				)::double precision AS avg_execution_seconds,
				COALESCE(
					percentile_cont(0.50) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (t.completed_at - t.started_at)))
						FILTER (WHERE t.started_at IS NOT NULL AND t.completed_at IS NOT NULL),
					0
				)::double precision AS p50_execution_seconds,
				COALESCE(
					percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (t.completed_at - t.started_at)))
						FILTER (WHERE t.started_at IS NOT NULL AND t.completed_at IS NOT NULL),
					0
				)::double precision AS p95_execution_seconds,
				COALESCE(
					percentile_cont(0.99) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (t.completed_at - t.started_at)))
						FILTER (WHERE t.started_at IS NOT NULL AND t.completed_at IS NOT NULL),
					0
				)::double precision AS p99_execution_seconds
			FROM tasks t
			WHERE COALESCE(t.completed_at, t.updated_at, t.created_at) >= $1
			  AND COALESCE(t.completed_at, t.updated_at, t.created_at) < $2
			GROUP BY 1, 2, 3
		),
		dlq AS (
			SELECT
				date_trunc('hour', d.moved_at) AS bucket_start,
				t.task_definition_code,
				COALESCE(NULLIF(t.assigned_service, ''), 'UNASSIGNED') AS assigned_service,
				COUNT(DISTINCT d.task_id)::bigint AS dlq_tasks
			FROM task_dlq d
			JOIN tasks t ON t.id = d.task_id
			WHERE d.moved_at >= $1
			  AND d.moved_at < $2
			GROUP BY 1, 2, 3
		),
		combined AS (
			SELECT
				COALESCE(b.bucket_start, d.bucket_start) AS bucket_start,
				COALESCE(b.task_definition_code, d.task_definition_code) AS task_definition_code,
				COALESCE(b.assigned_service, d.assigned_service) AS assigned_service,
				COALESCE(b.total_tasks, 0)::bigint AS total_tasks,
				COALESCE(b.completed_tasks, 0)::bigint AS completed_tasks,
				COALESCE(b.failed_tasks, 0)::bigint AS failed_tasks,
				COALESCE(b.retried_tasks, 0)::bigint AS retried_tasks,
				COALESCE(d.dlq_tasks, 0)::bigint AS dlq_tasks,
				COALESCE(b.avg_execution_seconds, 0)::double precision AS avg_execution_seconds,
				COALESCE(b.p50_execution_seconds, 0)::double precision AS p50_execution_seconds,
				COALESCE(b.p95_execution_seconds, 0)::double precision AS p95_execution_seconds,
				COALESCE(b.p99_execution_seconds, 0)::double precision AS p99_execution_seconds
			FROM base b
			FULL OUTER JOIN dlq d
			  ON b.bucket_start = d.bucket_start
			 AND b.task_definition_code = d.task_definition_code
			 AND b.assigned_service = d.assigned_service
		)
		INSERT INTO task_metrics_snapshots (
			bucket,
			bucket_start,
			task_definition_code,
			assigned_service,
			total_tasks,
			completed_tasks,
			failed_tasks,
			retried_tasks,
			dlq_tasks,
			avg_execution_seconds,
			p50_execution_seconds,
			p95_execution_seconds,
			p99_execution_seconds,
			retry_rate_percent,
			failure_rate_percent,
			dlq_rate_percent,
			snapshot_refreshed_at,
			created_at,
			updated_at
		)
		SELECT
			'HOURLY',
			c.bucket_start,
			c.task_definition_code,
			c.assigned_service,
			c.total_tasks,
			c.completed_tasks,
			c.failed_tasks,
			c.retried_tasks,
			c.dlq_tasks,
			c.avg_execution_seconds,
			c.p50_execution_seconds,
			c.p95_execution_seconds,
			c.p99_execution_seconds,
			CASE
				WHEN c.total_tasks = 0 THEN 0
				ELSE (c.retried_tasks::numeric * 100.0) / c.total_tasks::numeric
			END,
			CASE
				WHEN c.total_tasks = 0 THEN 0
				ELSE (c.failed_tasks::numeric * 100.0) / c.total_tasks::numeric
			END,
			CASE
				WHEN c.total_tasks = 0 THEN 0
				ELSE (c.dlq_tasks::numeric * 100.0) / c.total_tasks::numeric
			END,
			now(),
			now(),
			now()
		FROM combined c
		WHERE c.bucket_start IS NOT NULL
		ON CONFLICT (bucket, bucket_start, task_definition_code, assigned_service)
		DO UPDATE SET
			total_tasks = EXCLUDED.total_tasks,
			completed_tasks = EXCLUDED.completed_tasks,
			failed_tasks = EXCLUDED.failed_tasks,
			retried_tasks = EXCLUDED.retried_tasks,
			dlq_tasks = EXCLUDED.dlq_tasks,
			avg_execution_seconds = EXCLUDED.avg_execution_seconds,
			p50_execution_seconds = EXCLUDED.p50_execution_seconds,
			p95_execution_seconds = EXCLUDED.p95_execution_seconds,
			p99_execution_seconds = EXCLUDED.p99_execution_seconds,
			retry_rate_percent = EXCLUDED.retry_rate_percent,
			failure_rate_percent = EXCLUDED.failure_rate_percent,
			dlq_rate_percent = EXCLUDED.dlq_rate_percent,
			snapshot_refreshed_at = now(),
			updated_at = now()
	`, start.UTC(), end.UTC())
	if err != nil {
		return 0, fmt.Errorf("refreshTaskMetricSnapshotsTx: upsert snapshots: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

func refreshRegressionSnapshotsTx(
	ctx context.Context,
	tx *sqlx.Tx,
	start time.Time,
	end time.Time,
	regressionThreshold int,
) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("refreshRegressionSnapshotsTx: tx is nil")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("refreshRegressionSnapshotsTx: end is before start")
	}
	if regressionThreshold <= 0 {
		return 0, fmt.Errorf("refreshRegressionSnapshotsTx: regressionThreshold must be > 0")
	}

	result, err := tx.ExecContext(ctx, `
		WITH transitions AS (
			SELECT
				date_trunc('day', cst.transitioned_at)::date AS snapshot_date,
				ct.code AS case_type_code,
				cst.case_id::text AS case_id,
				cst.from_stage_code,
				cst.to_stage_code,
				cst.is_regression
			FROM case_stage_transitions cst
			JOIN cases c ON c.id = cst.case_id
			JOIN case_types ct ON ct.id = c.case_type_id
			WHERE cst.transitioned_at >= $1
			  AND cst.transitioned_at < $2
		),
		counts AS (
			SELECT
				t.snapshot_date,
				t.case_type_code,
				COUNT(*) FILTER (WHERE t.is_regression = FALSE AND t.from_stage_code IS NOT NULL)::bigint AS forward_transition_count,
				COUNT(*) FILTER (WHERE t.is_regression = TRUE)::bigint AS regression_count
			FROM transitions t
			GROUP BY t.snapshot_date, t.case_type_code
		),
		paths_ranked AS (
			SELECT
				t.snapshot_date,
				t.case_type_code,
				t.from_stage_code,
				t.to_stage_code,
				COUNT(*)::bigint AS path_count,
				ROW_NUMBER() OVER (
					PARTITION BY t.snapshot_date, t.case_type_code
					ORDER BY COUNT(*) DESC, t.from_stage_code ASC, t.to_stage_code ASC
				) AS rn
			FROM transitions t
			WHERE t.is_regression = TRUE
			  AND t.from_stage_code IS NOT NULL
			GROUP BY t.snapshot_date, t.case_type_code, t.from_stage_code, t.to_stage_code
		),
		paths AS (
			SELECT
				pr.snapshot_date,
				pr.case_type_code,
				COALESCE(
					jsonb_agg(
						jsonb_build_object(
							'from_stage_code', pr.from_stage_code,
							'to_stage_code', pr.to_stage_code,
							'count', pr.path_count
						)
						ORDER BY pr.path_count DESC, pr.from_stage_code ASC, pr.to_stage_code ASC
					) FILTER (WHERE pr.rn <= 10),
					'[]'::jsonb
				) AS most_common_paths
			FROM paths_ranked pr
			GROUP BY pr.snapshot_date, pr.case_type_code
		),
		flagged_ranked AS (
			SELECT
				t.snapshot_date,
				t.case_type_code,
				t.case_id,
				COUNT(*) FILTER (WHERE t.is_regression = TRUE)::bigint AS regression_count,
				ROW_NUMBER() OVER (
					PARTITION BY t.snapshot_date, t.case_type_code
					ORDER BY COUNT(*) FILTER (WHERE t.is_regression = TRUE) DESC, t.case_id ASC
				) AS rn
			FROM transitions t
			GROUP BY t.snapshot_date, t.case_type_code, t.case_id
			HAVING COUNT(*) FILTER (WHERE t.is_regression = TRUE) > $3
		),
		flagged AS (
			SELECT
				fr.snapshot_date,
				fr.case_type_code,
				COUNT(*)::bigint AS cases_above_threshold,
				COALESCE(
					jsonb_agg(
						jsonb_build_object(
							'case_id', fr.case_id,
							'regression_count', fr.regression_count
						)
						ORDER BY fr.regression_count DESC, fr.case_id ASC
					) FILTER (WHERE fr.rn <= 200),
					'[]'::jsonb
				) AS flagged_cases
			FROM flagged_ranked fr
			GROUP BY fr.snapshot_date, fr.case_type_code
		)
		INSERT INTO regression_metrics_snapshots (
			snapshot_date,
			case_type_code,
			forward_transition_count,
			regression_count,
			regression_rate_percent,
			most_common_paths,
			cases_above_threshold,
			flagged_cases,
			regression_threshold,
			snapshot_refreshed_at,
			created_at,
			updated_at
		)
		SELECT
			c.snapshot_date,
			c.case_type_code,
			c.forward_transition_count,
			c.regression_count,
			CASE
				WHEN c.forward_transition_count = 0 THEN 0
				ELSE (c.regression_count::numeric * 100.0) / c.forward_transition_count::numeric
			END AS regression_rate_percent,
			COALESCE(p.most_common_paths, '[]'::jsonb),
			COALESCE(f.cases_above_threshold, 0)::bigint,
			COALESCE(f.flagged_cases, '[]'::jsonb),
			$3::int,
			now(),
			now(),
			now()
		FROM counts c
		LEFT JOIN paths p
		  ON p.snapshot_date = c.snapshot_date
		 AND p.case_type_code = c.case_type_code
		LEFT JOIN flagged f
		  ON f.snapshot_date = c.snapshot_date
		 AND f.case_type_code = c.case_type_code
		ON CONFLICT (snapshot_date, case_type_code)
		DO UPDATE SET
			forward_transition_count = EXCLUDED.forward_transition_count,
			regression_count = EXCLUDED.regression_count,
			regression_rate_percent = EXCLUDED.regression_rate_percent,
			most_common_paths = EXCLUDED.most_common_paths,
			cases_above_threshold = EXCLUDED.cases_above_threshold,
			flagged_cases = EXCLUDED.flagged_cases,
			regression_threshold = EXCLUDED.regression_threshold,
			snapshot_refreshed_at = now(),
			updated_at = now()
	`, start.UTC(), end.UTC(), regressionThreshold)
	if err != nil {
		return 0, fmt.Errorf("refreshRegressionSnapshotsTx: upsert snapshots: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

func refreshServiceHealthSnapshotsTx(ctx context.Context, tx *sqlx.Tx, start time.Time, end time.Time) (int64, error) {
	if tx == nil {
		return 0, fmt.Errorf("refreshServiceHealthSnapshotsTx: tx is nil")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("refreshServiceHealthSnapshotsTx: end is before start")
	}

	result, err := tx.ExecContext(ctx, `
		WITH base AS (
			SELECT
				bucket_start,
				COALESCE(NULLIF(assigned_service, ''), 'UNASSIGNED') AS assigned_service,
				SUM(total_tasks)::bigint AS total_tasks,
				SUM(failed_tasks)::bigint AS failed_tasks,
				SUM(retried_tasks)::bigint AS retried_tasks,
				SUM(dlq_tasks)::bigint AS dlq_tasks,
				CASE
					WHEN SUM(total_tasks) = 0 THEN 0
					ELSE COALESCE(SUM(avg_execution_seconds * total_tasks), 0)::double precision
					     / SUM(total_tasks)::double precision
				END AS avg_execution_seconds
			FROM task_metrics_snapshots
			WHERE bucket = 'HOURLY'
			  AND bucket_start >= $1
			  AND bucket_start < $2
			GROUP BY bucket_start, assigned_service
		),
		totals AS (
			SELECT
				bucket_start,
				SUM(dlq_tasks)::bigint AS total_dlq_tasks
			FROM base
			GROUP BY bucket_start
		)
		INSERT INTO service_health_snapshots (
			bucket,
			bucket_start,
			assigned_service,
			total_tasks,
			failed_tasks,
			retried_tasks,
			dlq_tasks,
			failure_rate_percent,
			avg_execution_seconds,
			retry_rate_percent,
			dlq_rate_percent,
			dlq_contribution_rate_percent,
			snapshot_refreshed_at,
			created_at,
			updated_at
		)
		SELECT
			'HOURLY',
			b.bucket_start,
			b.assigned_service,
			b.total_tasks,
			b.failed_tasks,
			b.retried_tasks,
			b.dlq_tasks,
			CASE
				WHEN b.total_tasks = 0 THEN 0
				ELSE (b.failed_tasks::numeric * 100.0) / b.total_tasks::numeric
			END AS failure_rate_percent,
			b.avg_execution_seconds,
			CASE
				WHEN b.total_tasks = 0 THEN 0
				ELSE (b.retried_tasks::numeric * 100.0) / b.total_tasks::numeric
			END AS retry_rate_percent,
			CASE
				WHEN b.total_tasks = 0 THEN 0
				ELSE (b.dlq_tasks::numeric * 100.0) / b.total_tasks::numeric
			END AS dlq_rate_percent,
			CASE
				WHEN COALESCE(t.total_dlq_tasks, 0) = 0 THEN 0
				ELSE (b.dlq_tasks::numeric * 100.0) / t.total_dlq_tasks::numeric
			END AS dlq_contribution_rate_percent,
			now(),
			now(),
			now()
		FROM base b
		LEFT JOIN totals t ON t.bucket_start = b.bucket_start
		ON CONFLICT (bucket, bucket_start, assigned_service)
		DO UPDATE SET
			total_tasks = EXCLUDED.total_tasks,
			failed_tasks = EXCLUDED.failed_tasks,
			retried_tasks = EXCLUDED.retried_tasks,
			dlq_tasks = EXCLUDED.dlq_tasks,
			failure_rate_percent = EXCLUDED.failure_rate_percent,
			avg_execution_seconds = EXCLUDED.avg_execution_seconds,
			retry_rate_percent = EXCLUDED.retry_rate_percent,
			dlq_rate_percent = EXCLUDED.dlq_rate_percent,
			dlq_contribution_rate_percent = EXCLUDED.dlq_contribution_rate_percent,
			snapshot_refreshed_at = now(),
			updated_at = now()
	`, start.UTC(), end.UTC())
	if err != nil {
		return 0, fmt.Errorf("refreshServiceHealthSnapshotsTx: upsert snapshots: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}
