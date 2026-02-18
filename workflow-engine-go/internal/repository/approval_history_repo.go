package repository

import (
	"context"
	"fmt"

	"workflow-engine/internal/approval"
	"workflow-engine/pkg/model"
)

// GetApprovalHistory returns immutable approval audit entries for a case.
func (r *Repository) GetApprovalHistory(ctx context.Context, caseID string) ([]model.ApprovalAuditEntry, error) {
	if r.SQLX == nil {
		return nil, fmt.Errorf("GetApprovalHistory: sqlx is not configured")
	}
	rows, err := approval.GetApprovalHistory(ctx, r.SQLX, caseID)
	if err != nil {
		return nil, fmt.Errorf("GetApprovalHistory: %w", err)
	}
	return rows, nil
}
