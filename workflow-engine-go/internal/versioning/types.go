package versioning

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"workflow-engine/pkg/model"
)

// CaseTypeStatus is the lifecycle state for a case_type version.
type CaseTypeStatus string

const (
	CaseTypeStatusDraft      CaseTypeStatus = "DRAFT"
	CaseTypeStatusActive     CaseTypeStatus = "ACTIVE"
	CaseTypeStatusDeprecated CaseTypeStatus = "DEPRECATED"
)

// Reuse canonical config/task/stage domain definitions from pkg/model.
type CaseTypeConfig = model.CaseTypeConfig
type StageDefinition = model.StageDefinitionV2
type TaskDefinition = model.TaskDefinitionV2
type RetryPolicy = model.TaskRetryPolicy

// ValidationViolation describes one config validation problem.
type ValidationViolation struct {
	FieldPath string `json:"field_path"`
	Message   string `json:"message"`
}

// ValidationResult accumulates all validation violations (no short-circuiting).
type ValidationResult struct {
	Violations []ValidationViolation `json:"violations"`
}

// Add appends a single violation.
func (r *ValidationResult) Add(fieldPath string, message string) {
	if r == nil {
		return
	}
	r.Violations = append(r.Violations, ValidationViolation{
		FieldPath: strings.TrimSpace(fieldPath),
		Message:   strings.TrimSpace(message),
	})
}

// HasViolations returns true when at least one violation is present.
func (r *ValidationResult) HasViolations() bool {
	return r != nil && len(r.Violations) > 0
}

// Error satisfies the error interface.
func (r *ValidationResult) Error() string {
	if r == nil || len(r.Violations) == 0 {
		return "validation failed"
	}
	parts := make([]string, 0, len(r.Violations))
	for _, v := range r.Violations {
		if strings.TrimSpace(v.FieldPath) == "" {
			parts = append(parts, v.Message)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", v.FieldPath, v.Message))
	}
	return fmt.Sprintf("validation failed: %s", strings.Join(parts, "; "))
}

// CaseTypeVersion is the persisted case_type row with parsed config.
type CaseTypeVersion struct {
	ID           string         `json:"id" db:"id"`
	TenantID     *string        `json:"tenant_id,omitempty" db:"tenant_id"`
	Code         string         `json:"code" db:"code"`
	Version      int            `json:"version" db:"version"`
	Name         string         `json:"name" db:"name"`
	Description  *string        `json:"description,omitempty" db:"description"`
	Config       CaseTypeConfig `json:"config"`
	Status       CaseTypeStatus `json:"status" db:"status"`
	CreatedAt    time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at" db:"updated_at"`
	ActivatedAt  *time.Time     `json:"activated_at,omitempty" db:"activated_at"`
	ActivatedBy  *string        `json:"activated_by,omitempty" db:"activated_by"`
	DeprecatedAt *time.Time     `json:"deprecated_at,omitempty" db:"deprecated_at"`
	DeprecatedBy *string        `json:"deprecated_by,omitempty" db:"deprecated_by"`
}

// StageReorder captures ordinal changes.
type StageReorder struct {
	StageCode   string `json:"stage_code"`
	FromOrdinal int    `json:"from_ordinal"`
	ToOrdinal   int    `json:"to_ordinal"`
}

// ActivityDelta captures stage activity additions/removals.
type ActivityDelta struct {
	StageCode    string `json:"stage_code"`
	ActivityCode string `json:"activity_code"`
}

// TaskDefinitionSnapshot captures where a task lives and its full shape.
type TaskDefinitionSnapshot struct {
	TaskDefinitionCode string         `json:"task_definition_code"`
	StageCode          string         `json:"stage_code"`
	ActivityCode       string         `json:"activity_code"`
	Definition         TaskDefinition `json:"definition"`
}

// TaskDefinitionChange records old/new task definitions.
type TaskDefinitionChange struct {
	TaskDefinitionCode string                 `json:"task_definition_code"`
	Old                TaskDefinitionSnapshot `json:"old"`
	New                TaskDefinitionSnapshot `json:"new"`
}

// RetryPolicyChange captures retry policy drift by task definition.
type RetryPolicyChange struct {
	TaskDefinitionCode string       `json:"task_definition_code"`
	Old                *RetryPolicy `json:"old,omitempty"`
	New                *RetryPolicy `json:"new,omitempty"`
}

// MetadataChange captures top-level metadata updates.
type MetadataChange struct {
	Field string      `json:"field"`
	Old   interface{} `json:"old,omitempty"`
	New   interface{} `json:"new,omitempty"`
}

// CaseTypeVersionDiff is the persisted diff payload returned by governance APIs.
type CaseTypeVersionDiff struct {
	DiffID                   string                 `json:"diff_id,omitempty" db:"diff_id"`
	CaseTypeCode             string                 `json:"case_type_code" db:"case_type_code"`
	FromVersionID            string                 `json:"from_version_id" db:"from_case_type_id"`
	ToVersionID              string                 `json:"to_version_id" db:"to_case_type_id"`
	FromVersion              int                    `json:"from_version" db:"from_version"`
	ToVersion                int                    `json:"to_version" db:"to_version"`
	StagesAdded              []StageDefinition      `json:"stages_added,omitempty"`
	StagesRemoved            []StageDefinition      `json:"stages_removed,omitempty"`
	StagesReordered          []StageReorder         `json:"stages_reordered,omitempty"`
	ActivitiesAdded          []ActivityDelta        `json:"activities_added,omitempty"`
	ActivitiesRemoved        []ActivityDelta        `json:"activities_removed,omitempty"`
	TaskDefinitionsAdded     []TaskDefinitionSnapshot `json:"task_definitions_added,omitempty"`
	TaskDefinitionsRemoved   []TaskDefinitionSnapshot `json:"task_definitions_removed,omitempty"`
	TaskDefinitionsModified  []TaskDefinitionChange `json:"task_definitions_modified,omitempty"`
	RetryPolicyChanges       []RetryPolicyChange    `json:"retry_policy_changes,omitempty"`
	MetadataChanges          []MetadataChange       `json:"metadata_changes,omitempty"`
	ComputedBy               string                 `json:"computed_by,omitempty" db:"computed_by"`
	ComputedAt               time.Time              `json:"computed_at" db:"computed_at"`
}

// Empty returns true when no diff dimensions changed.
func (d CaseTypeVersionDiff) Empty() bool {
	return len(d.StagesAdded) == 0 &&
		len(d.StagesRemoved) == 0 &&
		len(d.StagesReordered) == 0 &&
		len(d.ActivitiesAdded) == 0 &&
		len(d.ActivitiesRemoved) == 0 &&
		len(d.TaskDefinitionsAdded) == 0 &&
		len(d.TaskDefinitionsRemoved) == 0 &&
		len(d.TaskDefinitionsModified) == 0 &&
		len(d.RetryPolicyChanges) == 0 &&
		len(d.MetadataChanges) == 0
}

// MarshalPayload marshals the diff into JSON for persistence.
func (d CaseTypeVersionDiff) MarshalPayload() (json.RawMessage, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// CaseTypeAuditAction identifies an audited case_type mutation.
type CaseTypeAuditAction string

const (
	CaseTypeAuditActionCreated          CaseTypeAuditAction = "CREATED"
	CaseTypeAuditActionConfigUpdated    CaseTypeAuditAction = "CONFIG_UPDATED"
	CaseTypeAuditActionActivated        CaseTypeAuditAction = "ACTIVATED"
	CaseTypeAuditActionDeprecated       CaseTypeAuditAction = "DEPRECATED"
	CaseTypeAuditActionValidationFailed CaseTypeAuditAction = "VALIDATION_FAILED"
	CaseTypeAuditActionCaseMigrated     CaseTypeAuditAction = "CASE_MIGRATED"
)

// CaseTypeAuditEntry is one append-only audit record.
type CaseTypeAuditEntry struct {
	AuditID        string              `json:"audit_id" db:"audit_id"`
	CaseTypeID     string              `json:"case_type_id" db:"case_type_id"`
	Action         CaseTypeAuditAction `json:"action" db:"action"`
	Actor          string              `json:"actor" db:"actor"`
	ChangedFields  json.RawMessage     `json:"changed_fields,omitempty" db:"changed_fields"`
	PreviousValue  json.RawMessage     `json:"previous_value,omitempty" db:"previous_value"`
	NewValue       json.RawMessage     `json:"new_value,omitempty" db:"new_value"`
	OccurredAt     time.Time           `json:"occurred_at" db:"occurred_at"`
}

// ImmutableCaseTypeError is returned when attempting to mutate non-DRAFT versions.
type ImmutableCaseTypeError struct {
	CaseTypeID string
	Status     CaseTypeStatus
}

func (e *ImmutableCaseTypeError) Error() string {
	if e == nil {
		return "case type is immutable"
	}
	return fmt.Sprintf("case type %s is immutable in status %s", e.CaseTypeID, e.Status)
}

// Is enables errors.Is(err, &ImmutableCaseTypeError{}).
func (e *ImmutableCaseTypeError) Is(target error) bool {
	_, ok := target.(*ImmutableCaseTypeError)
	return ok
}

// StageCompatibilityError is returned when migration target version is incompatible.
type StageCompatibilityError struct {
	CaseID             string
	CurrentStageCode   string
	CurrentStageOrdinal int
	TargetStageOrdinal int
	Reason             string
}

func (e *StageCompatibilityError) Error() string {
	if e == nil {
		return "stage compatibility error"
	}
	if strings.TrimSpace(e.Reason) != "" {
		return fmt.Sprintf("stage compatibility error: %s", e.Reason)
	}
	return fmt.Sprintf(
		"stage compatibility error for case %s: stage=%s current_ordinal=%d target_ordinal=%d",
		e.CaseID,
		e.CurrentStageCode,
		e.CurrentStageOrdinal,
		e.TargetStageOrdinal,
	)
}

var (
	ErrNoActiveVersion           = errors.New("no active case_type version")
	ErrDiffNotFound              = errors.New("case_type version diff not found")
	ErrCannotDeprecateSoleActive = errors.New("cannot deprecate sole active case_type version without replacement")
	ErrCaseAlreadyOnActiveVersion = errors.New("case already pinned to active case_type version")
	ErrCaseTerminalForMigration  = errors.New("terminal case cannot be migrated")
)
