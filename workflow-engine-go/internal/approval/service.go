package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type CreateGateRequest struct {
	TaskID                 string
	CaseID                 string
	Approval               model.ApprovalDefinition
	ApprovalAmount         *float64
	ApprovalChain          []model.ApprovalChainTierDefinition
	FallbackSupervisorRole string
}

// CreateApprovalGate inserts approval gate snapshot for a task at creation time.
func CreateApprovalGate(ctx context.Context, tx *sqlx.Tx, req CreateGateRequest, publisher EventPublisher) (string, error) {
	if tx == nil {
		return "", fmt.Errorf("CreateApprovalGate: tx is nil")
	}
	if req.TaskID == "" || req.CaseID == "" {
		return "", fmt.Errorf("CreateApprovalGate: task_id and case_id are required")
	}
	if req.Approval.ApprovalPolicy == "" {
		req.Approval.ApprovalPolicy = model.ApprovalPolicySingleApprover
	}
	if req.Approval.ApproverSelection == "" {
		req.Approval.ApproverSelection = model.ApproverSelectionExplicitList
	}
	if req.Approval.RequiredApproverCount <= 0 {
		req.Approval.RequiredApproverCount = 1
	}
	if req.Approval.ApprovalTimeoutHours <= 0 {
		req.Approval.ApprovalTimeoutHours = 24
	}
	if req.Approval.OnTimeoutAction == "" {
		req.Approval.OnTimeoutAction = model.TimeoutActionEscalate
	}
	if req.Approval.RejectionBehavior == "" {
		req.Approval.RejectionBehavior = model.RejectionBehaviorSendToRework
	}
	if req.FallbackSupervisorRole != "" {
		req.Approval.FallbackSupervisorRole = req.FallbackSupervisorRole
	}

	approversJSON, err := json.Marshal(req.Approval.Approvers)
	if err != nil {
		return "", fmt.Errorf("CreateApprovalGate: marshal approvers: %w", err)
	}
	var chainJSON interface{}
	if len(req.ApprovalChain) > 0 {
		chainRaw, err := json.Marshal(req.ApprovalChain)
		if err != nil {
			return "", fmt.Errorf("CreateApprovalGate: marshal chain: %w", err)
		}
		chainJSON = chainRaw
	}

	var gateID string
	err = tx.GetContext(ctx, &gateID, `
		INSERT INTO approval_gates (
			task_id,
			case_id,
			approval_policy,
			required_approver_count,
			approver_selection,
			approvers,
			authority_limit,
			approval_amount,
			approval_timeout_hours,
			on_timeout_action,
			rejection_behavior,
			rework_target_stage_code,
			fallback_supervisor_role,
			dynamic_rule_expression,
			chain_definition,
			gate_status
		)
		VALUES (
			$1::uuid,
			$2::uuid,
			$3,
			$4,
			$5,
			$6::jsonb,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14,
			$15::jsonb,
			'PENDING'
		)
		ON CONFLICT (task_id)
		DO UPDATE SET
			approval_policy = EXCLUDED.approval_policy,
			required_approver_count = EXCLUDED.required_approver_count,
			approver_selection = EXCLUDED.approver_selection,
			approvers = EXCLUDED.approvers,
			authority_limit = EXCLUDED.authority_limit,
			approval_amount = EXCLUDED.approval_amount,
			approval_timeout_hours = EXCLUDED.approval_timeout_hours,
			on_timeout_action = EXCLUDED.on_timeout_action,
			rejection_behavior = EXCLUDED.rejection_behavior,
			rework_target_stage_code = EXCLUDED.rework_target_stage_code,
			fallback_supervisor_role = EXCLUDED.fallback_supervisor_role,
			dynamic_rule_expression = EXCLUDED.dynamic_rule_expression,
			chain_definition = EXCLUDED.chain_definition,
			updated_at = now(),
			version = approval_gates.version + 1
		RETURNING id::text
	`, req.TaskID,
		req.CaseID,
		string(req.Approval.ApprovalPolicy),
		req.Approval.RequiredApproverCount,
		string(req.Approval.ApproverSelection),
		approversJSON,
		req.Approval.AuthorityLimit,
		req.ApprovalAmount,
		req.Approval.ApprovalTimeoutHours,
		string(req.Approval.OnTimeoutAction),
		string(req.Approval.RejectionBehavior),
		req.Approval.ReworkTargetStageCode,
		nullIfEmpty(req.Approval.FallbackSupervisorRole),
		nullIfEmpty(req.Approval.DynamicRule),
		chainJSON,
	)
	if err != nil {
		return "", fmt.Errorf("CreateApprovalGate: insert gate: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET requires_approval = TRUE,
			approval_gate_id = $1::uuid,
			approval_amount = $2,
			updated_at = now(),
			version = version + 1
		WHERE id = $3::uuid
	`, gateID, req.ApprovalAmount, req.TaskID); err != nil {
		return "", fmt.Errorf("CreateApprovalGate: update task: %w", err)
	}

	if len(req.ApprovalChain) > 0 {
		chainRaw, _ := json.Marshal(req.ApprovalChain)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO approval_chain_state (
				case_id,
				approval_gate_id,
				approval_chain_definition,
				current_tier,
				tier_status,
				chain_status,
				tier_started_at
			)
			VALUES (
				$1::uuid,
				$2::uuid,
				$3::jsonb,
				$4,
				'PENDING',
				'PENDING',
				now()
			)
			ON CONFLICT (approval_gate_id) DO NOTHING
		`, req.CaseID, gateID, chainRaw, req.ApprovalChain[0].Tier)
		if err != nil {
			return "", fmt.Errorf("CreateApprovalGate: insert chain state: %w", err)
		}
	}

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	payload, _ := json.Marshal(ApprovalEventPayload{
		GateID:      gateID,
		CaseID:      req.CaseID,
		TaskID:      req.TaskID,
		Policy:      req.Approval.ApprovalPolicy,
		GateStatus:  model.ApprovalGateStatusPending,
		Reason:      "approval_gate_created",
	})
	caseID := req.CaseID
	taskID := req.TaskID
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		TaskID:    &taskID,
		EventType: model.EventApprovalGateCreated,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return "", fmt.Errorf("CreateApprovalGate: publish event: %w", err)
	}

	return gateID, nil
}

type ActivateGateInput struct {
	GateID string
}

// ActivateApprovalGate selects approvers and creates approval_requests.
func ActivateApprovalGate(
	ctx context.Context,
	db *sqlx.DB,
	tx *sqlx.Tx,
	input ActivateGateInput,
	publisher EventPublisher,
) error {
	if db == nil {
		return fmt.Errorf("ActivateApprovalGate: db is nil")
	}
	if tx == nil {
		return fmt.Errorf("ActivateApprovalGate: tx is nil")
	}

	var gate model.ApprovalGate
	if err := tx.GetContext(ctx, &gate, `
		SELECT
			id::text AS id,
			task_id::text AS task_id,
			case_id::text AS case_id,
			approval_policy,
			required_approver_count,
			approver_selection,
			approvers,
			authority_limit,
			approval_amount,
			approval_timeout_hours,
			on_timeout_action,
			rejection_behavior,
			rework_target_stage_code,
			fallback_supervisor_role,
			dynamic_rule_expression,
			chain_definition,
			gate_status,
			opened_at,
			closed_at,
			version,
			created_at,
			updated_at
		FROM approval_gates
		WHERE id = $1::uuid
		FOR UPDATE
	`, input.GateID); err != nil {
		return fmt.Errorf("ActivateApprovalGate: load gate: %w", err)
	}
	if gate.GateStatus == model.ApprovalGateStatusSatisfied || gate.GateStatus == model.ApprovalGateStatusFailed {
		return nil
	}

	var existingPending int
	if err := tx.GetContext(ctx, &existingPending, `
		SELECT COUNT(*)
		FROM approval_requests
		WHERE approval_gate_id = $1::uuid
		  AND status = 'PENDING'
	`, gate.ID); err != nil {
		return fmt.Errorf("ActivateApprovalGate: count existing requests: %w", err)
	}
	if existingPending > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE approval_gates
			SET gate_status = 'ACTIVE',
				opened_at = COALESCE(opened_at, now()),
				updated_at = now(),
				version = version + 1
			WHERE id = $1::uuid
		`, gate.ID); err != nil {
			return fmt.Errorf("ActivateApprovalGate: set active existing requests: %w", err)
		}
		return nil
	}

	var caseData model.CaseInstance
	if err := tx.GetContext(ctx, &caseData, `
		SELECT
			id::text AS id,
			reference_number,
			case_type_id::text AS case_type_id,
			case_type_version,
			parent_case_id::text AS parent_case_id,
			source_case_id::text AS source_case_id,
			current_stage_code,
			current_stage_ordinal,
			status,
			metadata,
			assigned_to,
			row_version,
			created_at,
			updated_at,
			completed_at,
			suspend_reason,
			resume_at,
			withdrawal_reason,
			emergency_closed_at,
			emergency_reason,
			supervisor_id
		FROM cases
		WHERE id = $1::uuid
	`, gate.CaseID); err != nil {
		return fmt.Errorf("ActivateApprovalGate: load case: %w", err)
	}

	approverIDs, err := SelectApprovers(ctx, db, gate, caseData)
	if err != nil {
		if err == model.ErrNoEligibleApprover {
			if gate.FallbackSupervisorRole != nil && strings.TrimSpace(*gate.FallbackSupervisorRole) != "" {
				var fallbackUser string
				fallbackErr := tx.GetContext(ctx, &fallbackUser, `
					SELECT id
					FROM users
					WHERE role_code = $1
					  AND status = 'ACTIVE'
					ORDER BY created_at ASC
					LIMIT 1
				`, strings.TrimSpace(*gate.FallbackSupervisorRole))
				if fallbackErr == nil && fallbackUser != "" {
					approverIDs = []string{fallbackUser}
					err = nil
				}
			}
		}
		if err != nil {
			if err == model.ErrNoEligibleApprover {
				if publisher == nil {
					publisher = &SQLXEventPublisher{}
				}
				payload, _ := json.Marshal(ApprovalEventPayload{
					GateID: gate.ID,
					CaseID: gate.CaseID,
					TaskID: gate.TaskID,
					Reason: "no_eligible_approver",
				})
				caseID := gate.CaseID
				taskID := gate.TaskID
				if pubErr := publisher.PublishEvent(ctx, tx, model.Event{
					CaseID:    &caseID,
					TaskID:    &taskID,
					EventType: model.EventNoEligibleApprover,
					Payload:   payload,
					Status:    model.EventStatusPending,
				}); pubErr != nil {
					return fmt.Errorf("ActivateApprovalGate: publish NO_ELIGIBLE_APPROVER: %w", pubErr)
				}
				return model.ErrNoEligibleApprover
			}
			return fmt.Errorf("ActivateApprovalGate: select approvers: %w", err)
		}
	}
	if len(approverIDs) == 0 {
		return model.ErrNoEligibleApprover
	}

	var calendarID sql.NullString
	if err := tx.GetContext(ctx, &calendarID, `
		SELECT case_sla_calendar_id::text
		FROM cases
		WHERE id = $1::uuid
	`, gate.CaseID); err != nil {
		return fmt.Errorf("ActivateApprovalGate: load calendar id: %w", err)
	}
	timeoutDuration := time.Duration(gate.ApprovalTimeoutHours * float64(time.Hour))
	if timeoutDuration <= 0 {
		timeoutDuration = 24 * time.Hour
	}
	baseNow := time.Now().UTC()
	expiresAt := baseNow.Add(timeoutDuration)
	if calendarID.Valid && calendarID.String != "" {
		expiresAt, err = sla.AddBusinessHours(ctx, db, baseNow, timeoutDuration, calendarID.String)
		if err != nil {
			return fmt.Errorf("ActivateApprovalGate: compute business expiry: %w", err)
		}
	}

	for _, approverID := range approverIDs {
		var requestID string
		var tier interface{}
		if len(gate.ChainDefinition) > 0 {
			var tiers []model.ApprovalChainTierDefinition
			if err := json.Unmarshal(gate.ChainDefinition, &tiers); err == nil && len(tiers) > 0 {
				tier = tiers[0].Tier
			}
		}
		err := tx.GetContext(ctx, &requestID, `
			INSERT INTO approval_requests (
				approval_gate_id,
				approver_id,
				tier,
				status,
				evidence_refs,
				expires_at,
				delegation_chain
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				'PENDING',
				'[]'::jsonb,
				$4,
				'[]'::jsonb
			)
			RETURNING id::text
		`, gate.ID, approverID, tier, expiresAt)
		if err != nil {
			return fmt.Errorf("ActivateApprovalGate: insert request for approver %s: %w", approverID, err)
		}

		if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
			RequestID:     requestID,
			EventType:     model.ApprovalAuditEventRequested,
			ActorID:       "SYSTEM",
			PreviousState: model.ApprovalRequestStatusPending,
			NewState:      model.ApprovalRequestStatusPending,
		}); err != nil {
			return fmt.Errorf("ActivateApprovalGate: insert audit requested: %w", err)
		}

		if publisher == nil {
			publisher = &SQLXEventPublisher{}
		}
		payload, _ := json.Marshal(ApprovalEventPayload{
			GateID:        gate.ID,
			RequestID:     requestID,
			CaseID:        gate.CaseID,
			TaskID:        gate.TaskID,
			ApproverID:    approverID,
			RequestStatus: model.ApprovalRequestStatusPending,
			Reason:        "approval_requested",
		})
		caseID := gate.CaseID
		taskID := gate.TaskID
		if err := publisher.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			TaskID:    &taskID,
			EventType: model.EventApprovalRequested,
			Payload:   payload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("ActivateApprovalGate: publish APPROVAL_REQUESTED: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE approval_gates
		SET gate_status = 'ACTIVE',
			opened_at = COALESCE(opened_at, now()),
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
	`, gate.ID); err != nil {
		return fmt.Errorf("ActivateApprovalGate: set gate active: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = CASE WHEN status = 'IN_PROGRESS' THEN 'AWAITING_EXTERNAL' ELSE status END,
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
	`, gate.TaskID); err != nil {
		return fmt.Errorf("ActivateApprovalGate: move task awaiting external: %w", err)
	}

	return nil
}

type DecideRequestInput struct {
	RequestID    string
	Actor        Actor
	DecisionText string
	EvidenceRefs []string
}

type decisionRequestSnapshot struct {
	ID                    string                      `db:"id"`
	ApprovalGateID        string                      `db:"approval_gate_id"`
	ApproverID            string                      `db:"approver_id"`
	DelegatedToID         *string                     `db:"delegated_to_id"`
	Status                model.ApprovalRequestStatus `db:"status"`
	CaseID                string                      `db:"case_id"`
	TaskID                string                      `db:"task_id"`
	RejectionBehavior     model.RejectionBehavior     `db:"rejection_behavior"`
	ReworkTargetStageCode *string                     `db:"rework_target_stage_code"`
}

type DelegateRequestInput struct {
	RequestID     string
	Actor         Actor
	DelegatedToID string
	Reason        string
}

func ApproveRequest(ctx context.Context, db *sqlx.DB, input DecideRequestInput, publisher EventPublisher, evaluator *ApprovalPolicyEvaluator) error {
	return decideRequest(ctx, db, input, model.ApprovalRequestStatusApproved, publisher, evaluator)
}

func RejectRequest(ctx context.Context, db *sqlx.DB, input DecideRequestInput, publisher EventPublisher, evaluator *ApprovalPolicyEvaluator) error {
	return decideRequest(ctx, db, input, model.ApprovalRequestStatusRejected, publisher, evaluator)
}

func decideRequest(
	ctx context.Context,
	db *sqlx.DB,
	input DecideRequestInput,
	target model.ApprovalRequestStatus,
	publisher EventPublisher,
	evaluator *ApprovalPolicyEvaluator,
) error {
	if db == nil {
		return fmt.Errorf("decideRequest: db is nil")
	}
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("decideRequest: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	if evaluator == nil {
		evaluator = NewApprovalPolicyEvaluator(db, nil, publisher)
	}

	var snap decisionRequestSnapshot
	if err := tx.GetContext(ctx, &snap, `
		SELECT
			r.id::text AS id,
			r.approval_gate_id::text AS approval_gate_id,
			r.approver_id,
			r.delegated_to_id,
			r.status,
			g.case_id::text AS case_id,
			g.task_id::text AS task_id,
			g.rejection_behavior,
			g.rework_target_stage_code
		FROM approval_requests r
		JOIN approval_gates g ON g.id = r.approval_gate_id
		WHERE r.id = $1::uuid
		FOR UPDATE
	`, input.RequestID); err != nil {
		return fmt.Errorf("decideRequest: load request: %w", err)
	}

	actor := input.Actor
	actor.DecisionText = input.DecisionText
	actor.EvidenceRefs = input.EvidenceRefs
	if snap.Status == model.ApprovalRequestStatusDelegated {
		actor.IsDelegate = actor.ID == valueOrEmpty(snap.DelegatedToID)
		actor.IsOriginalApprover = actor.ID == snap.ApproverID
	}
	if err := ValidateApprovalTransition(ctx, snap.Status, target, actor); err != nil {
		return fmt.Errorf("decideRequest: %w", err)
	}

	evidenceJSON, err := json.Marshal(input.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("decideRequest: marshal evidence: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = $2,
			decision = $3,
			evidence_refs = $4::jsonb,
			decided_at = now(),
			decided_by = $5,
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
	`, input.RequestID, string(target), input.DecisionText, evidenceJSON, actor.ID)
	if err != nil {
		return fmt.Errorf("decideRequest: update request: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("decideRequest: rows affected: %w", err)
	}
	if affected == 0 {
		return nil
	}

	auditEvent := model.ApprovalAuditEventApproved
	eventType := model.EventApprovalGranted
	if target == model.ApprovalRequestStatusRejected {
		auditEvent = model.ApprovalAuditEventRejected
		eventType = model.EventApprovalRejected
	}
	if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
		RequestID:     input.RequestID,
		EventType:     auditEvent,
		ActorID:       actor.ID,
		DecisionText:  &input.DecisionText,
		PreviousState: snap.Status,
		NewState:      target,
	}); err != nil {
		return fmt.Errorf("decideRequest: insert audit: %w", err)
	}

	payload, _ := json.Marshal(ApprovalEventPayload{
		GateID:        snap.ApprovalGateID,
		RequestID:     input.RequestID,
		CaseID:        snap.CaseID,
		TaskID:        snap.TaskID,
		ApproverID:    actor.ID,
		RequestStatus: target,
		DecisionText:  input.DecisionText,
	})
	caseID := snap.CaseID
	taskID := snap.TaskID
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		TaskID:    &taskID,
		EventType: eventType,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("decideRequest: publish event: %w", err)
	}

	satisfied, err := evaluator.EvaluateApprovalPolicy(ctx, tx, snap.ApprovalGateID)
	if err != nil {
		return fmt.Errorf("decideRequest: evaluate policy: %w", err)
	}

	var chainID string
	chainErr := tx.GetContext(ctx, &chainID, `
		SELECT id::text
		FROM approval_chain_state
		WHERE approval_gate_id = $1::uuid
	`, snap.ApprovalGateID)
	if chainErr == nil {
		if err := evaluator.EvaluateApprovalChain(ctx, tx, chainID); err != nil {
			return fmt.Errorf("decideRequest: evaluate approval chain: %w", err)
		}
	} else if chainErr != sql.ErrNoRows {
		return fmt.Errorf("decideRequest: load approval chain state: %w", chainErr)
	}

	if target == model.ApprovalRequestStatusRejected && !satisfied {
		if err := executeRejectionBehavior(ctx, tx, snap, input.DecisionText, actor.ID, publisher); err != nil {
			return fmt.Errorf("decideRequest: rejection behavior: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("decideRequest: commit: %w", err)
	}
	return nil
}

func DelegateRequest(ctx context.Context, db *sqlx.DB, input DelegateRequestInput, publisher EventPublisher) error {
	if db == nil {
		return fmt.Errorf("DelegateRequest: db is nil")
	}
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("DelegateRequest: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	type requestSnapshot struct {
		ID             string `db:"id"`
		ApprovalGateID string `db:"approval_gate_id"`
		ApproverID     string `db:"approver_id"`
		Status         model.ApprovalRequestStatus `db:"status"`
		CaseID         string `db:"case_id"`
		TaskID         string `db:"task_id"`
		ExpiresAt      time.Time `db:"expires_at"`
		Tier           sql.NullInt64 `db:"tier"`
		DelegationChain json.RawMessage `db:"delegation_chain"`
	}
	var snap requestSnapshot
	if err := tx.GetContext(ctx, &snap, `
		SELECT
			r.id::text AS id,
			r.approval_gate_id::text AS approval_gate_id,
			r.approver_id,
			r.status,
			g.case_id::text AS case_id,
			g.task_id::text AS task_id,
			r.expires_at,
			r.tier,
			r.delegation_chain
		FROM approval_requests r
		JOIN approval_gates g ON g.id = r.approval_gate_id
		WHERE r.id = $1::uuid
		FOR UPDATE
	`, input.RequestID); err != nil {
		return fmt.Errorf("DelegateRequest: load request: %w", err)
	}

	actor := input.Actor
	actor.DelegatedToID = &input.DelegatedToID
	if err := ValidateApprovalTransition(ctx, snap.Status, model.ApprovalRequestStatusDelegated, actor); err != nil {
		return fmt.Errorf("DelegateRequest: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = 'DELEGATED',
			delegated_to_id = $2,
			delegated_at = now(),
			decided_at = now(),
			decided_by = $3,
			decision = $4,
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
	`, input.RequestID, input.DelegatedToID, actor.ID, input.Reason)
	if err != nil {
		return fmt.Errorf("DelegateRequest: update delegated status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("DelegateRequest: rows affected: %w", err)
	}
	if affected == 0 {
		return nil
	}

	chain := make([]map[string]interface{}, 0)
	if len(snap.DelegationChain) > 0 {
		_ = json.Unmarshal(snap.DelegationChain, &chain)
	}
	chain = append(chain, map[string]interface{}{
		"from": snap.ApproverID,
		"to":   input.DelegatedToID,
		"by":   actor.ID,
		"at":   time.Now().UTC(),
	})
	chainRaw, _ := json.Marshal(chain)

	var tier interface{}
	if snap.Tier.Valid {
		tier = int(snap.Tier.Int64)
	}
	var newRequestID string
	if err := tx.GetContext(ctx, &newRequestID, `
		INSERT INTO approval_requests (
			approval_gate_id,
			approver_id,
			tier,
			status,
			evidence_refs,
			expires_at,
			delegation_chain
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			'PENDING',
			'[]'::jsonb,
			$4,
			$5::jsonb
		)
		RETURNING id::text
	`, snap.ApprovalGateID, input.DelegatedToID, tier, snap.ExpiresAt, chainRaw); err != nil {
		return fmt.Errorf("DelegateRequest: insert delegated request: %w", err)
	}

	if err := insertApprovalAuditLog(ctx, tx, approvalAuditInput{
		RequestID:     input.RequestID,
		EventType:     model.ApprovalAuditEventDelegated,
		ActorID:       actor.ID,
		DecisionText:  &input.Reason,
		PreviousState: snap.Status,
		NewState:      model.ApprovalRequestStatusDelegated,
	}); err != nil {
		return fmt.Errorf("DelegateRequest: audit delegated: %w", err)
	}

	payload, _ := json.Marshal(ApprovalEventPayload{
		GateID:        snap.ApprovalGateID,
		RequestID:     newRequestID,
		CaseID:        snap.CaseID,
		TaskID:        snap.TaskID,
		ApproverID:    snap.ApproverID,
		DelegatedToID: input.DelegatedToID,
		RequestStatus: model.ApprovalRequestStatusDelegated,
		Reason:        input.Reason,
	})
	caseID := snap.CaseID
	taskID := snap.TaskID
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		TaskID:    &taskID,
		EventType: model.EventApprovalDelegated,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("DelegateRequest: publish APPROVAL_DELEGATED: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DelegateRequest: commit: %w", err)
	}
	return nil
}

func executeRejectionBehavior(
	ctx context.Context,
	tx *sqlx.Tx,
	snap decisionRequestSnapshot,
	decisionText string,
	actorID string,
	publisher EventPublisher,
) error {
	switch snap.RejectionBehavior {
	case model.RejectionBehaviorSendToRework:
		if snap.ReworkTargetStageCode == nil || *snap.ReworkTargetStageCode == "" {
			return fmt.Errorf("executeRejectionBehavior: rework_target_stage_code is required")
		}
		return sendCaseToRework(ctx, tx, snap.CaseID, snap.ApprovalGateID, *snap.ReworkTargetStageCode, decisionText, actorID, publisher)

	case model.RejectionBehaviorTerminalFail:
		return rejectCaseTerminally(ctx, tx, snap.CaseID, snap.ApprovalGateID, decisionText, actorID, publisher)
	}
	return nil
}

func sendCaseToRework(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	gateID string,
	reworkTargetStage string,
	reason string,
	rejectedBy string,
	publisher EventPublisher,
) error {
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	var currentStage sql.NullString
	var currentOrdinal int
	var rowVersion int
	var reworkCount int
	var maxReworkAttempts int
	if err := tx.QueryRowxContext(ctx, `
		SELECT current_stage_code, current_stage_ordinal, row_version, rework_count, max_rework_attempts
		FROM cases
		WHERE id = $1::uuid
		FOR UPDATE
	`, caseID).Scan(&currentStage, &currentOrdinal, &rowVersion, &reworkCount, &maxReworkAttempts); err != nil {
		return fmt.Errorf("sendCaseToRework: load case: %w", err)
	}
	currentStageCode := currentStage.String

	newReworkCount := reworkCount + 1
	if maxReworkAttempts > 0 && newReworkCount > maxReworkAttempts {
		if _, err := tx.ExecContext(ctx, `
			UPDATE cases
			SET status = 'REJECTED',
				completed_at = now(),
				rework_count = $2,
				updated_at = now(),
				row_version = row_version + 1
			WHERE id = $1::uuid
		`, caseID, newReworkCount); err != nil {
			return fmt.Errorf("sendCaseToRework: set max rework rejected: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'CANCELLED',
				completed_at = now(),
				error_detail = jsonb_set(COALESCE(error_detail, '{}'::jsonb), '{reason}', to_jsonb('CASE_MAX_REWORK_EXCEEDED'::text)),
				updated_at = now(),
				version = version + 1
			WHERE case_id = $1::uuid
			  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'FAILED')
		`, caseID); err != nil {
			return fmt.Errorf("sendCaseToRework: cancel tasks on max rework: %w", err)
		}

		payload, _ := json.Marshal(ApprovalEventPayload{
			CaseID: caseID,
			GateID: gateID,
			Reason: "case_max_rework_exceeded",
		})
		if err := publisher.PublishEvent(ctx, tx, model.Event{
			CaseID:    &caseID,
			EventType: model.EventCaseMaxReworkExceeded,
			Payload:   payload,
			Status:    model.EventStatusPending,
		}); err != nil {
			return fmt.Errorf("sendCaseToRework: publish CASE_MAX_REWORK_EXCEEDED: %w", err)
		}
		return nil
	}

	var toOrdinal int
	if err := tx.GetContext(ctx, &toOrdinal, `
		SELECT (s->>'sequence_order')::int AS ordinal
		FROM case_types ct,
			 jsonb_array_elements((ct.config->'stages')) s
		WHERE ct.id = (SELECT case_type_id FROM cases WHERE id = $1::uuid)
		  AND s->>'code' = $2
		LIMIT 1
	`, caseID, reworkTargetStage); err != nil {
		return fmt.Errorf("sendCaseToRework: resolve rework stage ordinal: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET current_stage_code = $2,
			current_stage_ordinal = $3,
			rework_count = $4,
			status = 'IN_PROGRESS',
			updated_at = now(),
			row_version = row_version + 1
		WHERE id = $1::uuid
		  AND row_version = $5
	`, caseID, reworkTargetStage, toOrdinal, newReworkCount, rowVersion); err != nil {
		return fmt.Errorf("sendCaseToRework: update case stage: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO case_stage_transitions (
			case_id,
			from_stage_code,
			from_stage_ordinal,
			to_stage_code,
			to_stage_ordinal,
			is_regression,
			regression_reason,
			triggered_by
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4,
			$5,
			TRUE,
			$6,
			$7
		)
	`, caseID, nullIfEmpty(currentStageCode), currentOrdinal, reworkTargetStage, toOrdinal, reason, rejectedBy); err != nil {
		return fmt.Errorf("sendCaseToRework: insert stage transition: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'CANCELLED',
			completed_at = now(),
			error_detail = jsonb_set(COALESCE(error_detail, '{}'::jsonb), '{reason}', to_jsonb('APPROVAL_REJECTED'::text)),
			updated_at = now(),
			version = version + 1
		WHERE case_id = $1::uuid
		  AND stage_code = $2
		  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'FAILED')
	`, caseID, currentStageCode); err != nil {
		return fmt.Errorf("sendCaseToRework: cancel stage tasks: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE approval_gates
		SET gate_status = 'REJECTED_REWORK_INITIATED',
			closed_at = now(),
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
	`, gateID); err != nil {
		return fmt.Errorf("sendCaseToRework: mark gate status: %w", err)
	}

	stagePayload, _ := json.Marshal(map[string]interface{}{
		"case_id":        caseID,
		"from_stage":     currentStageCode,
		"to_stage":       reworkTargetStage,
		"to_stage_order": toOrdinal,
	})
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		EventType: model.EventCaseStageChanged,
		Payload:   stagePayload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("sendCaseToRework: publish CASE_STAGE_CHANGED: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":          caseID,
		"from_stage":       currentStageCode,
		"to_stage":         reworkTargetStage,
		"rejection_reason": reason,
		"rejected_by":      rejectedBy,
		"rejected_at":      time.Now().UTC(),
	})
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		EventType: model.EventCaseSentToRework,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("sendCaseToRework: publish CASE_SENT_TO_REWORK: %w", err)
	}

	return nil
}

func rejectCaseTerminally(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	gateID string,
	reason string,
	rejectedBy string,
	publisher EventPublisher,
) error {
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE cases
		SET status = 'REJECTED',
			completed_at = now(),
			updated_at = now(),
			row_version = row_version + 1
		WHERE id = $1::uuid
	`, caseID); err != nil {
		return fmt.Errorf("rejectCaseTerminally: update case: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'CANCELLED',
			completed_at = now(),
			error_detail = jsonb_set(COALESCE(error_detail, '{}'::jsonb), '{reason}', to_jsonb('CASE_REJECTED'::text)),
			updated_at = now(),
			version = version + 1
		WHERE case_id = $1::uuid
		  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'FAILED')
	`, caseID); err != nil {
		return fmt.Errorf("rejectCaseTerminally: cancel tasks: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE approval_gates
		SET gate_status = 'FAILED',
			closed_at = now(),
			updated_at = now(),
			version = version + 1
		WHERE id = $1::uuid
	`, gateID); err != nil {
		return fmt.Errorf("rejectCaseTerminally: close gate: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"case_id":     caseID,
		"rejected_by": rejectedBy,
		"reason":      reason,
		"source":      "APPROVAL_REJECTED",
	})
	if err := publisher.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		EventType: model.EventCaseRejected,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return fmt.Errorf("rejectCaseTerminally: publish CASE_REJECTED: %w", err)
	}

	return nil
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func nullIfEmpty(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}
