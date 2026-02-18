package model

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Approval enums
// ---------------------------------------------------------------------------

type ApprovalPolicy string

const (
	ApprovalPolicySingleApprover ApprovalPolicy = "SINGLE_APPROVER"
	ApprovalPolicyAllMustApprove ApprovalPolicy = "ALL_MUST_APPROVE"
	ApprovalPolicyAnyCanApprove  ApprovalPolicy = "ANY_CAN_APPROVE"
	ApprovalPolicyMajority       ApprovalPolicy = "MAJORITY"
	ApprovalPolicyConsensus      ApprovalPolicy = "CONSENSUS"
)

type ApproverSelection string

const (
	ApproverSelectionExplicitList   ApproverSelection = "EXPLICIT_LIST"
	ApproverSelectionRoleBased      ApproverSelection = "ROLE_BASED"
	ApproverSelectionReportingChain ApproverSelection = "REPORTING_CHAIN"
	ApproverSelectionDynamicRule    ApproverSelection = "DYNAMIC_RULE"
)

type TimeoutAction string

const (
	TimeoutActionAutoApprove TimeoutAction = "AUTO_APPROVE"
	TimeoutActionAutoReject  TimeoutAction = "AUTO_REJECT"
	TimeoutActionEscalate    TimeoutAction = "ESCALATE"
)

type RejectionBehavior string

const (
	RejectionBehaviorSendToRework RejectionBehavior = "SEND_TO_REWORK"
	RejectionBehaviorTerminalFail RejectionBehavior = "TERMINAL_FAIL"
)

type ApprovalGateStatus string

const (
	ApprovalGateStatusPending                 ApprovalGateStatus = "PENDING"
	ApprovalGateStatusActive                  ApprovalGateStatus = "ACTIVE"
	ApprovalGateStatusSatisfied               ApprovalGateStatus = "SATISFIED"
	ApprovalGateStatusFailed                  ApprovalGateStatus = "FAILED"
	ApprovalGateStatusRejected                ApprovalGateStatus = "REJECTED"
	ApprovalGateStatusRejectedReworkInitiated ApprovalGateStatus = "REJECTED_REWORK_INITIATED"
	ApprovalGateStatusExpired                 ApprovalGateStatus = "EXPIRED"
	ApprovalGateStatusCancelled               ApprovalGateStatus = "CANCELLED"
)

type ApprovalRequestStatus string

const (
	ApprovalRequestStatusPending   ApprovalRequestStatus = "PENDING"
	ApprovalRequestStatusApproved  ApprovalRequestStatus = "APPROVED"
	ApprovalRequestStatusRejected  ApprovalRequestStatus = "REJECTED"
	ApprovalRequestStatusExpired   ApprovalRequestStatus = "EXPIRED"
	ApprovalRequestStatusDelegated ApprovalRequestStatus = "DELEGATED"
)

type ApprovalAuditEventType string

const (
	ApprovalAuditEventRequested    ApprovalAuditEventType = "REQUESTED"
	ApprovalAuditEventApproved     ApprovalAuditEventType = "APPROVED"
	ApprovalAuditEventRejected     ApprovalAuditEventType = "REJECTED"
	ApprovalAuditEventDelegated    ApprovalAuditEventType = "DELEGATED"
	ApprovalAuditEventExpired      ApprovalAuditEventType = "EXPIRED"
	ApprovalAuditEventAutoApproved ApprovalAuditEventType = "AUTO_APPROVED"
	ApprovalAuditEventAutoRejected ApprovalAuditEventType = "AUTO_REJECTED"
	ApprovalAuditEventEscalated    ApprovalAuditEventType = "ESCALATED"
)

type ApprovalChainTierStatus string

const (
	ApprovalChainTierStatusPending  ApprovalChainTierStatus = "PENDING"
	ApprovalChainTierStatusApproved ApprovalChainTierStatus = "APPROVED"
	ApprovalChainTierStatusRejected ApprovalChainTierStatus = "REJECTED"
	ApprovalChainTierStatusSkipped  ApprovalChainTierStatus = "SKIPPED"
)

type ApprovalChainStatus string

const (
	ApprovalChainStatusPending    ApprovalChainStatus = "PENDING"
	ApprovalChainStatusInProgress ApprovalChainStatus = "IN_PROGRESS"
	ApprovalChainStatusCompleted  ApprovalChainStatus = "COMPLETED"
	ApprovalChainStatusFailed     ApprovalChainStatus = "FAILED"
)

type AuthorityChangeType string

const (
	AuthorityChangeTypeGranted  AuthorityChangeType = "GRANTED"
	AuthorityChangeTypeRevoked  AuthorityChangeType = "REVOKED"
	AuthorityChangeTypeModified AuthorityChangeType = "MODIFIED"
)

// ---------------------------------------------------------------------------
// Config structs (case_type JSONB)
// ---------------------------------------------------------------------------

type ApprovalDefinition struct {
	ApprovalPolicy         ApprovalPolicy    `json:"approval_policy"`
	RequiredApproverCount  int               `json:"required_approver_count,omitempty"`
	ApproverSelection      ApproverSelection `json:"approver_selection"`
	Approvers              []string          `json:"approvers,omitempty"`
	AuthorityLimit         *float64          `json:"authority_limit,omitempty"`
	ApprovalAmountField    string            `json:"approval_amount_field,omitempty"`
	ApprovalTimeoutHours   float64           `json:"approval_timeout_hours"`
	OnTimeoutAction        TimeoutAction     `json:"on_timeout_action"`
	RejectionBehavior      RejectionBehavior `json:"rejection_behavior"`
	ReworkTargetStageCode  *string           `json:"rework_target_stage_code,omitempty"`
	FallbackSupervisorRole string            `json:"fallback_supervisor_role,omitempty"`
	DynamicRule            string            `json:"dynamic_rule,omitempty"`
}

type ApprovalChainTierDefinition struct {
	Tier           int      `json:"tier"`
	ApproverRole   string   `json:"approver_role"`
	Approvers      []string `json:"approvers,omitempty"`
	ApprovalPolicy ApprovalPolicy `json:"approval_policy,omitempty"`
	AuthorityLimit *float64 `json:"authority_limit,omitempty"`
	CanSkipIf      string   `json:"can_skip_if,omitempty"`
	RequiredIf     string   `json:"required_if,omitempty"`
}

// ---------------------------------------------------------------------------
// DB row models
// ---------------------------------------------------------------------------

type User struct {
	ID        string    `json:"id" db:"id"`
	FullName  string    `json:"full_name" db:"full_name"`
	RoleCode  string    `json:"role_code" db:"role_code"`
	ManagerID *string   `json:"manager_id,omitempty" db:"manager_id"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type ApprovalGate struct {
	ID                     string             `json:"id" db:"id"`
	TaskID                 string             `json:"task_id" db:"task_id"`
	CaseID                 string             `json:"case_id" db:"case_id"`
	ApprovalPolicy         ApprovalPolicy     `json:"approval_policy" db:"approval_policy"`
	RequiredApproverCount  int                `json:"required_approver_count" db:"required_approver_count"`
	ApproverSelection      ApproverSelection  `json:"approver_selection" db:"approver_selection"`
	Approvers              json.RawMessage    `json:"approvers" db:"approvers"`
	AuthorityLimit         *float64           `json:"authority_limit,omitempty" db:"authority_limit"`
	ApprovalAmount         *float64           `json:"approval_amount,omitempty" db:"approval_amount"`
	ApprovalTimeoutHours   float64            `json:"approval_timeout_hours" db:"approval_timeout_hours"`
	OnTimeoutAction        TimeoutAction      `json:"on_timeout_action" db:"on_timeout_action"`
	RejectionBehavior      RejectionBehavior  `json:"rejection_behavior" db:"rejection_behavior"`
	ReworkTargetStageCode  *string            `json:"rework_target_stage_code,omitempty" db:"rework_target_stage_code"`
	FallbackSupervisorRole *string            `json:"fallback_supervisor_role,omitempty" db:"fallback_supervisor_role"`
	DynamicRuleExpression  *string            `json:"dynamic_rule_expression,omitempty" db:"dynamic_rule_expression"`
	ChainDefinition        json.RawMessage    `json:"chain_definition,omitempty" db:"chain_definition"`
	GateStatus             ApprovalGateStatus `json:"gate_status" db:"gate_status"`
	OpenedAt               *time.Time         `json:"opened_at,omitempty" db:"opened_at"`
	ClosedAt               *time.Time         `json:"closed_at,omitempty" db:"closed_at"`
	Version                int                `json:"version" db:"version"`
	CreatedAt              time.Time          `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time          `json:"updated_at" db:"updated_at"`
}

type ApprovalRequest struct {
	ID              string                `json:"id" db:"id"`
	ApprovalGateID  string                `json:"approval_gate_id" db:"approval_gate_id"`
	ApproverID      string                `json:"approver_id" db:"approver_id"`
	Tier            *int                  `json:"tier,omitempty" db:"tier"`
	Status          ApprovalRequestStatus `json:"status" db:"status"`
	Decision        *string               `json:"decision,omitempty" db:"decision"`
	EvidenceRefs    json.RawMessage       `json:"evidence_refs" db:"evidence_refs"`
	DecidedAt       *time.Time            `json:"decided_at,omitempty" db:"decided_at"`
	DecidedBy       *string               `json:"decided_by,omitempty" db:"decided_by"`
	ExpiresAt       time.Time             `json:"expires_at" db:"expires_at"`
	DelegatedToID   *string               `json:"delegated_to_id,omitempty" db:"delegated_to_id"`
	DelegatedAt     *time.Time            `json:"delegated_at,omitempty" db:"delegated_at"`
	DelegationChain json.RawMessage       `json:"delegation_chain" db:"delegation_chain"`
	Version         int                   `json:"version" db:"version"`
	CreatedAt       time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time             `json:"updated_at" db:"updated_at"`
}

type ApprovalChainState struct {
	ID                       string                  `json:"id" db:"id"`
	CaseID                   string                  `json:"case_id" db:"case_id"`
	ApprovalGateID           string                  `json:"approval_gate_id" db:"approval_gate_id"`
	ApprovalChainDefinition  json.RawMessage         `json:"approval_chain_definition" db:"approval_chain_definition"`
	CurrentTier              int                     `json:"current_tier" db:"current_tier"`
	TierStatus               ApprovalChainTierStatus `json:"tier_status" db:"tier_status"`
	TierStartedAt            *time.Time              `json:"tier_started_at,omitempty" db:"tier_started_at"`
	TierCompletedAt          *time.Time              `json:"tier_completed_at,omitempty" db:"tier_completed_at"`
	ChainStatus              ApprovalChainStatus     `json:"chain_status" db:"chain_status"`
	CreatedAt                time.Time               `json:"created_at" db:"created_at"`
	UpdatedAt                time.Time               `json:"updated_at" db:"updated_at"`
}

type UserAuthority struct {
	ID            string     `json:"id" db:"id"`
	UserID        string     `json:"user_id" db:"user_id"`
	AuthorityType string     `json:"authority_type" db:"authority_type"`
	MaxAmount     float64    `json:"max_amount" db:"max_amount"`
	GrantedBy     string     `json:"granted_by" db:"granted_by"`
	GrantedAt     time.Time  `json:"granted_at" db:"granted_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`
	RevokedBy     *string    `json:"revoked_by,omitempty" db:"revoked_by"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

type AuthorityLimitHistory struct {
	ID            string              `json:"id" db:"id"`
	UserID        string              `json:"user_id" db:"user_id"`
	AuthorityType string              `json:"authority_type" db:"authority_type"`
	MaxAmount     float64             `json:"max_amount" db:"max_amount"`
	ChangeType    AuthorityChangeType `json:"change_type" db:"change_type"`
	ChangedBy     string              `json:"changed_by" db:"changed_by"`
	ChangedAt     time.Time           `json:"changed_at" db:"changed_at"`
	Reason        *string             `json:"reason,omitempty" db:"reason"`
	CreatedAt     time.Time           `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at" db:"updated_at"`
}

type ApprovalAuditEntry struct {
	ID                string                 `json:"id" db:"id"`
	ApprovalRequestID string                 `json:"approval_request_id" db:"approval_request_id"`
	EventType         ApprovalAuditEventType `json:"event_type" db:"event_type"`
	ActorID           string                 `json:"actor_id" db:"actor_id"`
	DecisionText      *string                `json:"decision_text,omitempty" db:"decision_text"`
	EvidenceRefs      json.RawMessage        `json:"evidence_refs" db:"evidence_refs"`
	PreviousState     *ApprovalRequestStatus `json:"previous_state,omitempty" db:"previous_state"`
	NewState          *ApprovalRequestStatus `json:"new_state,omitempty" db:"new_state"`
	CreatedAt         time.Time              `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at" db:"updated_at"`

	// hydrated fields for history views
	ApproverName *string `json:"approver_name,omitempty" db:"approver_name"`
}
