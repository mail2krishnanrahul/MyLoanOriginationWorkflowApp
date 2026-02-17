package repository

import (
	"context"
	"fmt"

	"workflow-engine/pkg/model"
)

// LoadActivitiesForStage returns all activity definitions for a given
// case_type version and stage, ordered by their ordinal.
func (r *Repository) LoadActivitiesForStage(
	ctx context.Context,
	executor DBExecutor,
	caseTypeID string,
	caseTypeVersion int,
	stageCode string,
) ([]model.ActivityDefinition, error) {
	query := `
		SELECT id, case_type_id, case_type_version, stage_code,
		       activity_code, activity_name, description,
		       ordinal, is_optional, completion_policy, created_at
		FROM activity_definitions
		WHERE case_type_id = $1::uuid
		  AND case_type_version = $2
		  AND stage_code = $3
		ORDER BY ordinal ASC`

	if executor == nil {
		executor = r.Pool
	}

	rows, err := executor.Query(ctx, query, caseTypeID, caseTypeVersion, stageCode)
	if err != nil {
		return nil, fmt.Errorf("failed to query activities for stage %s: %w", stageCode, err)
	}
	defer rows.Close()

	var activities []model.ActivityDefinition
	for rows.Next() {
		var a model.ActivityDefinition
		if err := rows.Scan(
			&a.ID, &a.CaseTypeID, &a.CaseTypeVersion, &a.StageCode,
			&a.ActivityCode, &a.ActivityName, &a.Description,
			&a.Ordinal, &a.IsOptional, &a.CompletionPolicy, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan activity: %w", err)
		}
		activities = append(activities, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return activities, nil
}
