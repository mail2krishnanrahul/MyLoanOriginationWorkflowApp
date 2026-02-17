package model

import "time"

// ---------------------------------------------------------------------------
// CaseStageTransition — runtime audit row
// ---------------------------------------------------------------------------

// CaseStageTransition records a single stage change for a case.
type CaseStageTransition struct {
	ID               string    `json:"id"                 db:"id"`
	CaseID           string    `json:"case_id"            db:"case_id"`
	FromStageCode    *string   `json:"from_stage_code"    db:"from_stage_code"`
	FromStageOrdinal *int      `json:"from_stage_ordinal" db:"from_stage_ordinal"`
	ToStageCode      string    `json:"to_stage_code"      db:"to_stage_code"`
	ToStageOrdinal   int       `json:"to_stage_ordinal"   db:"to_stage_ordinal"`
	IsRegression     bool      `json:"is_regression"      db:"is_regression"`
	RegressionReason *string   `json:"regression_reason"  db:"regression_reason"`
	TransitionedAt   time.Time `json:"transitioned_at"    db:"transitioned_at"`
	TriggeredBy      string    `json:"triggered_by"       db:"triggered_by"`
}

// TransitionInput is the parameter struct for RecordStageTransition.
type TransitionInput struct {
	CaseID           string
	ToStageCode      string
	ToStageOrdinal   int
	RegressionReason *string // required when transition is a regression
	TriggeredBy      string
}
