package approval

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

const (
	EventTypeApprovalGateCreated   = model.EventApprovalGateCreated
	EventTypeApprovalRequested     = model.EventApprovalRequested
	EventTypeApprovalGranted       = model.EventApprovalGranted
	EventTypeApprovalRejected      = model.EventApprovalRejected
	EventTypeApprovalDelegated     = model.EventApprovalDelegated
	EventTypeApprovalExpired       = model.EventApprovalExpired
	EventTypeApprovalGateSatisfied = model.EventApprovalGateSatisfied
	EventTypeApprovalGateFailed    = model.EventApprovalGateFailed
	EventTypeCaseSentToRework      = model.EventCaseSentToRework
	EventTypeCaseRejected          = model.EventCaseRejected
	EventTypeCaseMaxReworkExceeded = model.EventCaseMaxReworkExceeded
	EventTypeNoEligibleApprover    = model.EventNoEligibleApprover
)

// EventPublisher abstracts outbox writes for approval workflows.
type EventPublisher interface {
	PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error
}

// SQLXEventPublisher writes approval events into events_outbox.
type SQLXEventPublisher struct{}

func (p *SQLXEventPublisher) PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	return PublishEvent(ctx, tx, event)
}

// ApprovalEventPayload is the shared envelope for approval events.
type ApprovalEventPayload struct {
	GateID         string                      `json:"gate_id,omitempty"`
	RequestID      string                      `json:"request_id,omitempty"`
	CaseID         string                      `json:"case_id,omitempty"`
	TaskID         string                      `json:"task_id,omitempty"`
	ApproverID     string                      `json:"approver_id,omitempty"`
	DelegatedToID  string                      `json:"delegated_to_id,omitempty"`
	Tier           *int                        `json:"tier,omitempty"`
	Policy         model.ApprovalPolicy        `json:"policy,omitempty"`
	TimeoutAction  model.TimeoutAction         `json:"timeout_action,omitempty"`
	GateStatus     model.ApprovalGateStatus    `json:"gate_status,omitempty"`
	RequestStatus  model.ApprovalRequestStatus `json:"request_status,omitempty"`
	Reason         string                      `json:"reason,omitempty"`
	DecisionText   string                      `json:"decision_text,omitempty"`
	RejectionStage string                      `json:"rejection_stage,omitempty"`
}

func PublishEvent(ctx context.Context, tx *sqlx.Tx, event model.Event) error {
	if tx == nil {
		return fmt.Errorf("PublishEvent: tx is nil")
	}
	if event.EventType == "" {
		return fmt.Errorf("PublishEvent: event type is required")
	}
	if event.Payload == nil {
		event.Payload = json.RawMessage("{}")
	}
	if event.Status == "" {
		event.Status = model.EventStatusPending
	}
	if event.TargetService == "" {
		event.TargetService = "case-orchestrator"
	}
	if event.MaxAttempts == 0 {
		event.MaxAttempts = 5
	}
	if event.PartitionKey == nil && event.CaseID != nil {
		event.PartitionKey = event.CaseID
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO events_outbox (
			case_id,
			task_id,
			event_type,
			payload,
			status,
			target_service,
			max_attempts,
			partition_key,
			trace_id
		) VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4::jsonb,
			$5,
			$6,
			$7,
			$8,
			$9
		)
	`, event.CaseID, event.TaskID, string(event.EventType), event.Payload, string(event.Status), event.TargetService, event.MaxAttempts, event.PartitionKey, event.TraceID)
	if err != nil {
		return fmt.Errorf("PublishEvent: %w", err)
	}

	return nil
}
