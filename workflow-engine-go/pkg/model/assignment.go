package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

type Proficiency string

const (
	ProficiencyBeginner  Proficiency = "BEGINNER"
	ProficiencyCompetent Proficiency = "COMPETENT"
	ProficiencyExpert    Proficiency = "EXPERT"
)

type WorkbasketType string

const (
	WorkbasketTypeGeneral    WorkbasketType = "GENERAL"
	WorkbasketTypeSpecialist WorkbasketType = "SPECIALIST"
	WorkbasketTypeEscalation WorkbasketType = "ESCALATION"
)

type AssignmentStrategy string

const (
	StrategyRoundRobin  AssignmentStrategy = "ROUND_ROBIN"
	StrategyLeastLoaded AssignmentStrategy = "LEAST_LOADED"
	StrategySkillScore  AssignmentStrategy = "SKILL_SCORE"
)

type DelegationType string

const (
	DelegationManual      DelegationType = "MANUAL"
	DelegationAutoEsc     DelegationType = "AUTO_ESCALATION"
	DelegationOutOfOffice DelegationType = "OUT_OF_OFFICE"
)

// ---------------------------------------------------------------------------
// Structs
// ---------------------------------------------------------------------------

type Skill struct {
	Code        string `json:"code"        db:"code"`
	Description string `json:"description" db:"description"`
}

type Worker struct {
	ID                 string    `json:"id"                   db:"id"`
	MaxConcurrentTasks int       `json:"max_concurrent_tasks" db:"max_concurrent_tasks"`
	Status             string    `json:"status"               db:"status"` // ACTIVE, INACTIVE
	CreatedAt          time.Time `json:"created_at"           db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"           db:"updated_at"`
}

type WorkerSkill struct {
	WorkerID    string      `json:"worker_id"   db:"worker_id"`
	SkillCode   string      `json:"skill_code"  db:"skill_code"`
	Proficiency Proficiency `json:"proficiency" db:"proficiency"`
	CreatedAt   time.Time   `json:"created_at"  db:"created_at"`
}

type Workbasket struct {
	ID               string             `json:"id"                 db:"id"`
	Name             string             `json:"name"               db:"name"`
	Type             WorkbasketType     `json:"type"               db:"type"`
	Strategy         AssignmentStrategy `json:"strategy"           db:"strategy"`
	RoundRobinCursor *string            `json:"round_robin_cursor" db:"round_robin_cursor"`
	CreatedAt        time.Time          `json:"created_at"         db:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"         db:"updated_at"`
}

type WorkbasketMember struct {
	WorkbasketID string    `json:"workbasket_id" db:"workbasket_id"`
	WorkerID     string    `json:"worker_id"     db:"worker_id"`
	JoinedAt     time.Time `json:"joined_at"     db:"joined_at"`
}

type WorkerAvailability struct {
	ID             string    `json:"id"              db:"id"`
	WorkerID       string    `json:"worker_id"       db:"worker_id"`
	AvailableFrom  time.Time `json:"available_from"  db:"available_from"`
	AvailableUntil time.Time `json:"available_until" db:"available_until"`
	Timezone       string    `json:"timezone"        db:"timezone"`
	CreatedAt      time.Time `json:"created_at"      db:"created_at"`
}

type TaskDelegation struct {
	ID             string         `json:"id"               db:"id"`
	TaskID         string         `json:"task_id"          db:"task_id"`
	FromAssignee   *string        `json:"from_assignee"    db:"from_assignee"`
	ToAssignee     *string        `json:"to_assignee"      db:"to_assignee"`
	ToWorkbasketID *string        `json:"to_workbasket_id" db:"to_workbasket_id"`
	Reason         *string        `json:"reason"           db:"reason"`
	DelegationType DelegationType `json:"delegation_type"  db:"delegation_type"`
	DelegatedBy    string         `json:"delegated_by"     db:"delegated_by"`
	DelegatedAt    time.Time      `json:"delegated_at"     db:"delegated_at"`
}

type SLABreach struct {
	ID                string     `json:"id"                 db:"id"`
	TaskID            string     `json:"task_id"            db:"task_id"`
	CaseID            string     `json:"case_id"            db:"case_id"`
	OriginalDueAt     *time.Time `json:"original_due_at"    db:"original_due_at"`
	BreachDetectedAt  time.Time  `json:"breach_detected_at" db:"breach_detected_at"`
	AssigneeAtBreach  *string    `json:"assignee_at_breach" db:"assignee_at_breach"`
	ElapsedPercentage int        `json:"elapsed_percentage" db:"elapsed_percentage"`
}

// RequiredSkill defines a skill dependency for a task
type RequiredSkill struct {
	Code           string      `json:"code"`
	MinProficiency Proficiency `json:"min_proficiency"`
}

// TaskAssignmentExtension represents assignment-specific fields on Task
// These are added to the main Task struct via join or separate helper
type TaskAssignment struct {
	WorkbasketID   *string         `json:"workbasket_id"   db:"workbasket_id"`
	AssigneeID     *string         `json:"assignee_id"     db:"assignee_id"`
	RequiredSkills json.RawMessage `json:"required_skills" db:"required_skills"`
}
