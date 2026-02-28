package engine

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Response DTOs — designed for JSON serialisation to frontends & operators
// All field names use camelCase JSON tags to match the TypeScript frontend.
// ---------------------------------------------------------------------------

// CaseDetail is the full picture of a case's current state.
// JSON keys match the CaseDetail TypeScript interface in frontend/src/lib/api/types.ts.
type CaseDetail struct {
	// Header
	CaseID          string          `json:"id"`
	ReferenceNumber string          `json:"referenceNumber"`
	CaseType        string          `json:"caseType"`
	CaseTypeVersion int             `json:"caseTypeVersion"`
	Status          string          `json:"status"`
	CurrentStage    *string         `json:"currentStage"`
	AssignedTo      *string         `json:"assignedTo"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	CompletedAt     *time.Time      `json:"completedAt,omitempty"`

	// Extracted from metadata for convenience
	BorrowerName *string `json:"borrowerName,omitempty"`
	Priority     *string `json:"priority,omitempty"`
	ProductType  *string `json:"productType,omitempty"`
	LoanAmount   float64 `json:"loanAmount,omitempty"`

	// SLA
	SLAStatus           string  `json:"slaStatus"`
	SLARemainingMinutes float64 `json:"slaRemainingMinutes"`

	// Progress
	PercentComplete float64 `json:"percentComplete"` // 0.0–100.0
	TotalTasks      int     `json:"tasksTotal"`
	CompletedTasks  int     `json:"tasksCompleted"`

	// Related data
	StageHistory []StageHistoryEntry `json:"stageHistory"`
	SubCases     []SubCaseSummary    `json:"subCases"`
	Activities   []ActivitySummary   `json:"activities"` // current stage only

	// Deal snapshot from case_deal_links (full deal structure for Deal 360 view)
	DealSnapshot json.RawMessage `json:"dealSnapshot,omitempty"`
}

// StageHistoryEntry is one row from case_stage_transitions.
type StageHistoryEntry struct {
	FromStage    *string   `json:"fromStage"`
	ToStage      string    `json:"toStage"`
	IsRegression bool      `json:"isRegression"`
	Reason       *string   `json:"reason,omitempty"`
	TriggeredBy  string    `json:"triggeredBy"`
	TransitionAt time.Time `json:"transitionAt"`
}

// SubCaseSummary is a lightweight view of a child case.
type SubCaseSummary struct {
	CaseID          string  `json:"caseId"`
	ReferenceNumber string  `json:"referenceNumber"`
	CaseTypeCode    string  `json:"caseType"`
	Status          string  `json:"status"`
	CurrentStage    *string `json:"currentStage"`
}

// ActivitySummary groups tasks by activity_code with status counts.
type ActivitySummary struct {
	ActivityCode string         `json:"activityCode"`
	Tasks        []TaskSummary  `json:"tasks"`
	StatusCounts map[string]int `json:"statusCounts"` // e.g. {"COMPLETED":3,"PENDING":1}
	Total        int            `json:"total"`
	Completed    int            `json:"completed"`
}

// TaskSummary is a lightweight view of a task within an activity.
type TaskSummary struct {
	TaskID             string     `json:"taskId"`
	TaskDefinitionCode string     `json:"name"`
	Status             string     `json:"status"`
	Priority           int        `json:"priorityRaw"`
	AssignedService    *string    `json:"assignedService,omitempty"`
	DueAt              *time.Time `json:"dueAt,omitempty"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
}
