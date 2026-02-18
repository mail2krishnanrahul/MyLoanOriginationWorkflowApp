package approval

import (
	"context"
	"fmt"

	"workflow-engine/pkg/model"
)

// Actor represents caller identity for approval transition authorization.
type Actor struct {
	ID                 string
	IsSystem           bool
	IsSupervisor       bool
	IsApprover         bool
	IsDelegate         bool
	IsOriginalApprover bool
	DecisionText       string
	EvidenceRefs       []string
	DelegatedToID      *string
}

// ValidateApprovalTransition enforces approval_request lifecycle transitions.
func ValidateApprovalTransition(
	ctx context.Context,
	current model.ApprovalRequestStatus,
	requested model.ApprovalRequestStatus,
	actor Actor,
) error {
	_ = ctx
	if actor.ID == "" {
		return fmt.Errorf("ValidateApprovalTransition: %w: actor id is required", ErrUnauthorizedApprovalActor)
	}

	if current == requested {
		return fmt.Errorf("ValidateApprovalTransition: %w: no-op transition %s", ErrInvalidApprovalTransition, current)
	}

	switch current {
	case model.ApprovalRequestStatusApproved, model.ApprovalRequestStatusRejected:
		return fmt.Errorf("ValidateApprovalTransition: %w: terminal state %s", ErrInvalidApprovalTransition, current)

	case model.ApprovalRequestStatusPending:
		switch requested {
		case model.ApprovalRequestStatusApproved, model.ApprovalRequestStatusRejected:
			if !actor.IsApprover {
				return fmt.Errorf("ValidateApprovalTransition: %w: only approver may %s from PENDING", ErrUnauthorizedApprovalActor, requested)
			}
			if actor.IsSystem {
				return fmt.Errorf("ValidateApprovalTransition: %w: system cannot manually %s from PENDING", ErrUnauthorizedApprovalActor, requested)
			}
			if actor.DecisionText == "" {
				return fmt.Errorf("ValidateApprovalTransition: %w: decision text is required", ErrInvalidApprovalTransition)
			}
			if actor.EvidenceRefs == nil {
				return fmt.Errorf("ValidateApprovalTransition: %w: evidence_refs must be present (empty array allowed)", ErrInvalidApprovalTransition)
			}
			return nil

		case model.ApprovalRequestStatusDelegated:
			if !actor.IsApprover {
				return fmt.Errorf("ValidateApprovalTransition: %w: only approver may delegate", ErrUnauthorizedApprovalActor)
			}
			if actor.DelegatedToID == nil || *actor.DelegatedToID == "" {
				return fmt.Errorf("ValidateApprovalTransition: %w: delegated_to_id is required", ErrInvalidApprovalTransition)
			}
			return nil

		case model.ApprovalRequestStatusExpired:
			if !actor.IsSystem {
				return fmt.Errorf("ValidateApprovalTransition: %w: only system may expire request", ErrUnauthorizedApprovalActor)
			}
			return nil
		}

	case model.ApprovalRequestStatusDelegated:
		switch requested {
		case model.ApprovalRequestStatusApproved, model.ApprovalRequestStatusRejected:
			if !actor.IsDelegate || actor.IsOriginalApprover {
				return fmt.Errorf("ValidateApprovalTransition: %w: only delegate may %s delegated request", ErrUnauthorizedApprovalActor, requested)
			}
			if actor.DecisionText == "" {
				return fmt.Errorf("ValidateApprovalTransition: %w: decision text is required", ErrInvalidApprovalTransition)
			}
			if actor.EvidenceRefs == nil {
				return fmt.Errorf("ValidateApprovalTransition: %w: evidence_refs must be present (empty array allowed)", ErrInvalidApprovalTransition)
			}
			return nil
		}

	case model.ApprovalRequestStatusExpired:
		switch requested {
		case model.ApprovalRequestStatusApproved, model.ApprovalRequestStatusRejected, model.ApprovalRequestStatusPending:
			if !actor.IsSystem {
				return fmt.Errorf("ValidateApprovalTransition: %w: only system may transition EXPIRED -> %s", ErrUnauthorizedApprovalActor, requested)
			}
			return nil
		}
	}

	return fmt.Errorf("ValidateApprovalTransition: %w: %s -> %s", ErrInvalidApprovalTransition, current, requested)
}
