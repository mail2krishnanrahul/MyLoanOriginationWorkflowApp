package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type ApprovalPolicyEvaluator struct {
	db             *sqlx.DB
	logger         *slog.Logger
	eventPublisher EventPublisher
}

func NewApprovalPolicyEvaluator(db *sqlx.DB, logger *slog.Logger, publisher EventPublisher) *ApprovalPolicyEvaluator {
	if logger == nil {
		logger = slog.Default()
	}
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	return &ApprovalPolicyEvaluator{
		db:             db,
		logger:         logger,
		eventPublisher: publisher,
	}
}

type gateSnapshot struct {
	ID                    string                   `db:"id"`
	CaseID                string                   `db:"case_id"`
	TaskID                string                   `db:"task_id"`
	ApprovalPolicy        model.ApprovalPolicy     `db:"approval_policy"`
	RequiredApproverCount int                      `db:"required_approver_count"`
	GateStatus            model.ApprovalGateStatus `db:"gate_status"`
	Version               int                      `db:"version"`
}

type requestCounts struct {
	Total    int
	Approved int
	Rejected int
	Pending  int
	Expired  int
}

func (e *ApprovalPolicyEvaluator) EvaluateApprovalPolicy(
	ctx context.Context,
	tx *sqlx.Tx,
	gateID string,
) (satisfied bool, err error) {
	if tx == nil {
		return false, fmt.Errorf("EvaluateApprovalPolicy: tx is nil")
	}

	gate, err := loadGateSnapshot(ctx, tx, gateID)
	if err != nil {
		return false, fmt.Errorf("EvaluateApprovalPolicy: %w", err)
	}

	if gate.GateStatus == model.ApprovalGateStatusSatisfied {
		return true, nil
	}
	if gate.GateStatus == model.ApprovalGateStatusFailed || gate.GateStatus == model.ApprovalGateStatusRejected || gate.GateStatus == model.ApprovalGateStatusCancelled {
		return false, nil
	}

	counts, err := loadRequestCounts(ctx, tx, gateID)
	if err != nil {
		return false, fmt.Errorf("EvaluateApprovalPolicy: load requests: %w", err)
	}

	policyResult := evaluatePolicyCounts(gate.ApprovalPolicy, gate.RequiredApproverCount, counts)
	if policyResult.Err != nil {
		return false, fmt.Errorf("EvaluateApprovalPolicy: %w", policyResult.Err)
	}

	now := time.Now().UTC()
	if policyResult.Failed {
		updated, updateErr := closeGate(ctx, tx, gate.ID, gate.Version, model.ApprovalGateStatusFailed, now)
		if updateErr != nil {
			return false, fmt.Errorf("EvaluateApprovalPolicy: close gate failed: %w", updateErr)
		}
		if updated {
			if err := e.publishGateEvent(ctx, tx, gate, model.EventApprovalGateFailed, model.ApprovalGateStatusFailed, counts, "policy_fail_fast"); err != nil {
				return false, fmt.Errorf("EvaluateApprovalPolicy: publish fail event: %w", err)
			}
		}
		return false, nil
	}

	if policyResult.Satisfied {
		updated, updateErr := closeGate(ctx, tx, gate.ID, gate.Version, model.ApprovalGateStatusSatisfied, now)
		if updateErr != nil {
			return false, fmt.Errorf("EvaluateApprovalPolicy: close gate satisfied: %w", updateErr)
		}
		if updated {
			if _, err := tx.ExecContext(ctx, `
				UPDATE tasks
				SET status = CASE
					WHEN status = 'AWAITING_EXTERNAL' THEN 'PENDING'
					ELSE status
				END,
				assigned_service = CASE WHEN status = 'AWAITING_EXTERNAL' THEN NULL ELSE assigned_service END,
				assigned_at = CASE WHEN status = 'AWAITING_EXTERNAL' THEN NULL ELSE assigned_at END,
				last_heartbeat_at = CASE WHEN status = 'AWAITING_EXTERNAL' THEN NULL ELSE last_heartbeat_at END,
				updated_at = now(),
				version = version + 1
				WHERE id = $1::uuid
			`, gate.TaskID); err != nil {
				return false, fmt.Errorf("EvaluateApprovalPolicy: unblock task: %w", err)
			}
			if err := e.publishGateEvent(ctx, tx, gate, model.EventApprovalGateSatisfied, model.ApprovalGateStatusSatisfied, counts, "policy_satisfied"); err != nil {
				return false, fmt.Errorf("EvaluateApprovalPolicy: publish satisfied event: %w", err)
			}
		}
		return true, nil
	}

	return false, nil
}

func loadGateSnapshot(ctx context.Context, tx *sqlx.Tx, gateID string) (gateSnapshot, error) {
	var gate gateSnapshot
	err := tx.GetContext(ctx, &gate, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			task_id::text AS task_id,
			approval_policy,
			required_approver_count,
			gate_status,
			version
		FROM approval_gates
		WHERE id = $1::uuid
		FOR UPDATE
	`, gateID)
	if err != nil {
		return gateSnapshot{}, err
	}
	return gate, nil
}

func loadRequestCounts(ctx context.Context, tx *sqlx.Tx, gateID string) (requestCounts, error) {
	type row struct {
		Status string `db:"status"`
		Count  int    `db:"cnt"`
	}
	var rows []row
	err := tx.SelectContext(ctx, &rows, `
		SELECT status, COUNT(*) AS cnt
		FROM approval_requests
		WHERE approval_gate_id = $1::uuid
		  AND status <> 'DELEGATED'
		GROUP BY status
	`, gateID)
	if err != nil {
		return requestCounts{}, err
	}
	counts := requestCounts{}
	for _, r := range rows {
		switch r.Status {
		case "PENDING":
			counts.Pending = r.Count
		case "APPROVED":
			counts.Approved = r.Count
		case "REJECTED":
			counts.Rejected = r.Count
		case "EXPIRED":
			counts.Expired = r.Count
		}
		counts.Total += r.Count
	}
	return counts, nil
}

type policyEvaluationResult struct {
	Satisfied bool
	Failed    bool
	Err       error
}

func evaluatePolicyCounts(policy model.ApprovalPolicy, required int, counts requestCounts) policyEvaluationResult {
	total := counts.Total
	if total == 0 {
		return policyEvaluationResult{}
	}

	switch policy {
	case model.ApprovalPolicySingleApprover:
		return policyEvaluationResult{Satisfied: counts.Approved >= 1}

	case model.ApprovalPolicyAllMustApprove:
		if counts.Rejected > 0 {
			return policyEvaluationResult{Failed: true}
		}
		return policyEvaluationResult{Satisfied: counts.Approved == total}

	case model.ApprovalPolicyAnyCanApprove:
		needed := 1
		if required > 1 {
			needed = required
		}
		if counts.Approved >= needed {
			return policyEvaluationResult{Satisfied: true}
		}
		if counts.Rejected == total || (counts.Approved+counts.Pending) < needed {
			return policyEvaluationResult{Failed: true}
		}
		return policyEvaluationResult{}

	case model.ApprovalPolicyMajority:
		threshold := total / 2
		if counts.Approved > threshold {
			return policyEvaluationResult{Satisfied: true}
		}
		if counts.Rejected > threshold {
			return policyEvaluationResult{Failed: true}
		}
		if required > 0 && counts.Approved >= required {
			return policyEvaluationResult{Satisfied: true}
		}
		return policyEvaluationResult{}

	case model.ApprovalPolicyConsensus:
		requiredPct := 0.66
		approvedPct := float64(counts.Approved) / float64(total)
		if approvedPct > requiredPct {
			return policyEvaluationResult{Satisfied: true}
		}
		maxPossible := float64(counts.Approved+counts.Pending) / float64(total)
		if maxPossible <= requiredPct {
			return policyEvaluationResult{Failed: true}
		}
		if required > 0 && counts.Approved >= required {
			return policyEvaluationResult{Satisfied: true}
		}
		return policyEvaluationResult{}

	default:
		return policyEvaluationResult{Err: fmt.Errorf("unsupported policy %s", policy)}
	}
}

func closeGate(ctx context.Context, tx *sqlx.Tx, gateID string, currentVersion int, status model.ApprovalGateStatus, now time.Time) (bool, error) {
	result, err := tx.ExecContext(ctx, `
		UPDATE approval_gates
		SET gate_status = $1,
			closed_at = $2,
			updated_at = now(),
			version = version + 1
		WHERE id = $3::uuid
		  AND version = $4
		  AND gate_status IN ('PENDING', 'ACTIVE')
	`, string(status), now, gateID, currentVersion)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (e *ApprovalPolicyEvaluator) publishGateEvent(
	ctx context.Context,
	tx *sqlx.Tx,
	gate gateSnapshot,
	eventType model.EventType,
	status model.ApprovalGateStatus,
	counts requestCounts,
	reason string,
) error {
	payload, err := json.Marshal(ApprovalEventPayload{
		GateID:       gate.ID,
		CaseID:       gate.CaseID,
		TaskID:       gate.TaskID,
		Policy:       gate.ApprovalPolicy,
		GateStatus:   status,
		Reason:       reason,
		DecisionText: fmt.Sprintf("approved=%d rejected=%d pending=%d total=%d", counts.Approved, counts.Rejected, counts.Pending, counts.Total),
	})
	if err != nil {
		return err
	}
	caseID := gate.CaseID
	taskID := gate.TaskID
	publisher := e.eventPublisher
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	return publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		TaskID:    &taskID,
		EventType: eventType,
		Payload:   payload,
		Status:    model.EventStatusPending,
	})
}

// EvaluateApprovalChain advances tiered approvals and updates chain state.
func (e *ApprovalPolicyEvaluator) EvaluateApprovalChain(
	ctx context.Context,
	tx *sqlx.Tx,
	chainID string,
) error {
	if tx == nil {
		return fmt.Errorf("EvaluateApprovalChain: tx is nil")
	}

	type chainSnapshot struct {
		ID                      string                        `db:"id"`
		CaseID                  string                        `db:"case_id"`
		ApprovalGateID          string                        `db:"approval_gate_id"`
		ApprovalChainDefinition json.RawMessage               `db:"approval_chain_definition"`
		CurrentTier             int                           `db:"current_tier"`
		TierStatus              model.ApprovalChainTierStatus `db:"tier_status"`
		ChainStatus             model.ApprovalChainStatus     `db:"chain_status"`
	}
	var chain chainSnapshot
	if err := tx.GetContext(ctx, &chain, `
		SELECT
			id::text AS id,
			case_id::text AS case_id,
			approval_gate_id::text AS approval_gate_id,
			approval_chain_definition,
			current_tier,
			tier_status,
			chain_status
		FROM approval_chain_state
		WHERE id = $1::uuid
		FOR UPDATE
	`, chainID); err != nil {
		return fmt.Errorf("EvaluateApprovalChain: load chain: %w", err)
	}

	if chain.ChainStatus == model.ApprovalChainStatusCompleted || chain.ChainStatus == model.ApprovalChainStatusFailed {
		return nil
	}

	var tiers []model.ApprovalChainTierDefinition
	if err := json.Unmarshal(chain.ApprovalChainDefinition, &tiers); err != nil {
		return fmt.Errorf("EvaluateApprovalChain: decode definition: %w", err)
	}
	if len(tiers) == 0 {
		return nil
	}

	var metadataRaw []byte
	if err := tx.GetContext(ctx, &metadataRaw, `
		SELECT metadata
		FROM cases
		WHERE id = $1::uuid
	`, chain.CaseID); err != nil {
		return fmt.Errorf("EvaluateApprovalChain: load case metadata: %w", err)
	}
	metadata := map[string]interface{}{}
	if len(metadataRaw) > 0 {
		if err := json.Unmarshal(metadataRaw, &metadata); err != nil {
			return fmt.Errorf("EvaluateApprovalChain: parse case metadata: %w", err)
		}
	}

	tierDef, idx := findTierDefinition(tiers, chain.CurrentTier)
	if tierDef == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE approval_chain_state
			SET chain_status = 'COMPLETED',
				tier_status = 'APPROVED',
				tier_completed_at = now(),
				updated_at = now()
			WHERE id = $1::uuid
		`, chain.ID); err != nil {
			return fmt.Errorf("EvaluateApprovalChain: mark completed: %w", err)
		}
		return nil
	}

	evaluator := &ExpressionEvaluator{}
	if tierDef.CanSkipIf != "" {
		skip, err := evaluator.Evaluate(ctx, tierDef.CanSkipIf, metadata)
		if err != nil {
			return fmt.Errorf("EvaluateApprovalChain: can_skip_if tier %d: %w", tierDef.Tier, err)
		}
		if skip {
			nextTier := nextTierValue(tiers, idx)
			if nextTier == 0 {
				nextTier = chain.CurrentTier
			}
			_, err := tx.ExecContext(ctx, `
				UPDATE approval_chain_state
				SET tier_status = 'SKIPPED',
					tier_completed_at = now(),
					current_tier = $2,
					chain_status = CASE WHEN $3 THEN 'COMPLETED' ELSE 'IN_PROGRESS' END,
					updated_at = now()
				WHERE id = $1::uuid
			`, chain.ID, nextTier, idx == len(tiers)-1)
			if err != nil {
				return fmt.Errorf("EvaluateApprovalChain: mark skipped: %w", err)
			}
			return nil
		}
	}

	if tierDef.RequiredIf != "" {
		required, err := evaluator.Evaluate(ctx, tierDef.RequiredIf, metadata)
		if err != nil {
			return fmt.Errorf("EvaluateApprovalChain: required_if tier %d: %w", tierDef.Tier, err)
		}
		if !required {
			nextTier := nextTierValue(tiers, idx)
			_, err := tx.ExecContext(ctx, `
				UPDATE approval_chain_state
				SET tier_status = 'SKIPPED',
					tier_completed_at = now(),
					current_tier = $2,
					chain_status = CASE WHEN $3 THEN 'COMPLETED' ELSE 'IN_PROGRESS' END,
					updated_at = now()
				WHERE id = $1::uuid
			`, chain.ID, nextTier, idx == len(tiers)-1)
			if err != nil {
				return fmt.Errorf("EvaluateApprovalChain: skip optional tier: %w", err)
			}
			return nil
		}
	}

	counts, err := loadTierCounts(ctx, tx, chain.ApprovalGateID, tierDef.Tier)
	if err != nil {
		return fmt.Errorf("EvaluateApprovalChain: load tier counts: %w", err)
	}
	policy := tierDef.ApprovalPolicy
	if policy == "" {
		var gatePolicy model.ApprovalPolicy
		if err := tx.GetContext(ctx, &gatePolicy, `SELECT approval_policy FROM approval_gates WHERE id = $1::uuid`, chain.ApprovalGateID); err != nil {
			return fmt.Errorf("EvaluateApprovalChain: load gate policy: %w", err)
		}
		policy = gatePolicy
	}

	policyResult := evaluatePolicyCounts(policy, 0, counts)
	if policyResult.Err != nil {
		return fmt.Errorf("EvaluateApprovalChain: policy evaluation: %w", policyResult.Err)
	}

	if policyResult.Failed {
		if _, err := tx.ExecContext(ctx, `
			UPDATE approval_chain_state
			SET tier_status = 'REJECTED',
				tier_completed_at = now(),
				chain_status = 'FAILED',
				updated_at = now()
			WHERE id = $1::uuid
		`, chain.ID); err != nil {
			return fmt.Errorf("EvaluateApprovalChain: mark tier failed: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE approval_gates
			SET gate_status = 'FAILED',
				closed_at = now(),
				updated_at = now(),
				version = version + 1
			WHERE id = $1::uuid
		`, chain.ApprovalGateID); err != nil {
			return fmt.Errorf("EvaluateApprovalChain: close gate failed: %w", err)
		}
		payload, _ := json.Marshal(ApprovalEventPayload{
			GateID:     chain.ApprovalGateID,
			CaseID:     chain.CaseID,
			GateStatus: model.ApprovalGateStatusFailed,
			Tier:       &tierDef.Tier,
			Reason:     "approval_chain_tier_failed",
		})
		caseID := chain.CaseID
		publisher := e.eventPublisher
		if publisher == nil {
			publisher = &SQLXEventPublisher{}
		}
		if err := publisher.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			EventType: model.EventApprovalGateFailed,
			Payload:   payload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("EvaluateApprovalChain: publish tier failed event: %w", err)
		}
		return nil
	}

	if policyResult.Satisfied {
		lastTier := idx == len(tiers)-1
		nextTier := nextTierValue(tiers, idx)
		_, err := tx.ExecContext(ctx, `
			UPDATE approval_chain_state
			SET tier_status = 'APPROVED',
				tier_completed_at = now(),
				current_tier = $2,
				chain_status = CASE WHEN $3 THEN 'COMPLETED' ELSE 'IN_PROGRESS' END,
				updated_at = now()
			WHERE id = $1::uuid
		`, chain.ID, nextTier, lastTier)
		if err != nil {
			return fmt.Errorf("EvaluateApprovalChain: mark tier approved: %w", err)
		}

		if lastTier {
			_, err = tx.ExecContext(ctx, `
				UPDATE approval_gates
				SET gate_status = 'SATISFIED',
					closed_at = now(),
					updated_at = now(),
					version = version + 1
				WHERE id = $1::uuid
			`, chain.ApprovalGateID)
			if err != nil {
				return fmt.Errorf("EvaluateApprovalChain: close gate on final tier: %w", err)
			}
			payload, _ := json.Marshal(ApprovalEventPayload{
				GateID:     chain.ApprovalGateID,
				CaseID:     chain.CaseID,
				GateStatus: model.ApprovalGateStatusSatisfied,
				Tier:       &tierDef.Tier,
				Reason:     "approval_chain_completed",
			})
			caseID := chain.CaseID
			publisher := e.eventPublisher
			if publisher == nil {
				publisher = &SQLXEventPublisher{}
			}
			if err := publisher.PublishEvent(ctx, tx, model.Event{
				CaseID:    &caseID,
				EventType: model.EventApprovalGateSatisfied,
				Payload:   payload,
				Status:    model.EventStatusPending,
			}); err != nil {
				return fmt.Errorf("EvaluateApprovalChain: publish chain completed event: %w", err)
			}
		}
	}

	return nil
}

func loadTierCounts(ctx context.Context, tx *sqlx.Tx, gateID string, tier int) (requestCounts, error) {
	type row struct {
		Status string `db:"status"`
		Count  int    `db:"cnt"`
	}
	var rows []row
	err := tx.SelectContext(ctx, &rows, `
		SELECT status, COUNT(*) AS cnt
		FROM approval_requests
		WHERE approval_gate_id = $1::uuid
		  AND tier = $2
		  AND status <> 'DELEGATED'
		GROUP BY status
	`, gateID, tier)
	if err != nil {
		return requestCounts{}, err
	}
	counts := requestCounts{}
	for _, r := range rows {
		switch r.Status {
		case "PENDING":
			counts.Pending = r.Count
		case "APPROVED":
			counts.Approved = r.Count
		case "REJECTED":
			counts.Rejected = r.Count
		case "EXPIRED":
			counts.Expired = r.Count
		}
		counts.Total += r.Count
	}
	return counts, nil
}

func findTierDefinition(tiers []model.ApprovalChainTierDefinition, tier int) (*model.ApprovalChainTierDefinition, int) {
	for i := range tiers {
		if tiers[i].Tier == tier {
			return &tiers[i], i
		}
	}
	return nil, -1
}

func nextTierValue(tiers []model.ApprovalChainTierDefinition, index int) int {
	if index < 0 || index+1 >= len(tiers) {
		if len(tiers) == 0 {
			return 0
		}
		return tiers[len(tiers)-1].Tier
	}
	return tiers[index+1].Tier
}

// GetGateStatus fetches gate status for external checks.
func GetGateStatus(ctx context.Context, db *sqlx.DB, gateID string) (model.ApprovalGateStatus, error) {
	if db == nil {
		return "", fmt.Errorf("GetGateStatus: db is nil")
	}
	var status sql.NullString
	if err := db.GetContext(ctx, &status, `
		SELECT gate_status
		FROM approval_gates
		WHERE id = $1::uuid
	`, gateID); err != nil {
		return "", fmt.Errorf("GetGateStatus: %w", err)
	}
	if !status.Valid {
		return "", fmt.Errorf("GetGateStatus: empty status")
	}
	return model.ApprovalGateStatus(status.String), nil
}
