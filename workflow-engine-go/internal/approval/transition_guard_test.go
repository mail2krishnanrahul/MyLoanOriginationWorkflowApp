package approval

import (
	"context"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/stretchr/testify/assert"
)

func TestValidateApprovalTransition(t *testing.T) {
	tests := []struct {
		name      string
		current   model.ApprovalRequestStatus
		requested model.ApprovalRequestStatus
		actor     Actor
		wantErr   bool
	}{
		{
			name:      "happy path pending to approved",
			current:   model.ApprovalRequestStatusPending,
			requested: model.ApprovalRequestStatusApproved,
			actor: Actor{
				ID:           "approver-1",
				IsApprover:   true,
				DecisionText: "Looks good",
				EvidenceRefs: []string{},
			},
		},
		{
			name:      "edge case delegated action by original approver",
			current:   model.ApprovalRequestStatusDelegated,
			requested: model.ApprovalRequestStatusApproved,
			actor: Actor{
				ID:                 "approver-1",
				IsDelegate:         true,
				IsOriginalApprover: true,
				DecisionText:       "cannot",
				EvidenceRefs:       []string{},
			},
			wantErr: true,
		},
		{
			name:      "failure mode approved without decision text",
			current:   model.ApprovalRequestStatusPending,
			requested: model.ApprovalRequestStatusApproved,
			actor: Actor{
				ID:         "approver-2",
				IsApprover: true,
				EvidenceRefs: []string{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateApprovalTransition(context.Background(), tt.current, tt.requested, tt.actor)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}
