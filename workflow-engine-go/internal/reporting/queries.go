package reporting

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// GetCaseThroughput returns pre-aggregated throughput rows for a case type and bucket.
func GetCaseThroughput(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
	from, to time.Time,
	bucket MetricBucket,
) ([]ThroughputRow, error) {
	if db == nil {
		return nil, fmt.Errorf("GetCaseThroughput: db is nil")
	}
	if to.Before(from) {
		return nil, fmt.Errorf("GetCaseThroughput: to is before from")
	}
	normalizedCaseType := normalizeCaseTypeCode(caseTypeCode)
	if normalizedCaseType == "" {
		return nil, fmt.Errorf("GetCaseThroughput: caseTypeCode is required")
	}
	normalizedBucket, err := normalizeBucket(bucket)
	if err != nil {
		return nil, fmt.Errorf("GetCaseThroughput: %w", err)
	}

	rows := make([]ThroughputRow, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			bucket_start,
			created_count,
			completed_count,
			cancelled_count,
			inflight_count
		FROM case_throughput_snapshots
		WHERE case_type_code = $1
		  AND bucket = $2
		  AND bucket_start >= $3
		  AND bucket_start <= $4
		ORDER BY bucket_start ASC
	`, normalizedCaseType, string(normalizedBucket), from.UTC(), to.UTC()); err != nil {
		return nil, fmt.Errorf("GetCaseThroughput: query snapshots: %w", err)
	}
	if rows == nil {
		return []ThroughputRow{}, nil
	}
	return rows, nil
}

// GetStageFunnel returns in-flight stage counts and forward dwell analytics.
func GetStageFunnel(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
) ([]StageFunnelRow, error) {
	if db == nil {
		return nil, fmt.Errorf("GetStageFunnel: db is nil")
	}
	normalizedCaseType := normalizeCaseTypeCode(caseTypeCode)
	if normalizedCaseType == "" {
		return nil, fmt.Errorf("GetStageFunnel: caseTypeCode is required")
	}

	rows := make([]StageFunnelRow, 0)
	if err := db.SelectContext(ctx, &rows, `
		WITH selected_case_type AS (
			SELECT id, version, config
			FROM case_types
			WHERE code = $1
			ORDER BY (status = 'ACTIVE') DESC, version DESC
			LIMIT 1
		),
		stage_catalog AS (
			SELECT
				sd.stage_code,
				sd.ordinal AS stage_ordinal,
				CASE
					WHEN cfg_stage.stg IS NULL THEN NULL
					WHEN NULLIF(cfg_stage.stg->'sla'->>'duration_hours', '') IS NULL THEN NULL
					ELSE ((cfg_stage.stg->'sla'->>'duration_hours')::double precision * 3600.0)
				END AS sla_threshold_seconds
			FROM selected_case_type sct
			JOIN stage_definitions sd
			  ON sd.case_type_id = sct.id
			 AND sd.case_type_version = sct.version
			LEFT JOIN LATERAL (
				SELECT stg
				FROM jsonb_array_elements(COALESCE(sct.config->'stages', '[]'::jsonb)) AS stg
				WHERE stg->>'code' = sd.stage_code
				LIMIT 1
			) cfg_stage ON TRUE
		),
		current_counts AS (
			SELECT
				c.current_stage_code AS stage_code,
				COUNT(*)::bigint AS in_flight_count
			FROM cases c
			JOIN case_types ct ON ct.id = c.case_type_id
			WHERE ct.code = $1
			  AND c.current_stage_code IS NOT NULL
			  AND c.status IN ('OPEN', 'IN_PROGRESS', 'SUSPENDED', 'EXCEPTION')
			GROUP BY c.current_stage_code
		),
		ordered_transitions AS (
			SELECT
				t.id,
				t.case_id,
				t.from_stage_code,
				t.to_stage_code,
				t.is_regression,
				t.transitioned_at,
				LAG(t.transitioned_at) OVER (PARTITION BY t.case_id ORDER BY t.transitioned_at, t.id) AS prev_transitioned_at,
				LAG(t.to_stage_code) OVER (PARTITION BY t.case_id ORDER BY t.transitioned_at, t.id) AS prev_to_stage_code
			FROM case_stage_transitions t
			JOIN cases c ON c.id = t.case_id
			JOIN case_types ct ON ct.id = c.case_type_id
			WHERE ct.code = $1
		),
		forward_dwell AS (
			SELECT
				ot.from_stage_code AS stage_code,
				AVG(EXTRACT(EPOCH FROM (ot.transitioned_at - ot.prev_transitioned_at)))::double precision AS avg_forward_dwell_seconds,
				COALESCE(
					percentile_cont(0.95) WITHIN GROUP (
						ORDER BY EXTRACT(EPOCH FROM (ot.transitioned_at - ot.prev_transitioned_at))
					),
					0
				)::double precision AS p95_forward_dwell_seconds,
				COUNT(*)::bigint AS forward_transition_count
			FROM ordered_transitions ot
			WHERE ot.is_regression = FALSE
			  AND ot.from_stage_code IS NOT NULL
			  AND ot.prev_transitioned_at IS NOT NULL
			  AND ot.prev_to_stage_code = ot.from_stage_code
			GROUP BY ot.from_stage_code
		),
		regression_counts AS (
			SELECT
				ot.from_stage_code AS stage_code,
				COUNT(*) FILTER (WHERE ot.is_regression = TRUE)::bigint AS regression_count,
				COUNT(*) FILTER (WHERE ot.is_regression = FALSE AND ot.from_stage_code IS NOT NULL)::bigint AS forward_count_for_rate
			FROM ordered_transitions ot
			WHERE ot.from_stage_code IS NOT NULL
			GROUP BY ot.from_stage_code
		)
		SELECT
			sc.stage_code,
			sc.stage_ordinal,
			COALESCE(cc.in_flight_count, 0)::bigint AS in_flight_count,
			COALESCE(fd.avg_forward_dwell_seconds, 0)::double precision AS avg_forward_dwell_seconds,
			COALESCE(fd.p95_forward_dwell_seconds, 0)::double precision AS p95_forward_dwell_seconds,
			COALESCE(fd.forward_transition_count, 0)::bigint AS forward_transition_count,
			COALESCE(rc.regression_count, 0)::bigint AS regression_count,
			CASE
				WHEN COALESCE(rc.forward_count_for_rate, 0) = 0 THEN 0
				ELSE (COALESCE(rc.regression_count, 0)::double precision * 100.0) / rc.forward_count_for_rate::double precision
			END AS regression_rate_percent,
			sc.sla_threshold_seconds,
			CASE
				WHEN sc.sla_threshold_seconds IS NULL OR sc.sla_threshold_seconds <= 0 THEN FALSE
				WHEN COALESCE(fd.avg_forward_dwell_seconds, 0) > sc.sla_threshold_seconds THEN TRUE
				ELSE FALSE
			END AS is_abnormal_dwell
		FROM stage_catalog sc
		LEFT JOIN current_counts cc ON cc.stage_code = sc.stage_code
		LEFT JOIN forward_dwell fd ON fd.stage_code = sc.stage_code
		LEFT JOIN regression_counts rc ON rc.stage_code = sc.stage_code
		ORDER BY sc.stage_ordinal ASC
	`, normalizedCaseType); err != nil {
		return nil, fmt.Errorf("GetStageFunnel: query funnel: %w", err)
	}
	if rows == nil {
		return []StageFunnelRow{}, nil
	}
	return rows, nil
}

// GetTaskMetrics returns a summary from pre-aggregated task_metrics_snapshots.
func GetTaskMetrics(
	ctx context.Context,
	db *sqlx.DB,
	taskDefCode string,
	assignedService string,
	from, to time.Time,
) (TaskMetricsSummary, error) {
	if db == nil {
		return TaskMetricsSummary{}, fmt.Errorf("GetTaskMetrics: db is nil")
	}
	if to.Before(from) {
		return TaskMetricsSummary{}, fmt.Errorf("GetTaskMetrics: to is before from")
	}
	taskCode := strings.TrimSpace(taskDefCode)
	if taskCode == "" {
		return TaskMetricsSummary{}, fmt.Errorf("GetTaskMetrics: taskDefCode is required")
	}
	service := strings.TrimSpace(assignedService)

	summary := TaskMetricsSummary{TaskDefinitionCode: taskCode}
	if service != "" {
		summary.AssignedService = service
	}

	err := db.GetContext(ctx, &summary, `
		SELECT
			COALESCE(SUM(total_tasks), 0)::bigint AS total_tasks,
			COALESCE(SUM(completed_tasks), 0)::bigint AS completed_tasks,
			COALESCE(SUM(failed_tasks), 0)::bigint AS failed_tasks,
			COALESCE(SUM(retried_tasks), 0)::bigint AS retried_tasks,
			COALESCE(SUM(dlq_tasks), 0)::bigint AS dlq_tasks,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE COALESCE(SUM(avg_execution_seconds * total_tasks), 0)::double precision
				     / SUM(total_tasks)::double precision
			END AS avg_execution_seconds,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE COALESCE(SUM(p50_execution_seconds * total_tasks), 0)::double precision
				     / SUM(total_tasks)::double precision
			END AS p50_execution_seconds,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE COALESCE(SUM(p95_execution_seconds * total_tasks), 0)::double precision
				     / SUM(total_tasks)::double precision
			END AS p95_execution_seconds,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE COALESCE(SUM(p99_execution_seconds * total_tasks), 0)::double precision
				     / SUM(total_tasks)::double precision
			END AS p99_execution_seconds,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE (COALESCE(SUM(retried_tasks), 0)::double precision * 100.0)
				     / SUM(total_tasks)::double precision
			END AS retry_rate_percent,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE (COALESCE(SUM(failed_tasks), 0)::double precision * 100.0)
				     / SUM(total_tasks)::double precision
			END AS failure_rate_percent,
			CASE
				WHEN COALESCE(SUM(total_tasks), 0) = 0 THEN 0
				ELSE (COALESCE(SUM(dlq_tasks), 0)::double precision * 100.0)
				     / SUM(total_tasks)::double precision
			END AS dlq_rate_percent
		FROM task_metrics_snapshots
		WHERE bucket = 'HOURLY'
		  AND task_definition_code = $1
		  AND bucket_start >= $2
		  AND bucket_start <= $3
		  AND ($4 = '' OR assigned_service = $4)
	`, taskCode, from.UTC(), to.UTC(), service)
	if err != nil {
		return TaskMetricsSummary{}, fmt.Errorf("GetTaskMetrics: query summary: %w", err)
	}
	return summary, nil
}

// GetSLAComplianceReport returns daily compliance, at-risk, and breached case lists.
func GetSLAComplianceReport(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
	from, to time.Time,
) (SLAComplianceReport, error) {
	if db == nil {
		return SLAComplianceReport{}, fmt.Errorf("GetSLAComplianceReport: db is nil")
	}
	if to.Before(from) {
		return SLAComplianceReport{}, fmt.Errorf("GetSLAComplianceReport: to is before from")
	}
	caseType := normalizeCaseTypeCode(caseTypeCode)
	if caseType == "" {
		return SLAComplianceReport{}, fmt.Errorf("GetSLAComplianceReport: caseTypeCode is required")
	}

	report := SLAComplianceReport{
		CaseTypeCode:  caseType,
		Daily:         []SLAComplianceDailyRow{},
		AtRiskCases:   []SLAAtRiskCase{},
		BreachedCases: []SLABreachedCase{},
	}

	dailyRows := make([]SLAComplianceDailyRow, 0)
	if err := db.SelectContext(ctx, &dailyRows, `
		SELECT
			date_trunc('day', COALESCE(c.completed_at, c.created_at))::timestamptz AS metric_day,
			COUNT(*) FILTER (WHERE c.completed_at IS NOT NULL)::bigint AS completed_cases,
			COUNT(*) FILTER (
				WHERE c.completed_at IS NOT NULL
				  AND c.case_due_at IS NOT NULL
				  AND c.completed_at <= c.case_due_at
			)::bigint AS compliant_cases,
			COUNT(*) FILTER (
				WHERE c.completed_at IS NOT NULL
				  AND c.case_due_at IS NOT NULL
				  AND c.completed_at > c.case_due_at
			)::bigint AS breached_cases
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE ct.code = $1
		  AND COALESCE(c.completed_at, c.created_at) >= $2
		  AND COALESCE(c.completed_at, c.created_at) < $3
		GROUP BY 1
		ORDER BY 1 ASC
	`, caseType, from.UTC(), to.UTC()); err != nil {
		return SLAComplianceReport{}, fmt.Errorf("GetSLAComplianceReport: query daily: %w", err)
	}

	for i := range dailyRows {
		if dailyRows[i].CompletedCases > 0 {
			dailyRows[i].ComplianceRatePercent = (float64(dailyRows[i].CompliantCases) * 100.0) / float64(dailyRows[i].CompletedCases)
		}
		report.TotalCompletedCases += dailyRows[i].CompletedCases
		report.TotalCompliantCases += dailyRows[i].CompliantCases
		report.TotalBreachedCases += dailyRows[i].BreachedCases
	}
	if report.TotalCompletedCases > 0 {
		report.ComplianceRatePercent = (float64(report.TotalCompliantCases) * 100.0) / float64(report.TotalCompletedCases)
	}
	report.Daily = dailyRows

	if err := db.SelectContext(ctx, &report.AtRiskCases, `
		SELECT
			c.id::text AS case_id,
			c.reference_number,
			c.status,
			c.current_stage_code,
			c.case_due_at AS sla_deadline,
			EXTRACT(EPOCH FROM (c.case_due_at - now())) / 3600.0 AS hours_to_deadline
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE ct.code = $1
		  AND c.case_due_at IS NOT NULL
		  AND c.case_due_at >= $2
		  AND c.case_due_at < $3
		  AND c.status IN ('OPEN', 'IN_PROGRESS', 'SUSPENDED', 'EXCEPTION')
		  AND c.case_due_at > now()
		  AND c.case_due_at <= now() + interval '4 hours'
		ORDER BY c.case_due_at ASC
	`, caseType, from.UTC(), to.UTC()); err != nil {
		return SLAComplianceReport{}, fmt.Errorf("GetSLAComplianceReport: query at-risk cases: %w", err)
	}

	if err := db.SelectContext(ctx, &report.BreachedCases, `
		SELECT
			c.id::text AS case_id,
			c.reference_number,
			c.status,
			c.current_stage_code,
			c.case_due_at AS sla_deadline,
			c.completed_at,
			CASE
				WHEN c.completed_at IS NOT NULL THEN EXTRACT(EPOCH FROM (c.completed_at - c.case_due_at)) / 3600.0
				ELSE EXTRACT(EPOCH FROM (now() - c.case_due_at)) / 3600.0
			END AS breach_hours
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE ct.code = $1
		  AND c.case_due_at IS NOT NULL
		  AND c.case_due_at >= $2
		  AND c.case_due_at < $3
		  AND (
			(c.completed_at IS NOT NULL AND c.completed_at > c.case_due_at)
			OR (
				c.completed_at IS NULL
				AND c.status IN ('OPEN', 'IN_PROGRESS', 'SUSPENDED', 'EXCEPTION')
				AND now() > c.case_due_at
			)
		  )
		ORDER BY c.case_due_at ASC
	`, caseType, from.UTC(), to.UTC()); err != nil {
		return SLAComplianceReport{}, fmt.Errorf("GetSLAComplianceReport: query breached cases: %w", err)
	}

	return report, nil
}

// GetQueueDepth returns live operational queue depth metrics.
func GetQueueDepth(
	ctx context.Context,
	db *sqlx.DB,
) (QueueDepthSnapshot, error) {
	if db == nil {
		return QueueDepthSnapshot{}, fmt.Errorf("GetQueueDepth: db is nil")
	}

	// Fast polling query is preferred over a materialized view because queue state is mutable
	// at sub-minute frequency and 30-second refresh windows benefit from index-backed live counts.
	snapshot := QueueDepthSnapshot{
		CapturedAt:               time.Now().UTC(),
		PendingByServicePriority: []QueueDepthPendingRow{},
		RetryQueue:               []QueueDepthRetryRow{},
		DLQDepthByCaseType:       []QueueDepthDLQRow{},
	}

	if err := db.SelectContext(ctx, &snapshot.PendingByServicePriority, `
		SELECT
			COALESCE(NULLIF(assigned_service, ''), 'UNASSIGNED') AS assigned_service,
			priority,
			COUNT(*)::bigint AS pending_count,
			COALESCE(EXTRACT(EPOCH FROM (now() - MIN(created_at))), 0)::bigint AS oldest_age_seconds
		FROM tasks
		WHERE status = 'PENDING'
		  AND is_poison_pill = FALSE
		  AND (next_retry_at IS NULL OR next_retry_at <= now())
		GROUP BY 1, 2
		ORDER BY pending_count DESC, assigned_service ASC, priority DESC
	`); err != nil {
		return QueueDepthSnapshot{}, fmt.Errorf("GetQueueDepth: query pending depth: %w", err)
	}

	if err := db.SelectContext(ctx, &snapshot.RetryQueue, `
		SELECT
			COALESCE(NULLIF(assigned_service, ''), 'UNASSIGNED') AS assigned_service,
			next_retry_at,
			COUNT(*)::bigint AS retry_count
		FROM tasks
		WHERE status IN ('PENDING', 'FAILED')
		  AND is_poison_pill = FALSE
		  AND next_retry_at IS NOT NULL
		  AND next_retry_at > now()
		GROUP BY 1, 2
		ORDER BY next_retry_at ASC, assigned_service ASC
	`); err != nil {
		return QueueDepthSnapshot{}, fmt.Errorf("GetQueueDepth: query retry depth: %w", err)
	}

	if err := db.SelectContext(ctx, &snapshot.DLQDepthByCaseType, `
		SELECT
			ct.code AS case_type_code,
			COUNT(*)::bigint AS depth
		FROM task_dlq d
		JOIN cases c ON c.id = d.case_id
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE d.soft_deleted_at IS NULL
		GROUP BY ct.code
		ORDER BY depth DESC, ct.code ASC
	`); err != nil {
		return QueueDepthSnapshot{}, fmt.Errorf("GetQueueDepth: query dlq depth: %w", err)
	}

	if err := db.GetContext(ctx, &snapshot.OldestPendingAgeSeconds, `
		SELECT COALESCE(MAX(EXTRACT(EPOCH FROM (now() - created_at))), 0)::bigint
		FROM tasks
		WHERE status = 'PENDING'
		  AND is_poison_pill = FALSE
		  AND (next_retry_at IS NULL OR next_retry_at <= now())
	`); err != nil {
		return QueueDepthSnapshot{}, fmt.Errorf("GetQueueDepth: query oldest pending age: %w", err)
	}

	for _, row := range snapshot.PendingByServicePriority {
		snapshot.TotalPending += row.PendingCount
	}
	for _, row := range snapshot.RetryQueue {
		snapshot.TotalRetry += row.RetryCount
	}
	for _, row := range snapshot.DLQDepthByCaseType {
		snapshot.TotalDLQ += row.Depth
	}

	return snapshot, nil
}

// GetCaseEventTimeline returns paginated outbox events for a case.
func GetCaseEventTimeline(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
	page int,
	size int,
) ([]EventRow, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetCaseEventTimeline: db is nil")
	}
	trimmedCaseID := strings.TrimSpace(caseID)
	if trimmedCaseID == "" {
		return nil, 0, fmt.Errorf("GetCaseEventTimeline: caseID is required")
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)::int
		FROM events_outbox
		WHERE case_id = $1::uuid
	`, trimmedCaseID); err != nil {
		return nil, 0, fmt.Errorf("GetCaseEventTimeline: count events: %w", err)
	}
	if offset >= total {
		return []EventRow{}, total, nil
	}

	type eventScanRow struct {
		ID            string         `db:"id"`
		CaseID        sql.NullString `db:"case_id"`
		TaskID        sql.NullString `db:"task_id"`
		EventType     string         `db:"event_type"`
		Payload       []byte         `db:"payload"`
		Status        string         `db:"status"`
		TargetService string         `db:"target_service"`
		CreatedAt     time.Time      `db:"created_at"`
		DeliveredAt   sql.NullTime   `db:"delivered_at"`
	}

	scanRows := make([]eventScanRow, 0)
	if err := db.SelectContext(ctx, &scanRows, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			task_id::text AS task_id,
			event_type,
			payload,
			status,
			target_service,
			created_at,
			delivered_at
		FROM events_outbox
		WHERE case_id = $1::uuid
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`, trimmedCaseID, size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetCaseEventTimeline: query events: %w", err)
	}

	rows := make([]EventRow, 0, len(scanRows))
	for _, item := range scanRows {
		event := EventRow{
			ID:            item.ID,
			EventType:     item.EventType,
			Payload:       json.RawMessage(item.Payload),
			Status:        item.Status,
			TargetService: item.TargetService,
			CreatedAt:     item.CreatedAt,
		}
		if item.CaseID.Valid {
			value := strings.TrimSpace(item.CaseID.String)
			event.CaseID = &value
		}
		if item.TaskID.Valid {
			value := strings.TrimSpace(item.TaskID.String)
			event.TaskID = &value
		}
		if item.DeliveredAt.Valid {
			tm := item.DeliveredAt.Time
			event.DeliveredAt = &tm
		}
		rows = append(rows, event)
	}

	return rows, total, nil
}

// GetEventsByTypeInRange returns paginated events by event type and time range.
func GetEventsByTypeInRange(
	ctx context.Context,
	db *sqlx.DB,
	eventType string,
	from, to time.Time,
	page int,
	size int,
) ([]EventRow, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetEventsByTypeInRange: db is nil")
	}
	trimmedType := strings.ToUpper(strings.TrimSpace(eventType))
	if trimmedType == "" {
		return nil, 0, fmt.Errorf("GetEventsByTypeInRange: eventType is required")
	}
	if to.Before(from) {
		return nil, 0, fmt.Errorf("GetEventsByTypeInRange: to is before from")
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)::int
		FROM events_outbox
		WHERE event_type = $1
		  AND created_at >= $2
		  AND created_at < $3
	`, trimmedType, from.UTC(), to.UTC()); err != nil {
		return nil, 0, fmt.Errorf("GetEventsByTypeInRange: count events: %w", err)
	}
	if offset >= total {
		return []EventRow{}, total, nil
	}

	type eventScanRow struct {
		ID            string         `db:"id"`
		CaseID        sql.NullString `db:"case_id"`
		TaskID        sql.NullString `db:"task_id"`
		EventType     string         `db:"event_type"`
		Payload       []byte         `db:"payload"`
		Status        string         `db:"status"`
		TargetService string         `db:"target_service"`
		CreatedAt     time.Time      `db:"created_at"`
		DeliveredAt   sql.NullTime   `db:"delivered_at"`
	}

	scanRows := make([]eventScanRow, 0)
	if err := db.SelectContext(ctx, &scanRows, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			task_id::text AS task_id,
			event_type,
			payload,
			status,
			target_service,
			created_at,
			delivered_at
		FROM events_outbox
		WHERE event_type = $1
		  AND created_at >= $2
		  AND created_at < $3
		ORDER BY created_at ASC, id ASC
		LIMIT $4 OFFSET $5
	`, trimmedType, from.UTC(), to.UTC(), size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetEventsByTypeInRange: query events: %w", err)
	}

	rows := make([]EventRow, 0, len(scanRows))
	for _, item := range scanRows {
		event := EventRow{
			ID:            item.ID,
			EventType:     item.EventType,
			Payload:       json.RawMessage(item.Payload),
			Status:        item.Status,
			TargetService: item.TargetService,
			CreatedAt:     item.CreatedAt,
		}
		if item.CaseID.Valid {
			value := strings.TrimSpace(item.CaseID.String)
			event.CaseID = &value
		}
		if item.TaskID.Valid {
			value := strings.TrimSpace(item.TaskID.String)
			event.TaskID = &value
		}
		if item.DeliveredAt.Valid {
			tm := item.DeliveredAt.Time
			event.DeliveredAt = &tm
		}
		rows = append(rows, event)
	}

	return rows, total, nil
}

// CountEventVolumeByTypePerHour returns hourly event volume grouped by type.
func CountEventVolumeByTypePerHour(
	ctx context.Context,
	db *sqlx.DB,
	from, to time.Time,
) ([]EventVolumeRow, error) {
	if db == nil {
		return nil, fmt.Errorf("CountEventVolumeByTypePerHour: db is nil")
	}
	if to.Before(from) {
		return nil, fmt.Errorf("CountEventVolumeByTypePerHour: to is before from")
	}

	rows := make([]EventVolumeRow, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			date_trunc('hour', created_at)::timestamptz AS bucket_hour,
			event_type,
			COUNT(*)::bigint AS volume
		FROM events_outbox
		WHERE created_at >= $1
		  AND created_at < $2
		GROUP BY 1, 2
		ORDER BY bucket_hour ASC, event_type ASC
	`, from.UTC(), to.UTC()); err != nil {
		return nil, fmt.Errorf("CountEventVolumeByTypePerHour: query volume: %w", err)
	}
	if rows == nil {
		return []EventVolumeRow{}, nil
	}
	return rows, nil
}

// GetRegressionReport returns regression metrics from pre-aggregated daily snapshots.
func GetRegressionReport(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
	from, to time.Time,
) (RegressionReport, error) {
	if db == nil {
		return RegressionReport{}, fmt.Errorf("GetRegressionReport: db is nil")
	}
	if to.Before(from) {
		return RegressionReport{}, fmt.Errorf("GetRegressionReport: to is before from")
	}
	caseType := normalizeCaseTypeCode(caseTypeCode)
	if caseType == "" {
		return RegressionReport{}, fmt.Errorf("GetRegressionReport: caseTypeCode is required")
	}

	report := RegressionReport{
		CaseTypeCode:             caseType,
		MostCommonRegressionPath: []RegressionPathRow{},
		FlaggedCases:             []RegressionCaseFlag{},
	}

	if err := db.GetContext(ctx, &report, `
		SELECT
			COALESCE(SUM(forward_transition_count), 0)::bigint AS forward_transition_count,
			COALESCE(SUM(regression_count), 0)::bigint AS regression_count,
			CASE
				WHEN COALESCE(SUM(forward_transition_count), 0) = 0 THEN 0
				ELSE (COALESCE(SUM(regression_count), 0)::double precision * 100.0)
				     / SUM(forward_transition_count)::double precision
			END AS regression_rate_percent,
			COALESCE(MAX(regression_threshold), 0)::int AS regression_threshold
		FROM regression_metrics_snapshots
		WHERE case_type_code = $1
		  AND snapshot_date >= $2::date
		  AND snapshot_date <= $3::date
	`, caseType, from.UTC(), to.UTC()); err != nil {
		return RegressionReport{}, fmt.Errorf("GetRegressionReport: query summary: %w", err)
	}
	report.CaseTypeCode = caseType

	if err := db.SelectContext(ctx, &report.MostCommonRegressionPath, `
		WITH paths AS (
			SELECT
				path_item->>'from_stage_code' AS from_stage_code,
				path_item->>'to_stage_code' AS to_stage_code,
				COALESCE((path_item->>'count')::bigint, 0)::bigint AS path_count
			FROM regression_metrics_snapshots rms
			CROSS JOIN LATERAL jsonb_array_elements(COALESCE(rms.most_common_paths, '[]'::jsonb)) AS path_item
			WHERE rms.case_type_code = $1
			  AND rms.snapshot_date >= $2::date
			  AND rms.snapshot_date <= $3::date
		)
		SELECT
			from_stage_code,
			to_stage_code,
			SUM(path_count)::bigint AS path_count
		FROM paths
		WHERE from_stage_code IS NOT NULL
		  AND from_stage_code <> ''
		  AND to_stage_code IS NOT NULL
		  AND to_stage_code <> ''
		GROUP BY from_stage_code, to_stage_code
		ORDER BY path_count DESC, from_stage_code ASC, to_stage_code ASC
		LIMIT 10
	`, caseType, from.UTC(), to.UTC()); err != nil {
		return RegressionReport{}, fmt.Errorf("GetRegressionReport: query paths: %w", err)
	}

	if err := db.SelectContext(ctx, &report.FlaggedCases, `
		WITH flagged AS (
			SELECT
				flag_item->>'case_id' AS case_id,
				COALESCE((flag_item->>'regression_count')::bigint, 0)::bigint AS regression_count
			FROM regression_metrics_snapshots rms
			CROSS JOIN LATERAL jsonb_array_elements(COALESCE(rms.flagged_cases, '[]'::jsonb)) AS flag_item
			WHERE rms.case_type_code = $1
			  AND rms.snapshot_date >= $2::date
			  AND rms.snapshot_date <= $3::date
		)
		SELECT
			case_id,
			MAX(regression_count)::bigint AS regression_count
		FROM flagged
		WHERE case_id IS NOT NULL
		  AND case_id <> ''
		GROUP BY case_id
		ORDER BY regression_count DESC, case_id ASC
		LIMIT 200
	`, caseType, from.UTC(), to.UTC()); err != nil {
		return RegressionReport{}, fmt.Errorf("GetRegressionReport: query flagged cases: %w", err)
	}

	if report.MostCommonRegressionPath == nil {
		report.MostCommonRegressionPath = []RegressionPathRow{}
	}
	if report.FlaggedCases == nil {
		report.FlaggedCases = []RegressionCaseFlag{}
	}

	return report, nil
}

// GetServiceHealthLeaderboard returns the latest hourly service health ranking.
func GetServiceHealthLeaderboard(
	ctx context.Context,
	db *sqlx.DB,
) ([]ServiceHealthRow, error) {
	if db == nil {
		return nil, fmt.Errorf("GetServiceHealthLeaderboard: db is nil")
	}

	rows := make([]ServiceHealthRow, 0)
	if err := db.SelectContext(ctx, &rows, `
		WITH latest AS (
			SELECT MAX(bucket_start) AS bucket_start
			FROM service_health_snapshots
			WHERE bucket = 'HOURLY'
		)
		SELECT
			sh.bucket_start,
			sh.assigned_service,
			sh.total_tasks,
			sh.failed_tasks,
			sh.retried_tasks,
			sh.dlq_tasks,
			sh.failure_rate_percent::double precision AS failure_rate_percent,
			sh.avg_execution_seconds,
			sh.retry_rate_percent::double precision AS retry_rate_percent,
			sh.dlq_rate_percent::double precision AS dlq_rate_percent,
			sh.dlq_contribution_rate_percent::double precision AS dlq_contribution_rate_percent
		FROM service_health_snapshots sh
		JOIN latest ON sh.bucket_start = latest.bucket_start
		WHERE sh.bucket = 'HOURLY'
		ORDER BY
			sh.failure_rate_percent DESC,
			sh.dlq_contribution_rate_percent DESC,
			sh.avg_execution_seconds DESC,
			sh.assigned_service ASC
	`); err != nil {
		return nil, fmt.Errorf("GetServiceHealthLeaderboard: query leaderboard: %w", err)
	}
	if rows == nil {
		return []ServiceHealthRow{}, nil
	}
	return rows, nil
}
