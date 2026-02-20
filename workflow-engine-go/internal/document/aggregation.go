package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ApplyAggregationRules updates case.metadata based on task payload fields.
func ApplyAggregationRules(
	ctx context.Context,
	tx *sqlx.Tx,
	caseID string,
	task model.Task,
	rules []model.AggregationRule,
) error {
	if tx == nil {
		return fmt.Errorf("ApplyAggregationRules: tx is nil")
	}
	if strings.TrimSpace(caseID) == "" {
		return fmt.Errorf("ApplyAggregationRules: caseID is required")
	}

	taskInput := map[string]interface{}{}
	if len(task.InputPayload) > 0 {
		if err := json.Unmarshal(task.InputPayload, &taskInput); err != nil {
			return fmt.Errorf("ApplyAggregationRules: parse task input payload: %w", err)
		}
	}
	taskOutput := map[string]interface{}{}
	if len(task.OutputPayload) > 0 {
		if err := json.Unmarshal(task.OutputPayload, &taskOutput); err != nil {
			return fmt.Errorf("ApplyAggregationRules: parse task output payload: %w", err)
		}
	}

	type patch struct {
		path  string
		value interface{}
	}
	patches := make([]patch, 0, len(rules))
	for _, rule := range rules {
		if !rule.OnTaskComplete {
			continue
		}
		if strings.TrimSpace(rule.SourceTask) != strings.TrimSpace(task.TaskDefinitionCode) {
			continue
		}
		target := strings.TrimSpace(rule.TargetField)
		if target == "" {
			continue
		}
		// Unconditional trim prefix per S1017
		target = strings.TrimPrefix(target, "metadata.")
		if target == "" {
			continue
		}

		value, found := resolveAggregationSourceValue(taskInput, taskOutput, rule.SourceField)
		if !found {
			return fmt.Errorf("ApplyAggregationRules: source field not found for rule source_task=%s source_field=%s", rule.SourceTask, rule.SourceField)
		}
		patches = append(patches, patch{
			path:  target,
			value: value,
		})
	}
	if len(patches) == 0 {
		return nil
	}

	expression := "COALESCE(metadata, '{}'::jsonb)"
	args := make([]interface{}, 0, len(patches)+1)
	for _, item := range patches {
		pathLiteral, err := jsonbPathLiteral(item.path)
		if err != nil {
			return fmt.Errorf("ApplyAggregationRules: invalid target path %s: %w", item.path, err)
		}
		valueJSON, err := json.Marshal(item.value)
		if err != nil {
			return fmt.Errorf("ApplyAggregationRules: marshal value for %s: %w", item.path, err)
		}
		args = append(args, string(valueJSON))
		expression = fmt.Sprintf("jsonb_set(%s, %s, $%d::jsonb, true)", expression, pathLiteral, len(args))
	}
	args = append(args, caseID)

	query := fmt.Sprintf(`
		UPDATE cases
		SET metadata = %s,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $%d::uuid
	`, expression, len(args))

	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("ApplyAggregationRules: update case metadata: %w", err)
	}
	return nil
}

func resolveAggregationSourceValue(
	taskInput map[string]interface{},
	taskOutput map[string]interface{},
	sourceField string,
) (interface{}, bool) {
	sourceField = strings.TrimSpace(sourceField)
	if sourceField == "" {
		return nil, false
	}
	switch {
	case strings.HasPrefix(sourceField, "input_payload."):
		return getByPath(taskInput, strings.TrimPrefix(sourceField, "input_payload."))
	case strings.HasPrefix(sourceField, "output_payload."):
		return getByPath(taskOutput, strings.TrimPrefix(sourceField, "output_payload."))
	default:
		if value, ok := getByPath(taskOutput, sourceField); ok {
			return value, true
		}
		return getByPath(taskInput, sourceField)
	}
}
