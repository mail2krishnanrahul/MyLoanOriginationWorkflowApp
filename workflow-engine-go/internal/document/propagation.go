package document

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// ResolveTaskInputs resolves input dependencies from completed tasks.
func ResolveTaskInputs(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
	taskDef model.TaskDefinitionV2,
) (map[string]interface{}, error) {
	if db == nil {
		return nil, fmt.Errorf("ResolveTaskInputs: db is nil")
	}
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("ResolveTaskInputs: caseID is required")
	}

	resolved := map[string]interface{}{}
	if len(taskDef.Config) > 0 {
		if err := json.Unmarshal(taskDef.Config, &resolved); err != nil {
			return nil, fmt.Errorf("ResolveTaskInputs: parse task config defaults: %w", err)
		}
	}

	uniqueSourceTasks := make([]string, 0, len(taskDef.Inputs))
	sourceSeen := map[string]struct{}{}
	for _, input := range taskDef.Inputs {
		sourceTask := strings.TrimSpace(input.SourceTask)
		if sourceTask == "" {
			continue
		}
		if _, ok := sourceSeen[sourceTask]; ok {
			continue
		}
		sourceSeen[sourceTask] = struct{}{}
		uniqueSourceTasks = append(uniqueSourceTasks, sourceTask)
	}

	sourceOutputs := map[string]map[string]interface{}{}
	if len(uniqueSourceTasks) > 0 {
		query, args, err := sqlx.In(`
			SELECT task_definition_code, output_payload
			FROM (
				SELECT
					task_definition_code,
					output_payload,
					ROW_NUMBER() OVER (
						PARTITION BY task_definition_code
						ORDER BY completed_at DESC NULLS LAST, created_at DESC
					) AS rn
				FROM tasks
				WHERE case_id = ?
				  AND status = 'COMPLETED'
				  AND task_definition_code IN (?)
			) ranked
			WHERE rn = 1
		`, caseID, uniqueSourceTasks)
		if err != nil {
			return nil, fmt.Errorf("ResolveTaskInputs: build source task query: %w", err)
		}
		query = db.Rebind(query)

		type row struct {
			TaskDefinitionCode string          `db:"task_definition_code"`
			OutputPayload      json.RawMessage `db:"output_payload"`
		}
		var rows []row
		if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
			return nil, fmt.Errorf("ResolveTaskInputs: query source tasks: %w", err)
		}

		for _, row := range rows {
			payload := map[string]interface{}{}
			if len(row.OutputPayload) > 0 {
				if err := json.Unmarshal(row.OutputPayload, &payload); err != nil {
					return nil, fmt.Errorf("ResolveTaskInputs: parse output payload for %s: %w", row.TaskDefinitionCode, err)
				}
			}
			sourceOutputs[row.TaskDefinitionCode] = payload
		}
	}

	for _, input := range taskDef.Inputs {
		field := strings.TrimSpace(input.Field)
		if field == "" {
			return nil, fmt.Errorf("ResolveTaskInputs: input field is required for task %s", taskDef.Code)
		}

		sourceTask := strings.TrimSpace(input.SourceTask)
		if sourceTask == "" {
			if input.Required {
				if _, exists := resolved[field]; !exists {
					return nil, &DependencyError{SourceTask: taskDef.Code, SourceField: field}
				}
			}
			continue
		}

		sourceField := strings.TrimSpace(input.SourceField)
		if sourceField == "" {
			sourceField = field
		}
		sourcePayload, exists := sourceOutputs[sourceTask]
		if !exists {
			if input.Required {
				return nil, &DependencyError{SourceTask: sourceTask, SourceField: sourceField}
			}
			continue
		}

		value, found := getByPath(sourcePayload, sourceField)
		if !found && strings.HasPrefix(sourceField, "output_payload.") {
			value, found = getByPath(sourcePayload, strings.TrimPrefix(sourceField, "output_payload."))
		}
		if !found {
			if input.Required {
				return nil, &DependencyError{SourceTask: sourceTask, SourceField: sourceField}
			}
			continue
		}
		resolved[field] = value
	}

	return resolved, nil
}
