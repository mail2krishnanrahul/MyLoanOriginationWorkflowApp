package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// AuditAction — typed constants for audit trail actions
// ---------------------------------------------------------------------------

type AuditAction string

const (
	AuditCaseCreated       AuditAction = "CASE_CREATED"
	AuditCaseCompleted     AuditAction = "CASE_COMPLETED"
	AuditStageChanged      AuditAction = "STAGE_CHANGED"
	AuditTaskCreated       AuditAction = "TASK_CREATED"
	AuditTaskCompleted     AuditAction = "TASK_COMPLETED"
	AuditTaskFailed        AuditAction = "TASK_FAILED"
	AuditTaskEscalated     AuditAction = "TASK_ESCALATED"
	AuditActivityCompleted AuditAction = "ACTIVITY_COMPLETED"
)

// AuditEntityType — the type of entity that was mutated.
type AuditEntityType string

const (
	AuditEntityCase     AuditEntityType = "CASE"
	AuditEntityTask     AuditEntityType = "TASK"
	AuditEntityStage    AuditEntityType = "STAGE"
	AuditEntityActivity AuditEntityType = "ACTIVITY"
)

// AuditActorType — who performed the action.
type AuditActorType string

const (
	AuditActorUser   AuditActorType = "USER"
	AuditActorSystem AuditActorType = "SYSTEM"
	AuditActorAPI    AuditActorType = "API"
)

// ---------------------------------------------------------------------------
// AuditEntry — one row in the audit_trail table
// ---------------------------------------------------------------------------

// AuditEntry records a single mutation event for a case.
type AuditEntry struct {
	ID          string          `json:"id"            db:"id"`
	CaseID      string          `json:"case_id"       db:"case_id"`
	Action      AuditAction     `json:"action"        db:"action"`
	EntityType  AuditEntityType `json:"entity_type"   db:"entity_type"`
	EntityID    *string         `json:"entity_id"     db:"entity_id"`
	ActorID     string          `json:"actor_id"      db:"actor_id"`
	ActorType   AuditActorType  `json:"actor_type"    db:"actor_type"`
	ChangeDelta json.RawMessage `json:"change_delta"  db:"change_delta"`
	Metadata    json.RawMessage `json:"metadata"      db:"metadata"`
	CreatedAt   time.Time       `json:"created_at"    db:"created_at"`
}
