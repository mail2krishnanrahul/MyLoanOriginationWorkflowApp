package sla

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// SLAReportFilters scopes SLA report dimensions.
type SLAReportFilters struct {
	CaseTypeCode       *string
	StageCode          *string
	ActivityCode       *string
	TaskDefinitionCode *string
}

// SLAComplianceReport is the main operational compliance response row.
type SLAComplianceReport struct {
	CaseTypeCode       string  `json:"case_type_code" db:"case_type_code"`
	StageCode          string  `json:"stage_code" db:"stage_code"`
	ActivityCode       string  `json:"activity_code" db:"activity_code"`
	TaskDefinitionCode string  `json:"task_definition_code" db:"task_definition_code"`
	TotalCount         int     `json:"total_count" db:"total_count"`
	CompletedCount     int     `json:"completed_count" db:"completed_count"`
	BreachedCount      int     `json:"breached_count" db:"breached_count"`
	ComplianceRate     float64 `json:"compliance_rate" db:"compliance_rate"`
	AvgElapsedMinutes  float64 `json:"avg_elapsed_minutes" db:"avg_elapsed_minutes"`
	P95ElapsedMinutes  int     `json:"p95_elapsed_minutes" db:"p95_elapsed_minutes"`
}

// GetSLAComplianceReport returns SLA compliance using sla_metrics_summary only.
func GetSLAComplianceReport(
	ctx context.Context,
	db *sqlx.DB,
	startDate time.Time,
	endDate time.Time,
	filters SLAReportFilters,
) ([]SLAComplianceReport, error) {
	if db == nil {
		return nil, fmt.Errorf("GetSLAComplianceReport: db is nil")
	}
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("GetSLAComplianceReport: endDate is before startDate")
	}

	where := []string{"metric_date >= $1", "metric_date <= $2"}
	args := []any{startDate.UTC().Format("2006-01-02"), endDate.UTC().Format("2006-01-02")}
	argPos := 3

	if filters.CaseTypeCode != nil && *filters.CaseTypeCode != "" {
		where = append(where, fmt.Sprintf("case_type_code = $%d", argPos))
		args = append(args, *filters.CaseTypeCode)
		argPos++
	}
	if filters.StageCode != nil && *filters.StageCode != "" {
		where = append(where, fmt.Sprintf("stage_code = $%d", argPos))
		args = append(args, *filters.StageCode)
		argPos++
	}
	if filters.ActivityCode != nil && *filters.ActivityCode != "" {
		where = append(where, fmt.Sprintf("activity_code = $%d", argPos))
		args = append(args, *filters.ActivityCode)
		argPos++
	}
	if filters.TaskDefinitionCode != nil && *filters.TaskDefinitionCode != "" {
		where = append(where, fmt.Sprintf("task_definition_code = $%d", argPos))
		args = append(args, *filters.TaskDefinitionCode)
		argPos++
	}

	query := fmt.Sprintf(`
		SELECT
			case_type_code,
			stage_code,
			activity_code,
			task_definition_code,
			SUM(total_count)::int AS total_count,
			SUM(completed_count)::int AS completed_count,
			SUM(breached_count)::int AS breached_count,
			CASE
				WHEN SUM(completed_count) = 0 THEN 1.0
				ELSE (SUM(completed_count) - SUM(breached_count))::float / SUM(completed_count)::float
			END AS compliance_rate,
			AVG(avg_elapsed_minutes) AS avg_elapsed_minutes,
			MAX(p95_elapsed_minutes)::int AS p95_elapsed_minutes
		FROM sla_metrics_summary
		WHERE %s
		GROUP BY case_type_code, stage_code, activity_code, task_definition_code
		ORDER BY case_type_code, stage_code, activity_code, task_definition_code
	`, strings.Join(where, " AND "))

	var out []SLAComplianceReport
	if err := db.SelectContext(ctx, &out, query, args...); err != nil {
		return nil, fmt.Errorf("GetSLAComplianceReport: %w", err)
	}
	return out, nil
}

// SLABreachTrendRow returns breaches/hour from summary data.
type SLABreachTrendRow struct {
	BucketHour     time.Time `json:"bucket_hour" db:"bucket_hour"`
	Breaches       int       `json:"breaches" db:"breaches"`
}

// GetSLABreachTrend returns breach trend over a date range from summary data.
func GetSLABreachTrend(ctx context.Context, db *sqlx.DB, startDate, endDate time.Time) ([]SLABreachTrendRow, error) {
	if db == nil {
		return nil, fmt.Errorf("GetSLABreachTrend: db is nil")
	}
	query := `
		SELECT
			(date_trunc('day', metric_date::timestamp)) AS bucket_hour,
			SUM(breached_count)::int AS breaches
		FROM sla_metrics_summary
		WHERE metric_date >= $1
		  AND metric_date <= $2
		GROUP BY 1
		ORDER BY 1 ASC
	`
	var out []SLABreachTrendRow
	if err := db.SelectContext(ctx, &out, query, startDate.UTC().Format("2006-01-02"), endDate.UTC().Format("2006-01-02")); err != nil {
		return nil, fmt.Errorf("GetSLABreachTrend: %w", err)
	}
	return out, nil
}

// TopBreachTaskRow returns task definitions most prone to breach.
type TopBreachTaskRow struct {
	TaskDefinitionCode string  `json:"task_definition_code" db:"task_definition_code"`
	BreachedCount      int     `json:"breached_count" db:"breached_count"`
	BreachRate         float64 `json:"breach_rate" db:"breach_rate"`
}

// GetTopBreachProneTaskDefinitions returns top N breach-prone task definitions.
func GetTopBreachProneTaskDefinitions(ctx context.Context, db *sqlx.DB, startDate, endDate time.Time, limit int) ([]TopBreachTaskRow, error) {
	if db == nil {
		return nil, fmt.Errorf("GetTopBreachProneTaskDefinitions: db is nil")
	}
	if limit <= 0 {
		limit = 10
	}
	query := `
		SELECT
			task_definition_code,
			SUM(breached_count)::int AS breached_count,
			CASE
				WHEN SUM(total_count) = 0 THEN 0.0
				ELSE SUM(breached_count)::float / SUM(total_count)::float
			END AS breach_rate
		FROM sla_metrics_summary
		WHERE metric_date >= $1
		  AND metric_date <= $2
		GROUP BY task_definition_code
		ORDER BY breach_rate DESC, breached_count DESC
		LIMIT $3
	`
	var out []TopBreachTaskRow
	if err := db.SelectContext(ctx, &out, query, startDate.UTC().Format("2006-01-02"), endDate.UTC().Format("2006-01-02"), limit); err != nil {
		return nil, fmt.Errorf("GetTopBreachProneTaskDefinitions: %w", err)
	}
	return out, nil
}
