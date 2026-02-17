package model

import "time"

// ---------------------------------------------------------------------------
// CompletionPolicy governs when an activity is considered complete
// ---------------------------------------------------------------------------

type CompletionPolicy string

const (
	// CompletionAllTasks — activity is done when ALL its tasks are done.
	CompletionAllTasks CompletionPolicy = "ALL_TASKS"
	// CompletionAnyTask — activity is done when ANY one task completes.
	CompletionAnyTask CompletionPolicy = "ANY_TASK"
	// CompletionManual — activity requires explicit manual sign-off.
	CompletionManual CompletionPolicy = "MANUAL"
)

// ---------------------------------------------------------------------------\n// Task completion status used by IsActivityComplete — use TaskStatusDone\n// from task.go (value: \"COMPLETED\").\n// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ActivityDefinition — design-time row: one activity per stage per version
// ---------------------------------------------------------------------------

// ActivityDefinition describes a task grouping within a stage.
type ActivityDefinition struct {
	ID               string           `json:"id"                db:"id"`
	CaseTypeID       string           `json:"case_type_id"      db:"case_type_id"`
	CaseTypeVersion  int              `json:"case_type_version" db:"case_type_version"`
	StageCode        string           `json:"stage_code"        db:"stage_code"`
	ActivityCode     string           `json:"activity_code"     db:"activity_code"`
	ActivityName     string           `json:"activity_name"     db:"activity_name"`
	Description      string           `json:"description"       db:"description"`
	Ordinal          int              `json:"ordinal"           db:"ordinal"`
	IsOptional       bool             `json:"is_optional"       db:"is_optional"`
	CompletionPolicy CompletionPolicy `json:"completion_policy" db:"completion_policy"`
	CreatedAt        time.Time        `json:"created_at"        db:"created_at"`
}

// ---------------------------------------------------------------------------
// TaskStatusHolder is a minimal interface for checking task completion.
// Any struct with a Status field can satisfy this by being cast to the
// concrete slice, or callers can pass lightweight structs.
// ---------------------------------------------------------------------------

// TaskWithStatus is a lightweight carrier for evaluating completion.
type TaskWithStatus struct {
	Status string
}

// ---------------------------------------------------------------------------
// IsActivityComplete evaluates whether the given tasks satisfy the policy.
//   - tasks is pre-filtered to belong to this activity already.
//   - ALL_TASKS: every task must be COMPLETED
//   - ANY_TASK:  at least one task must be COMPLETED
//   - MANUAL:    always returns false (requires external sign-off)
// ---------------------------------------------------------------------------

func IsActivityComplete(tasks []TaskWithStatus, policy CompletionPolicy) bool {
	if len(tasks) == 0 {
		// No tasks → vacuously true for ALL_TASKS, false for ANY_TASK
		return policy == CompletionAllTasks
	}

	switch policy {
	case CompletionAllTasks:
		for _, t := range tasks {
			if t.Status != string(TaskStatusDone) {
				return false
			}
		}
		return true

	case CompletionAnyTask:
		for _, t := range tasks {
			if t.Status == string(TaskStatusDone) {
				return true
			}
		}
		return false

	case CompletionManual:
		return false // requires explicit sign-off, never auto-completes

	default:
		return false
	}
}
