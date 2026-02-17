package model

import "time"

// ---------------------------------------------------------------------------
// Stage type constants
// ---------------------------------------------------------------------------

const (
	StageTypeEntry    = "ENTRY"
	StageTypeNormal   = "NORMAL"
	StageTypeExit     = "EXIT"
	StageTypeTerminal = "TERMINAL"
)

// ---------------------------------------------------------------------------
// StageDefinition — design-time row: one stage per case_type version
// ---------------------------------------------------------------------------

// StageDefinition describes a single stage within a versioned case type.
type StageDefinition struct {
	ID              string    `json:"id"                db:"id"`
	CaseTypeID      string    `json:"case_type_id"      db:"case_type_id"`
	CaseTypeVersion int       `json:"case_type_version" db:"case_type_version"`
	StageCode       string    `json:"stage_code"        db:"stage_code"`
	StageName       string    `json:"stage_name"        db:"stage_name"`
	Ordinal         int       `json:"ordinal"           db:"ordinal"`
	StageType       string    `json:"stage_type"        db:"stage_type"`
	Description     string    `json:"description"       db:"description"`
	IsSkippable     bool      `json:"is_skippable"      db:"is_skippable"`
	CreatedAt       time.Time `json:"created_at"        db:"created_at"`
}
