package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

func loadCaseMetadataForApproval(ctx context.Context, tx repository.DBExecutor, caseID string) (map[string]interface{}, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, `
		SELECT metadata
		FROM cases
		WHERE id = $1::uuid
	`, caseID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("loadCaseMetadataForApproval: %w", err)
	}

	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}

	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("loadCaseMetadataForApproval: parse metadata: %w", err)
	}
	return out, nil
}

func resolveApprovalAmountForTask(taskDef model.TaskDefinitionV2, metadata map[string]interface{}) (*float64, error) {
	if !taskDef.RequiresApproval || taskDef.Approval == nil {
		return nil, nil
	}
	field := strings.TrimSpace(taskDef.Approval.ApprovalAmountField)
	if field == "" {
		return nil, nil
	}
	value, ok := metadataLookup(metadata, field)
	if !ok {
		return nil, nil
	}
	amount, ok := coerceFloat(value)
	if !ok {
		return nil, fmt.Errorf("resolveApprovalAmountForTask: field %s is not numeric", field)
	}
	return &amount, nil
}

func metadataLookup(metadata map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = metadata
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		next, ok := m[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func coerceFloat(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func createApprovalGateForTask(
	ctx context.Context,
	tx repository.DBExecutor,
	repo *repository.Repository,
	caseID string,
	taskID string,
	taskDef model.TaskDefinitionV2,
	config model.CaseTypeConfig,
	approvalAmount *float64,
) (string, error) {
	if taskDef.Approval == nil {
		return "", fmt.Errorf("createApprovalGateForTask: approval definition is nil for task %s", taskDef.Code)
	}

	def := *taskDef.Approval
	if def.ApprovalPolicy == "" {
		def.ApprovalPolicy = model.ApprovalPolicySingleApprover
	}
	if def.ApproverSelection == "" {
		def.ApproverSelection = model.ApproverSelectionExplicitList
	}
	if def.RequiredApproverCount <= 0 {
		def.RequiredApproverCount = 1
	}
	if def.ApprovalTimeoutHours <= 0 {
		def.ApprovalTimeoutHours = 24
	}
	if def.OnTimeoutAction == "" {
		def.OnTimeoutAction = model.TimeoutActionEscalate
	}
	if def.RejectionBehavior == "" {
		def.RejectionBehavior = model.RejectionBehaviorSendToRework
	}
	if strings.TrimSpace(def.FallbackSupervisorRole) == "" {
		def.FallbackSupervisorRole = strings.TrimSpace(config.FallbackSupervisorRole)
	}

	approversJSON, err := json.Marshal(def.Approvers)
	if err != nil {
		return "", fmt.Errorf("createApprovalGateForTask: marshal approvers: %w", err)
	}

	var chainJSON interface{}
	if len(config.ApprovalChain) > 0 {
		raw, err := json.Marshal(config.ApprovalChain)
		if err != nil {
			return "", fmt.Errorf("createApprovalGateForTask: marshal approval chain: %w", err)
		}
		chainJSON = raw
	}

	var gateID string
	err = tx.QueryRow(ctx, `
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
	`, taskID, caseID, string(def.ApprovalPolicy), def.RequiredApproverCount, string(def.ApproverSelection), approversJSON,
		def.AuthorityLimit, approvalAmount, def.ApprovalTimeoutHours, string(def.OnTimeoutAction), string(def.RejectionBehavior),
		def.ReworkTargetStageCode, nullIfEmptyString(def.FallbackSupervisorRole), nullIfEmptyString(def.DynamicRule), chainJSON,
	).Scan(&gateID)
	if err != nil {
		return "", fmt.Errorf("createApprovalGateForTask: insert gate: %w", err)
	}

	if len(config.ApprovalChain) > 0 {
		firstTier := config.ApprovalChain[0].Tier
		if _, err := tx.Exec(ctx, `
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
		`, caseID, gateID, chainJSON, firstTier); err != nil {
			return "", fmt.Errorf("createApprovalGateForTask: insert approval_chain_state: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE tasks
		SET requires_approval = TRUE,
		    approval_gate_id = $1::uuid,
		    approval_amount = $2,
		    updated_at = now(),
		    version = version + 1
		WHERE id = $3::uuid
	`, gateID, approvalAmount, taskID); err != nil {
		return "", fmt.Errorf("createApprovalGateForTask: update task: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"gate_id":      gateID,
		"case_id":      caseID,
		"task_id":      taskID,
		"policy":       string(def.ApprovalPolicy),
		"gate_status":  string(model.ApprovalGateStatusPending),
		"event_reason": "approval_gate_created",
	})
	if err := repo.PublishEvent(ctx, tx, model.Event{
		CaseID:    &caseID,
		TaskID:    &taskID,
		EventType: model.EventApprovalGateCreated,
		Payload:   payload,
		Status:    model.EventStatusPending,
	}); err != nil {
		return "", fmt.Errorf("createApprovalGateForTask: publish APPROVAL_GATE_CREATED: %w", err)
	}

	return gateID, nil
}

func nullIfEmptyString(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}
