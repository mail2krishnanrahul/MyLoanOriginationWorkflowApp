package reporting

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MetricBucket controls throughput aggregation granularity.
type MetricBucket string

const (
	MetricBucketHourly MetricBucket = "HOURLY"
	MetricBucketDaily  MetricBucket = "DAILY"
)

// Valid returns true when bucket has a supported value.
func (b MetricBucket) Valid() bool {
	switch b {
	case MetricBucketHourly, MetricBucketDaily:
		return true
	default:
		return false
	}
}

// ThroughputRow is a pre-aggregated throughput snapshot row.
type ThroughputRow struct {
	BucketStart    time.Time `json:"bucket_start" db:"bucket_start"`
	CreatedCount   int64     `json:"created_count" db:"created_count"`
	CompletedCount int64     `json:"completed_count" db:"completed_count"`
	CancelledCount int64     `json:"cancelled_count" db:"cancelled_count"`
	InFlightCount  int64     `json:"in_flight_count" db:"inflight_count"`
}

// StageFunnelRow is one stage-level funnel metric row.
type StageFunnelRow struct {
	StageCode               string   `json:"stage_code" db:"stage_code"`
	StageOrdinal            int      `json:"stage_ordinal" db:"stage_ordinal"`
	InFlightCount           int64    `json:"in_flight_count" db:"in_flight_count"`
	AvgForwardDwellSeconds  float64  `json:"avg_forward_dwell_seconds" db:"avg_forward_dwell_seconds"`
	P95ForwardDwellSeconds  float64  `json:"p95_forward_dwell_seconds" db:"p95_forward_dwell_seconds"`
	ForwardTransitionCount  int64    `json:"forward_transition_count" db:"forward_transition_count"`
	RegressionCount         int64    `json:"regression_count" db:"regression_count"`
	RegressionRatePercent   float64  `json:"regression_rate_percent" db:"regression_rate_percent"`
	SLAThresholdSeconds     *float64 `json:"sla_threshold_seconds,omitempty" db:"sla_threshold_seconds"`
	AbnormalDwellTime       bool     `json:"abnormal_dwell_time" db:"is_abnormal_dwell"`
}

// TaskMetricsSummary returns aggregate task execution metrics.
type TaskMetricsSummary struct {
	TaskDefinitionCode  string  `json:"task_definition_code"`
	AssignedService     string  `json:"assigned_service,omitempty"`
	TotalTasks          int64   `json:"total_tasks" db:"total_tasks"`
	CompletedTasks      int64   `json:"completed_tasks" db:"completed_tasks"`
	FailedTasks         int64   `json:"failed_tasks" db:"failed_tasks"`
	RetriedTasks        int64   `json:"retried_tasks" db:"retried_tasks"`
	DLQTasks            int64   `json:"dlq_tasks" db:"dlq_tasks"`
	AvgExecutionSeconds float64 `json:"avg_execution_seconds" db:"avg_execution_seconds"`
	P50ExecutionSeconds float64 `json:"p50_execution_seconds" db:"p50_execution_seconds"`
	P95ExecutionSeconds float64 `json:"p95_execution_seconds" db:"p95_execution_seconds"`
	P99ExecutionSeconds float64 `json:"p99_execution_seconds" db:"p99_execution_seconds"`
	RetryRatePercent    float64 `json:"retry_rate_percent" db:"retry_rate_percent"`
	FailureRatePercent  float64 `json:"failure_rate_percent" db:"failure_rate_percent"`
	DLQRatePercent      float64 `json:"dlq_rate_percent" db:"dlq_rate_percent"`
}

// SLAComplianceDailyRow is one day in the SLA compliance trend.
type SLAComplianceDailyRow struct {
	MetricDay             time.Time `json:"metric_day" db:"metric_day"`
	CompletedCases        int64     `json:"completed_cases" db:"completed_cases"`
	CompliantCases        int64     `json:"compliant_cases" db:"compliant_cases"`
	BreachedCases         int64     `json:"breached_cases" db:"breached_cases"`
	ComplianceRatePercent float64   `json:"compliance_rate_percent"`
}

// SLAAtRiskCase represents an in-flight case close to SLA deadline.
type SLAAtRiskCase struct {
	CaseID           string     `json:"case_id" db:"case_id"`
	ReferenceNumber  string     `json:"reference_number" db:"reference_number"`
	Status           string     `json:"status" db:"status"`
	CurrentStageCode *string    `json:"current_stage_code,omitempty" db:"current_stage_code"`
	SLADeadline      time.Time  `json:"sla_deadline" db:"sla_deadline"`
	HoursToDeadline  float64    `json:"hours_to_deadline" db:"hours_to_deadline"`
}

// SLABreachedCase represents a case that breached SLA.
type SLABreachedCase struct {
	CaseID            string     `json:"case_id" db:"case_id"`
	ReferenceNumber   string     `json:"reference_number" db:"reference_number"`
	Status            string     `json:"status" db:"status"`
	CurrentStageCode  *string    `json:"current_stage_code,omitempty" db:"current_stage_code"`
	SLADeadline       time.Time  `json:"sla_deadline" db:"sla_deadline"`
	CompletedAt       *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	BreachHours       float64    `json:"breach_hours" db:"breach_hours"`
}

// SLAComplianceReport is the full case-level SLA reporting payload.
type SLAComplianceReport struct {
	CaseTypeCode            string                  `json:"case_type_code"`
	Daily                   []SLAComplianceDailyRow `json:"daily"`
	AtRiskCases             []SLAAtRiskCase         `json:"at_risk_cases"`
	BreachedCases           []SLABreachedCase       `json:"breached_cases"`
	TotalCompletedCases     int64                   `json:"total_completed_cases"`
	TotalCompliantCases     int64                   `json:"total_compliant_cases"`
	TotalBreachedCases      int64                   `json:"total_breached_cases"`
	ComplianceRatePercent   float64                 `json:"compliance_rate_percent"`
}

// QueueDepthPendingRow returns pending depth grouped by service and priority.
type QueueDepthPendingRow struct {
	AssignedService  string `json:"assigned_service" db:"assigned_service"`
	Priority         int    `json:"priority" db:"priority"`
	PendingCount     int64  `json:"pending_count" db:"pending_count"`
	OldestAgeSeconds int64  `json:"oldest_age_seconds" db:"oldest_age_seconds"`
}

// QueueDepthRetryRow returns retry queue depth grouped by next_attempt_at.
type QueueDepthRetryRow struct {
	AssignedService string     `json:"assigned_service" db:"assigned_service"`
	NextAttemptAt   *time.Time `json:"next_attempt_at,omitempty" db:"next_attempt_at"`
	RetryCount      int64      `json:"retry_count" db:"retry_count"`
}

// QueueDepthDLQRow returns DLQ depth by case_type.
type QueueDepthDLQRow struct {
	CaseTypeCode string `json:"case_type_code" db:"case_type_code"`
	Depth        int64  `json:"depth" db:"depth"`
}

// QueueDepthSnapshot is the real-time queue depth response.
type QueueDepthSnapshot struct {
	CapturedAt               time.Time             `json:"captured_at"`
	PendingByServicePriority []QueueDepthPendingRow `json:"pending_by_service_priority"`
	RetryQueue               []QueueDepthRetryRow   `json:"retry_queue"`
	DLQDepthByCaseType       []QueueDepthDLQRow     `json:"dlq_depth_by_case_type"`
	TotalPending             int64                  `json:"total_pending"`
	TotalRetry               int64                  `json:"total_retry"`
	TotalDLQ                 int64                  `json:"total_dlq"`
	OldestPendingAgeSeconds  int64                  `json:"oldest_pending_age_seconds"`
}

// EventRow is one row from events_outbox timeline APIs.
type EventRow struct {
	ID            string          `json:"id" db:"id"`
	CaseID        *string         `json:"case_id,omitempty" db:"case_id"`
	TaskID        *string         `json:"task_id,omitempty" db:"task_id"`
	EventType     string          `json:"event_type" db:"event_type"`
	Payload       json.RawMessage `json:"payload" db:"payload"`
	Status        string          `json:"status" db:"status"`
	TargetService string          `json:"target_service" db:"target_service"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	DeliveredAt   *time.Time      `json:"delivered_at,omitempty" db:"delivered_at"`
}

// EventVolumeRow is hourly event volume for anomaly detection.
type EventVolumeRow struct {
	BucketHour time.Time `json:"bucket_hour" db:"bucket_hour"`
	EventType  string    `json:"event_type" db:"event_type"`
	Volume     int64     `json:"volume" db:"volume"`
}

// RegressionPathRow represents one from->to regression path frequency.
type RegressionPathRow struct {
	FromStageCode string `json:"from_stage_code" db:"from_stage_code"`
	ToStageCode   string `json:"to_stage_code" db:"to_stage_code"`
	Count         int64  `json:"count" db:"path_count"`
}

// RegressionCaseFlag is a case flagged for excess regressions.
type RegressionCaseFlag struct {
	CaseID          string `json:"case_id" db:"case_id"`
	RegressionCount int64  `json:"regression_count" db:"regression_count"`
}

// RegressionReport is the aggregated regression/rework response.
type RegressionReport struct {
	CaseTypeCode             string               `json:"case_type_code"`
	ForwardTransitionCount   int64                `json:"forward_transition_count" db:"forward_transition_count"`
	RegressionCount          int64                `json:"regression_count" db:"regression_count"`
	RegressionRatePercent    float64              `json:"regression_rate_percent" db:"regression_rate_percent"`
	RegressionThreshold      int                  `json:"regression_threshold" db:"regression_threshold"`
	MostCommonRegressionPath []RegressionPathRow  `json:"most_common_regression_paths"`
	FlaggedCases             []RegressionCaseFlag `json:"flagged_cases"`
}

// ServiceHealthRow represents one ranked service row.
type ServiceHealthRow struct {
	BucketStart                 time.Time `json:"bucket_start" db:"bucket_start"`
	AssignedService             string    `json:"assigned_service" db:"assigned_service"`
	TotalTasks                  int64     `json:"total_tasks" db:"total_tasks"`
	FailedTasks                 int64     `json:"failed_tasks" db:"failed_tasks"`
	RetriedTasks                int64     `json:"retried_tasks" db:"retried_tasks"`
	DLQTasks                    int64     `json:"dlq_tasks" db:"dlq_tasks"`
	FailureRatePercent          float64   `json:"failure_rate_percent" db:"failure_rate_percent"`
	AvgExecutionSeconds         float64   `json:"avg_execution_seconds" db:"avg_execution_seconds"`
	RetryRatePercent            float64   `json:"retry_rate_percent" db:"retry_rate_percent"`
	DLQRatePercent              float64   `json:"dlq_rate_percent" db:"dlq_rate_percent"`
	DLQContributionRatePercent  float64   `json:"dlq_contribution_rate_percent" db:"dlq_contribution_rate_percent"`
}

const (
	defaultEventPageSize = 50
	maxEventPageSize     = 200
)

func normalizePagination(page int, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = defaultEventPageSize
	}
	if size > maxEventPageSize {
		size = maxEventPageSize
	}
	return page, size
}

func normalizeCaseTypeCode(caseTypeCode string) string {
	return strings.ToUpper(strings.TrimSpace(caseTypeCode))
}

func normalizeBucket(bucket MetricBucket) (MetricBucket, error) {
	value := MetricBucket(strings.ToUpper(strings.TrimSpace(string(bucket))))
	if !value.Valid() {
		return "", fmt.Errorf("unsupported metric bucket %q", bucket)
	}
	return value, nil
}
