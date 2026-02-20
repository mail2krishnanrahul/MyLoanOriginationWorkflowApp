package versioning

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"workflow-engine/pkg/model"
)

// ValidateCaseTypeConfig validates structural consistency before activation.
// It accumulates all violations and never short-circuits.
func ValidateCaseTypeConfig(
	ctx context.Context,
	config CaseTypeConfig,
) error {
	_ = ctx
	result := &ValidationResult{Violations: make([]ValidationViolation, 0)}

	if len(config.Stages) == 0 {
		result.Add("stages", "at least one stage must be defined")
		return result
	}

	stageCodeByOrdinal := make(map[int]string, len(config.Stages))
	stageCodeSet := make(map[string]struct{}, len(config.Stages))
	minOrdinal := 0
	maxOrdinal := 0

	taskCodeLocations := make(map[string]string)
	compensatingGraph := make(map[string]string)

	for i, stage := range config.Stages {
		stagePath := fmt.Sprintf("stages[%d]", i)
		stageCode := strings.TrimSpace(stage.Code)
		if stageCode == "" {
			result.Add(stagePath+".code", "stage code is required")
		} else {
			if _, exists := stageCodeSet[stageCode]; exists {
				result.Add(stagePath+".code", fmt.Sprintf("duplicate stage code %s", stageCode))
			} else {
				stageCodeSet[stageCode] = struct{}{}
			}
		}

		if stage.SequenceOrder <= 0 {
			result.Add(stagePath+".sequence_order", "stage ordinal must be greater than 0")
		} else {
			if existingCode, exists := stageCodeByOrdinal[stage.SequenceOrder]; exists {
				result.Add(stagePath+".sequence_order", fmt.Sprintf("ordinal %d is already used by stage %s", stage.SequenceOrder, existingCode))
			} else {
				stageCodeByOrdinal[stage.SequenceOrder] = stageCode
			}
			if minOrdinal == 0 || stage.SequenceOrder < minOrdinal {
				minOrdinal = stage.SequenceOrder
			}
			if stage.SequenceOrder > maxOrdinal {
				maxOrdinal = stage.SequenceOrder
			}
		}

		if len(stage.Activities) == 0 {
			result.Add(stagePath+".activities", "each stage must include at least one activity")
		}

		activityCodes := make(map[string]struct{}, len(stage.Activities))
		for j, activity := range stage.Activities {
			activityPath := fmt.Sprintf("%s.activities[%d]", stagePath, j)
			activityCode := strings.TrimSpace(activity.Code)
			if activityCode == "" {
				result.Add(activityPath+".code", "activity code is required")
			} else {
				if _, exists := activityCodes[activityCode]; exists {
					result.Add(activityPath+".code", fmt.Sprintf("duplicate activity code %s within stage %s", activityCode, stageCode))
				} else {
					activityCodes[activityCode] = struct{}{}
				}
			}

			for k, taskDef := range activity.TaskDefs {
				taskPath := fmt.Sprintf("%s.task_definitions[%d]", activityPath, k)
				taskCode := strings.TrimSpace(taskDef.Code)
				if taskCode == "" {
					result.Add(taskPath+".code", "task definition code is required")
				} else {
					if existingLoc, exists := taskCodeLocations[taskCode]; exists {
						result.Add(taskPath+".code", fmt.Sprintf("duplicate task definition code %s already used at %s", taskCode, existingLoc))
					} else {
						taskCodeLocations[taskCode] = taskPath
					}
				}

				assignedService := extractAssignedService(taskDef)
				if assignedService == "" {
					result.Add(taskPath+".assigned_service", "assigned_service is required and must be non-empty")
				}

				validateRetryPolicy(result, taskPath, taskDef)

				if compCode := strings.TrimSpace(taskDef.CompensatingTaskCode); compCode != "" && taskCode != "" {
					compensatingGraph[taskCode] = compCode
				}

				for _, referencedStageCode := range extractReferencedStageCodes(taskDef) {
					if _, ok := stageCodeSet[referencedStageCode]; !ok {
						result.Add(taskPath+".stage_code", fmt.Sprintf("referenced stage %s is not defined", referencedStageCode))
					}
				}
			}
		}
	}

	if minOrdinal != 1 {
		result.Add("stages", fmt.Sprintf("stage ordinals must start at 1 (found %d)", minOrdinal))
	}
	if maxOrdinal != len(stageCodeByOrdinal) {
		result.Add("stages", fmt.Sprintf("stage ordinals must be contiguous from 1..%d", len(stageCodeByOrdinal)))
	}
	for expected := 1; expected <= len(stageCodeByOrdinal); expected++ {
		if _, ok := stageCodeByOrdinal[expected]; !ok {
			result.Add("stages", fmt.Sprintf("missing stage ordinal %d", expected))
		}
	}

	for taskCode, compCode := range compensatingGraph {
		if _, ok := taskCodeLocations[compCode]; !ok {
			result.Add("task_definitions", fmt.Sprintf("task %s references unknown compensating_task_code %s", taskCode, compCode))
		}
	}

	validateCompensatingCycle(result, compensatingGraph)

	if result.HasViolations() {
		slog.Warn("case_type config validation failed", "violations", len(result.Violations))
		return result
	}

	return nil
}

func validateRetryPolicy(result *ValidationResult, taskPath string, taskDef TaskDefinition) {
	if taskDef.RetryPolicy == nil {
		result.Add(taskPath+".retry_policy", "retry_policy is required")
		return
	}
	if taskDef.RetryPolicy.MaxRetries < 0 {
		result.Add(taskPath+".retry_policy.max_retries", "max_retries must be greater than or equal to 0")
	}
	if taskDef.RetryPolicy.BaseIntervalSeconds <= 0 {
		result.Add(taskPath+".retry_policy.base_interval_seconds", "base_interval_seconds must be greater than 0")
	}
	if taskDef.RetryPolicy.MaxIntervalSeconds <= 0 {
		result.Add(taskPath+".retry_policy.max_interval_seconds", "max_interval_seconds must be greater than 0")
	}
	if taskDef.RetryPolicy.MaxIntervalSeconds > 0 &&
		taskDef.RetryPolicy.BaseIntervalSeconds > 0 &&
		taskDef.RetryPolicy.MaxIntervalSeconds < taskDef.RetryPolicy.BaseIntervalSeconds {
		result.Add(taskPath+".retry_policy.max_interval_seconds", "max_interval_seconds must be greater than or equal to base_interval_seconds")
	}
	if taskDef.RetryPolicy.BackoffStrategy != "" {
		switch taskDef.RetryPolicy.BackoffStrategy {
		case model.RetryBackoffStrategyFixed,
			model.RetryBackoffStrategyLinear,
			model.RetryBackoffStrategyExponential:
			// valid
		default:
			result.Add(taskPath+".retry_policy.backoff_strategy", "backoff_strategy must be FIXED, LINEAR, or EXPONENTIAL")
		}
	}
}

func extractAssignedService(taskDef TaskDefinition) string {
	if len(taskDef.Config) == 0 {
		return ""
	}

	var config map[string]interface{}
	if err := json.Unmarshal(taskDef.Config, &config); err != nil {
		return ""
	}

	if value := stringFromMap(config, "assigned_service"); value != "" {
		return value
	}
	if value := stringFromMap(config, "service"); value != "" {
		return value
	}

	if integrationRaw, ok := config["integration"].(map[string]interface{}); ok {
		if value := stringFromMap(integrationRaw, "assigned_service"); value != "" {
			return value
		}
		if value := stringFromMap(integrationRaw, "service"); value != "" {
			return value
		}
	}

	return ""
}

func extractReferencedStageCodes(taskDef TaskDefinition) []string {
	if len(taskDef.Config) == 0 {
		return nil
	}
	var config map[string]interface{}
	if err := json.Unmarshal(taskDef.Config, &config); err != nil {
		return nil
	}

	result := make([]string, 0, 2)
	for _, key := range []string{"stage_code", "target_stage_code"} {
		if value := stringFromMap(config, key); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func stringFromMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func validateCompensatingCycle(result *ValidationResult, graph map[string]string) {
	state := make(map[string]int, len(graph))
	path := make([]string, 0, len(graph))

	var visit func(node string)
	visit = func(node string) {
		if state[node] == 1 {
			cyclePath := append(path, node)
			result.Add("task_definitions.compensating_task_code", fmt.Sprintf("circular compensating task dependency detected: %s", strings.Join(cyclePath, " -> ")))
			return
		}
		if state[node] == 2 {
			return
		}
		state[node] = 1
		path = append(path, node)

		next, ok := graph[node]
		if ok && strings.TrimSpace(next) != "" {
			visit(next)
		}

		if len(path) > 0 {
			path = path[:len(path)-1]
		}
		state[node] = 2
	}

	for node := range graph {
		if state[node] == 0 {
			visit(node)
		}
	}
}
