package approval

import (
	"context"
	"fmt"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// GetApprovalHistory returns full approval audit trail for a case.
func GetApprovalHistory(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
) ([]model.ApprovalAuditEntry, error) {
	if db == nil {
		return nil, fmt.Errorf("GetApprovalHistory: db is nil")
	}
	if caseID == "" {
		return nil, fmt.Errorf("GetApprovalHistory: caseID is required")
	}

	type row struct {
		ID                string     `db:"id"`
		ApprovalRequestID string     `db:"approval_request_id"`
		EventType         string     `db:"event_type"`
		ActorID           string     `db:"actor_id"`
		DecisionText      *string    `db:"decision_text"`
		EvidenceRefs      []byte     `db:"evidence_refs"`
		PreviousState     *string    `db:"previous_state"`
		NewState          *string    `db:"new_state"`
		CreatedAt         time.Time  `db:"created_at"`
		UpdatedAt         time.Time  `db:"updated_at"`
		ApproverName      *string    `db:"approver_name"`
	}

	var rows []row
	err := db.SelectContext(ctx, &rows, `
		SELECT
			l.id::text AS id,
			l.approval_request_id::text AS approval_request_id,
			l.event_type,
			l.actor_id,
			l.decision_text,
			l.evidence_refs,
			l.previous_state,
			l.new_state,
			l.created_at,
			l.updated_at,
			u.full_name AS approver_name
		FROM approval_audit_log l
		JOIN approval_requests r ON r.id = l.approval_request_id
		JOIN approval_gates g ON g.id = r.approval_gate_id
		LEFT JOIN users u ON u.id = COALESCE(r.decided_by, r.approver_id)
		WHERE g.case_id = $1::uuid
		ORDER BY l.created_at ASC
	`, caseID)
	if err != nil {
		return nil, fmt.Errorf("GetApprovalHistory: %w", err)
	}

	out := make([]model.ApprovalAuditEntry, 0, len(rows))
	for _, r := range rows {
		entry := model.ApprovalAuditEntry{
			ID:                r.ID,
			ApprovalRequestID: r.ApprovalRequestID,
			EventType:         model.ApprovalAuditEventType(r.EventType),
			ActorID:           r.ActorID,
			DecisionText:      r.DecisionText,
			EvidenceRefs:      r.EvidenceRefs,
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
			ApproverName:      r.ApproverName,
		}
		if r.PreviousState != nil {
			prev := model.ApprovalRequestStatus(*r.PreviousState)
			entry.PreviousState = &prev
		}
		if r.NewState != nil {
			next := model.ApprovalRequestStatus(*r.NewState)
			entry.NewState = &next
		}
		out = append(out, entry)
	}

	return out, nil
}
