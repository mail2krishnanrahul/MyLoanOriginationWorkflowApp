package model

import "time"

// SLAEntityType identifies which runtime entity the SLA row applies to.
type SLAEntityType string

const (
	SLAEntityTypeCase     SLAEntityType = "CASE"
	SLAEntityTypeStage    SLAEntityType = "STAGE"
	SLAEntityTypeActivity SLAEntityType = "ACTIVITY"
	SLAEntityTypeTask     SLAEntityType = "TASK"
)

// SLABreachAction defines what the system should do once a breach is detected.
type SLABreachAction string

const (
	SLABreachActionEscalateToSupervisor SLABreachAction = "ESCALATE_TO_SUPERVISOR"
	SLABreachActionAutoReassign         SLABreachAction = "AUTO_REASSIGN"
	SLABreachActionCreateExceptionCase  SLABreachAction = "CREATE_EXCEPTION_CASE"
	SLABreachActionNotifyOnly           SLABreachAction = "NOTIFY_ONLY"
)

// SLABreachSeverity quantifies how far past due an entity is.
type SLABreachSeverity string

const (
	SLABreachSeverityMinor    SLABreachSeverity = "MINOR"
	SLABreachSeverityModerate SLABreachSeverity = "MODERATE"
	SLABreachSeverityMajor    SLABreachSeverity = "MAJOR"
	SLABreachSeverityCritical SLABreachSeverity = "CRITICAL"
)

// SLAOperation defines allowed lifecycle operations on SLA state.
type SLAOperation string

const (
	SLAOperationPause   SLAOperation = "PAUSE"
	SLAOperationResume  SLAOperation = "RESUME"
	SLAOperationReset   SLAOperation = "RESET"
	SLAOperationExtend  SLAOperation = "EXTEND"
	SLAOperationBreach  SLAOperation = "BREACH"
	SLAOperationWarning SLAOperation = "WARNING"
)

// SLAState reflects the current lifecycle position of the SLA clock.
type SLAState string

const (
	SLAStateActive   SLAState = "ACTIVE"
	SLAStatePaused   SLAState = "PAUSED"
	SLAStateBreached SLAState = "BREACHED"
)

// SLADefinition is an immutable SLA snapshot attached at creation time.
type SLADefinition struct {
	DurationHours         float64         `json:"duration_hours"`
	WarningThresholdPct   float64         `json:"warning_threshold_pct"`
	CriticalThresholdPct  float64         `json:"critical_threshold_pct"`
	BreachAction          SLABreachAction `json:"breach_action"`
	CalendarID            string          `json:"calendar_id,omitempty"`
}

// SLAHierarchyConfig contains optional SLA definitions by granularity.
type SLAHierarchyConfig struct {
	Case     *SLADefinition            `json:"case,omitempty"`
	Stages   map[string]*SLADefinition `json:"stages,omitempty"`
	Activities map[string]*SLADefinition `json:"activities,omitempty"`
	Tasks    map[string]*SLADefinition `json:"tasks,omitempty"`
}

// BusinessCalendarRow maps to business_calendars.
type BusinessCalendarRow struct {
	ID                  string    `json:"id" db:"id"`
	TenantID            string    `json:"tenant_id" db:"tenant_id"`
	Name                string    `json:"name" db:"name"`
	Timezone            string    `json:"timezone" db:"timezone"`
	StartTime           string    `json:"start_time" db:"start_time"`
	EndTime             string    `json:"end_time" db:"end_time"`
	WorkingDaysBitfield int       `json:"working_days_bitfield" db:"working_days_bitfield"`
	IsDefault           bool      `json:"is_default" db:"is_default"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time `json:"updated_at" db:"updated_at"`
}

// HolidayCalendarRow maps to holiday_calendars.
type HolidayCalendarRow struct {
	ID          string    `json:"id" db:"id"`
	CalendarID  string    `json:"calendar_id" db:"calendar_id"`
	HolidayDate time.Time `json:"date" db:"holiday_date"`
	Name        string    `json:"name" db:"name"`
	IsRecurring bool      `json:"is_recurring" db:"is_recurring"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// SLAPauseLog maps to sla_pause_log.
type SLAPauseLog struct {
	ID                    string        `json:"id" db:"id"`
	EntityType            SLAEntityType `json:"entity_type" db:"entity_type"`
	EntityID              string        `json:"entity_id" db:"entity_id"`
	PausedAt              time.Time     `json:"paused_at" db:"paused_at"`
	ResumedAt             *time.Time    `json:"resumed_at,omitempty" db:"resumed_at"`
	PauseReason           string        `json:"pause_reason" db:"pause_reason"`
	ElapsedBeforePauseMS  int64         `json:"elapsed_before_pause_ms" db:"elapsed_before_pause_ms"`
	Action                string        `json:"action" db:"action"`
	CreatedAt             time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at" db:"updated_at"`
}

// SLABreachLog maps to sla_breach_log.
type SLABreachLog struct {
	ID                 string            `json:"id" db:"id"`
	EntityType         SLAEntityType     `json:"entity_type" db:"entity_type"`
	EntityID           string            `json:"entity_id" db:"entity_id"`
	BreachDetectedAt   time.Time         `json:"breach_detected_at" db:"breach_detected_at"`
	OriginalDueAt      *time.Time        `json:"original_due_at,omitempty" db:"original_due_at"`
	AssigneeAtBreach   *string           `json:"assignee_at_breach,omitempty" db:"assignee_at_breach"`
	ElapsedTimeMinutes int               `json:"elapsed_time_minutes" db:"elapsed_time_minutes"`
	BreachSeverity     SLABreachSeverity `json:"breach_severity" db:"breach_severity"`
	BreachActionTaken  SLABreachAction   `json:"breach_action_taken" db:"breach_action_taken"`
	SLACycle           int               `json:"sla_cycle" db:"sla_cycle"`
	CreatedAt          time.Time         `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at" db:"updated_at"`
}

// SLAResetLog maps to sla_reset_log.
type SLAResetLog struct {
	ID               string        `json:"id" db:"id"`
	EntityType       SLAEntityType `json:"entity_type" db:"entity_type"`
	EntityID         string        `json:"entity_id" db:"entity_id"`
	ResetAt          time.Time     `json:"reset_at" db:"reset_at"`
	PreviousDueAt    *time.Time    `json:"previous_due_at,omitempty" db:"previous_due_at"`
	NewDueAt         *time.Time    `json:"new_due_at,omitempty" db:"new_due_at"`
	NewDurationHours *float64      `json:"new_duration_hours,omitempty" db:"new_duration_hours"`
	Reason           string        `json:"reason" db:"reason"`
	ApprovedBy       string        `json:"approved_by" db:"approved_by"`
	CreatedAt        time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at" db:"updated_at"`
}

// SLAExtensionLog maps to sla_extension_log.
type SLAExtensionLog struct {
	ID                     string        `json:"id" db:"id"`
	EntityType             SLAEntityType `json:"entity_type" db:"entity_type"`
	EntityID               string        `json:"entity_id" db:"entity_id"`
	ExtendedAt             time.Time     `json:"extended_at" db:"extended_at"`
	PreviousDueAt          *time.Time    `json:"previous_due_at,omitempty" db:"previous_due_at"`
	NewDueAt               *time.Time    `json:"new_due_at,omitempty" db:"new_due_at"`
	ExtensionDurationHours float64       `json:"extension_duration_hours" db:"extension_duration_hours"`
	Reason                 string        `json:"reason" db:"reason"`
	ApprovedBy             string        `json:"approved_by" db:"approved_by"`
	CreatedAt              time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time     `json:"updated_at" db:"updated_at"`
}

// SLAMetricsSummary maps to sla_metrics_summary.
type SLAMetricsSummary struct {
	MetricDate          time.Time `json:"metric_date" db:"metric_date"`
	CaseTypeCode        string    `json:"case_type_code" db:"case_type_code"`
	StageCode           string    `json:"stage_code" db:"stage_code"`
	ActivityCode        string    `json:"activity_code" db:"activity_code"`
	TaskDefinitionCode  string    `json:"task_definition_code" db:"task_definition_code"`
	TotalCount          int64     `json:"total_count" db:"total_count"`
	CompletedCount      int64     `json:"completed_count" db:"completed_count"`
	BreachedCount       int64     `json:"breached_count" db:"breached_count"`
	AvgElapsedMinutes   float64   `json:"avg_elapsed_minutes" db:"avg_elapsed_minutes"`
	P50ElapsedMinutes   int       `json:"p50_elapsed_minutes" db:"p50_elapsed_minutes"`
	P95ElapsedMinutes   int       `json:"p95_elapsed_minutes" db:"p95_elapsed_minutes"`
	P99ElapsedMinutes   int       `json:"p99_elapsed_minutes" db:"p99_elapsed_minutes"`
	TotalPauseMinutes   int64     `json:"total_pause_minutes" db:"total_pause_minutes"`
	CreatedAt           time.Time `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time `json:"updated_at" db:"updated_at"`
}
