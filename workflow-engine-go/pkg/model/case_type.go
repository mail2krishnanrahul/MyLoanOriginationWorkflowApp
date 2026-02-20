package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Status constants for CaseType lifecycle
// ---------------------------------------------------------------------------

const (
	CaseTypeStatusDraft      = "DRAFT"
	CaseTypeStatusActive     = "ACTIVE"
	CaseTypeStatusDeprecated = "DEPRECATED"
)

// ---------------------------------------------------------------------------
// Task type constants
// ---------------------------------------------------------------------------

const (
	TaskTypeSystem = "SYSTEM"
	TaskTypeUser   = "USER"
)

// ---------------------------------------------------------------------------
// CaseType — maps to the case_types table row
// ---------------------------------------------------------------------------

// CaseType is a versioned blueprint for a loan-origination workflow.
// One row = one version of a definition (e.g. HOME_LOAN v1).
type CaseType struct {
	ID           string         `json:"id"            db:"id"`
	TenantID     *string        `json:"tenant_id,omitempty" db:"tenant_id"`
	Code         string         `json:"code"          db:"code"`
	Version      int            `json:"version"       db:"version"`
	Name         string         `json:"name"          db:"name"`
	Description  string         `json:"description"   db:"description"`
	Config       CaseTypeConfig `json:"config"    db:"config"`
	Status       string         `json:"status"        db:"status"`
	CreatedAt    time.Time      `json:"created_at"    db:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"    db:"updated_at"`
	DeprecatedAt *time.Time     `json:"deprecated_at" db:"deprecated_at"`
}

// ---------------------------------------------------------------------------
// CaseTypeConfig — the JSONB shape
// ---------------------------------------------------------------------------

// CaseTypeConfig is the top-level JSON structure stored in the config column.
type CaseTypeConfig struct {
	Stages                 []StageDefinitionV2          `json:"stages"`
	SubCaseTypes           []string                     `json:"sub_case_types,omitempty"` // e.g. ["CREDIT_CHECK", "VALUATION"]
	DefaultCalendarID      string                       `json:"default_calendar_id,omitempty"`
	SLA                    *SLAHierarchyConfig          `json:"sla,omitempty"`
	ApprovalChain          []ApprovalChainTierDefinition `json:"approval_chain,omitempty"`
	FallbackSupervisorRole string                       `json:"fallback_supervisor_role,omitempty"`
	MaxReworkAttempts      int                          `json:"max_rework_attempts,omitempty"`
	DocumentTypes          []DocumentTypeDefinition     `json:"document_types,omitempty"`
	AggregationRules       []AggregationRule            `json:"aggregation_rules,omitempty"`
	PoisonPillThreshold    int                          `json:"poison_pill_threshold,omitempty"`
}

// Scan implements the sql.Scanner interface so pgx can read JSONB into this struct.
func (c *CaseTypeConfig) Scan(src interface{}) error {
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	default:
		return json.Unmarshal(src.([]byte), c)
	}
}

// Value implements the driver.Valuer interface so pgx can write this struct as JSONB.
func (c CaseTypeConfig) Value() ([]byte, error) {
	return json.Marshal(c)
}

// ---------------------------------------------------------------------------
// StageDefinitionV2 — one stage inside the config
// ---------------------------------------------------------------------------

// StageDefinitionV2 describes an ordered progress marker within a case type.
type StageDefinitionV2 struct {
	Code          string           `json:"code"`
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	SequenceOrder int              `json:"sequence_order"`
	Activities    []ActivityConfig `json:"activities"`
	SLA           *SLADefinition   `json:"sla,omitempty"`
}

// ---------------------------------------------------------------------------
// ActivityConfig — a config-defined task grouping inside a stage (JSONB)
// ---------------------------------------------------------------------------

// ActivityConfig is the JSONB-embedded activity shape within CaseTypeConfig.
// For the database-row model, see ActivityDefinition in activity_definition.go.
type ActivityConfig struct {
	Code          string             `json:"code"`
	Name          string             `json:"name"`
	Description   string             `json:"description,omitempty"`
	SequenceOrder int                `json:"sequence_order"`
	TaskDefs      []TaskDefinitionV2 `json:"task_definitions"`
	SLA           *SLADefinition     `json:"sla,omitempty"`
}

// ---------------------------------------------------------------------------
// TaskDefinitionV2 — an atomic unit of work
// ---------------------------------------------------------------------------

// TaskDefinitionV2 is the smallest executable unit in a workflow.
type TaskDefinitionV2 struct {
	Code                   string                 `json:"code"`
	Name                   string                 `json:"name"`
	Description            string                 `json:"description,omitempty"`
	Type                   string                 `json:"type"` // SYSTEM or USER
	Required               bool                   `json:"required"`
	SequenceOrder          int                    `json:"sequence_order"`
	Config                 json.RawMessage        `json:"config,omitempty"` // UI hints, endpoints, timeouts, etc.
	SLA                    *SLADefinition         `json:"sla,omitempty"`
	RequiresApproval       bool                   `json:"requires_approval,omitempty"`
	Approval               *ApprovalDefinition    `json:"approval,omitempty"`
	IsDocumentVerification bool                   `json:"is_document_verification,omitempty"`
	DocumentTypeCode       string                 `json:"document_type_code,omitempty"`
	RetryPolicy            *TaskRetryPolicy       `json:"retry_policy,omitempty"`
	FailureSeverity        TaskFailureSeverity    `json:"failure_severity,omitempty"`
	EscalateCaseOnFailure  bool                   `json:"escalate_case_on_failure,omitempty"`
	CompensatingTaskCode   string                 `json:"compensating_task_code,omitempty"`
	PoisonPillThreshold    int                    `json:"poison_pill_threshold,omitempty"`
	Inputs                 []TaskInputDefinition  `json:"inputs,omitempty"`
	Outputs                []TaskOutputDefinition `json:"outputs,omitempty"`
	InputSchema            map[string]interface{} `json:"input_schema,omitempty"`
	OutputSchema           map[string]interface{} `json:"output_schema,omitempty"`
}

// DocumentTypeDefinition configures allowed/required document categories for a case type.
type DocumentTypeDefinition struct {
	DocumentTypeCode      string   `json:"document_type_code"`
	DisplayName           string   `json:"display_name"`
	Description           string   `json:"description,omitempty"`
	AllowedExtensions     []string `json:"allowed_extensions"`
	MaxSizeMB             int      `json:"max_size_mb"`
	RequiredAtStage       string   `json:"required_at_stage,omitempty"`
	RequiredCountMin      int      `json:"required_count_min,omitempty"`
	RequiredCountMax      int      `json:"required_count_max,omitempty"`
	IsSensitive           bool     `json:"is_sensitive,omitempty"`
	RequiresVerification  bool     `json:"requires_verification,omitempty"`
	VerificationRole      string   `json:"verification_role,omitempty"`
	RetentionDays         int      `json:"retention_days,omitempty"`
	RetentionPolicy       string   `json:"retention_policy,omitempty"`
	AllowedViewers        []string `json:"allowed_viewers,omitempty"`
}

// TaskInputDefinition describes one resolved input field dependency for a task.
type TaskInputDefinition struct {
	Field      string `json:"field"`
	SourceTask string `json:"source_task,omitempty"`
	SourceField string `json:"source_field,omitempty"`
	Required   bool   `json:"required,omitempty"`
}

// TaskOutputDefinition describes one output field produced by a task.
type TaskOutputDefinition struct {
	Field string `json:"field"`
	Type  string `json:"type,omitempty"`
}

// AggregationRule maps task data into case metadata at task completion.
type AggregationRule struct {
	TargetField    string `json:"target_field"`
	SourceTask     string `json:"source_task"`
	SourceField    string `json:"source_field"`
	OnTaskComplete bool   `json:"on_task_complete,omitempty"`
}
