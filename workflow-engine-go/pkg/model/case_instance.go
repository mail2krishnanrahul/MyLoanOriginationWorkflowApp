package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Case status constants
// ---------------------------------------------------------------------------

const (
	CaseStatusOpen       = "OPEN"
	CaseStatusInProgress = "IN_PROGRESS"
	CaseStatusCompleted  = "COMPLETED"
	CaseStatusCancelled  = "CANCELLED"
	CaseStatusSuspended  = "SUSPENDED"
	CaseStatusRejected   = "REJECTED"
	CaseStatusCloned     = "CLONED"
	CaseStatusException  = "EXCEPTION"
)

// ---------------------------------------------------------------------------
// CaseInstance — runtime instance of a CaseType blueprint
// ---------------------------------------------------------------------------

// CaseInstance represents a single loan application (or sub-process) being
// tracked through the workflow engine.
type CaseInstance struct {
	ID                  string          `json:"id"                    db:"id"`
	TenantID            string          `json:"tenant_id"             db:"tenant_id"`
	ReferenceNumber     string          `json:"reference_number"      db:"reference_number"`
	CaseTypeID          string          `json:"case_type_id"          db:"case_type_id"`
	CaseTypeVersion     int             `json:"case_type_version"     db:"case_type_version"`
	ParentCaseID        *string         `json:"parent_case_id"        db:"parent_case_id"`
	SourceCaseID        *string         `json:"source_case_id"        db:"source_case_id"`
	CurrentStageCode    *string         `json:"current_stage_code"    db:"current_stage_code"`
	CurrentStageOrdinal int             `json:"current_stage_ordinal" db:"current_stage_ordinal"`
	Status              string          `json:"status"                db:"status"`
	Metadata            json.RawMessage `json:"metadata"              db:"metadata"`
	AssignedTo          *string         `json:"assigned_to"           db:"assigned_to"`
	ReworkCount         int             `json:"rework_count"          db:"rework_count"`
	MaxReworkAttempts   int             `json:"max_rework_attempts"   db:"max_rework_attempts"`
	RowVersion          int             `json:"row_version"           db:"row_version"`
	CreatedAt           time.Time       `json:"created_at"            db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"            db:"updated_at"`
	CompletedAt         *time.Time      `json:"completed_at"          db:"completed_at"`
	
	// Lifecycle fields
	SuspendReason     *string    `json:"suspend_reason"        db:"suspend_reason"`
	ResumeAt          *time.Time `json:"resume_at"             db:"resume_at"`
	WithdrawalReason  *string    `json:"withdrawal_reason"     db:"withdrawal_reason"`
	EmergencyClosedAt *time.Time `json:"emergency_closed_at"   db:"emergency_closed_at"`
	EmergencyReason   *string    `json:"emergency_reason"      db:"emergency_reason"`
	SupervisorID      *string    `json:"supervisor_id"         db:"supervisor_id"`
	ExceptionAt       *time.Time `json:"exception_at"          db:"exception_at"`
	ExceptionReason   *string    `json:"exception_reason"      db:"exception_reason"`
	ExceptionTaskID   *string    `json:"exception_task_id"     db:"exception_task_id"`
	ExceptionSeverity *string    `json:"exception_severity"    db:"exception_severity"`
}

// IsRegression returns true when the proposed newOrdinal is strictly less than
// the case's current stage ordinal — i.e. the case would move backward.
// This is useful for guard-rail checks before allowing a stage transition.
func (c *CaseInstance) IsRegression(newOrdinal int) bool {
	return newOrdinal < c.CurrentStageOrdinal
}

// IsTerminal returns true if the case is in a final state that should not
// accept further transitions.
func (c *CaseInstance) IsTerminal() bool {
	return c.Status == CaseStatusCompleted || c.Status == CaseStatusCancelled || c.Status == CaseStatusRejected
}
