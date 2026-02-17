package engine

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Response DTOs — designed for JSON serialisation to frontends & operators
// ---------------------------------------------------------------------------

// CaseDetail is the full picture of a case's current state.
type CaseDetail struct {
	// Header
	CaseID          string          `json:"case_id"`
	ReferenceNumber string          `json:"reference_number"`
	CaseTypeCode    string          `json:"case_type_code"`
	CaseTypeVersion int             `json:"case_type_version"`
	Status          string          `json:"status"`
	CurrentStage    *string         `json:"current_stage"`
	AssignedTo      *string         `json:"assigned_to"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`

	// Progress
	PercentComplete float64 `json:"percent_complete"` // 0.0–100.0
	TotalTasks      int     `json:"total_tasks"`
	CompletedTasks  int     `json:"completed_tasks"`

	// Related data
	StageHistory []StageHistoryEntry `json:"stage_history"`
	SubCases     []SubCaseSummary    `json:"sub_cases"`
	Activities   []ActivitySummary   `json:"activities"` // current stage only
}

// StageHistoryEntry is one row from case_stage_transitions.
type StageHistoryEntry struct {
	FromStage    *string   `json:"from_stage"`
	ToStage      string    `json:"to_stage"`
	IsRegression bool      `json:"is_regression"`
	Reason       *string   `json:"reason,omitempty"`
	TriggeredBy  string    `json:"triggered_by"`
	TransitionAt time.Time `json:"transition_at"`
}

// SubCaseSummary is a lightweight view of a child case.
type SubCaseSummary struct {
	CaseID          string  `json:"case_id"`
	ReferenceNumber string  `json:"reference_number"`
	CaseTypeCode    string  `json:"case_type_code"`
	Status          string  `json:"status"`
	CurrentStage    *string `json:"current_stage"`
}

// ActivitySummary groups tasks by activity_code with status counts.
type ActivitySummary struct {
	ActivityCode string         `json:"activity_code"`
	Tasks        []TaskSummary  `json:"tasks"`
	StatusCounts map[string]int `json:"status_counts"` // e.g. {"COMPLETED":3,"PENDING":1}
	Total        int            `json:"total"`
	Completed    int            `json:"completed"`
}

// TaskSummary is a lightweight view of a task within an activity.
type TaskSummary struct {
	TaskID             string     `json:"task_id"`
	TaskDefinitionCode string     `json:"task_definition_code"`
	Status             string     `json:"status"`
	Priority           int        `json:"priority"`
	AssignedService    *string    `json:"assigned_service,omitempty"`
	DueAt              *time.Time `json:"due_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}
