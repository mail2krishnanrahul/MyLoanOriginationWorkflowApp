package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// CaseHeader — the joined case + case_type row for the read API
// ---------------------------------------------------------------------------

// CaseHeader holds the joined result of cases + case_types for display.
type CaseHeader struct {
	CaseID          string
	ReferenceNumber string
	CaseTypeCode    string
	CaseTypeVersion int
	Status          string
	CurrentStage    *string
	AssignedTo      *string
	Metadata        json.RawMessage
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
}

// GetCaseByReference fetches a case by its human-readable reference number,
// joining with case_types to include the code.
func (r *Repository) GetCaseByReference(ctx context.Context, ref string) (*CaseHeader, error) {
	var h CaseHeader
	err := r.Pool.QueryRow(ctx, `
		SELECT c.id, c.reference_number, ct.code, c.case_type_version,
		       c.status, c.current_stage_code, c.assigned_to,
		       c.metadata, c.created_at, c.updated_at, c.completed_at
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.reference_number = $1`, ref,
	).Scan(
		&h.CaseID, &h.ReferenceNumber, &h.CaseTypeCode, &h.CaseTypeVersion,
		&h.Status, &h.CurrentStage, &h.AssignedTo,
		&h.Metadata, &h.CreatedAt, &h.UpdatedAt, &h.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("case %s not found: %w", ref, err)
	}
	return &h, nil
}

// GetCaseByID fetches a case by its UUID primary key,
// joining with case_types to include the code.
func (r *Repository) GetCaseByID(ctx context.Context, id string) (*CaseHeader, error) {
	var h CaseHeader
	err := r.Pool.QueryRow(ctx, `
		SELECT c.id, c.reference_number, ct.code, c.case_type_version,
		       c.status, c.current_stage_code, c.assigned_to,
		       c.metadata, c.created_at, c.updated_at, c.completed_at
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.id = $1::uuid`, id,
	).Scan(
		&h.CaseID, &h.ReferenceNumber, &h.CaseTypeCode, &h.CaseTypeVersion,
		&h.Status, &h.CurrentStage, &h.AssignedTo,
		&h.Metadata, &h.CreatedAt, &h.UpdatedAt, &h.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("case %s not found: %w", id, err)
	}
	return &h, nil
}

// GetStageHistory fetches all stage transitions for a case, ordered chronologically.
func (r *Repository) GetStageHistory(ctx context.Context, caseID string) ([]model.CaseStageTransition, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT id, case_id,
		       from_stage_code, to_stage_code,
		       from_stage_ordinal, to_stage_ordinal,
		       is_regression, regression_reason,
		       triggered_by, transitioned_at
		FROM case_stage_transitions
		WHERE case_id = $1::uuid
		ORDER BY transitioned_at ASC`, caseID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stage history: %w", err)
	}
	defer rows.Close()

	var history []model.CaseStageTransition
	for rows.Next() {
		var t model.CaseStageTransition
		if err := rows.Scan(
			&t.ID, &t.CaseID,
			&t.FromStageCode, &t.ToStageCode,
			&t.FromStageOrdinal, &t.ToStageOrdinal,
			&t.IsRegression, &t.RegressionReason,
			&t.TriggeredBy, &t.TransitionedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan transition: %w", err)
		}
		history = append(history, t)
	}
	return history, rows.Err()
}

// GetSubCases fetches child cases for a parent case.
func (r *Repository) GetSubCases(ctx context.Context, parentCaseID string) ([]CaseHeader, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT c.id, c.reference_number, ct.code, c.case_type_version,
		       c.status, c.current_stage_code, c.assigned_to,
		       c.metadata, c.created_at, c.updated_at, c.completed_at
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.parent_case_id = $1::uuid
		ORDER BY c.created_at ASC`, parentCaseID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch sub-cases: %w", err)
	}
	defer rows.Close()

	var subs []CaseHeader
	for rows.Next() {
		var h CaseHeader
		if err := rows.Scan(
			&h.CaseID, &h.ReferenceNumber, &h.CaseTypeCode, &h.CaseTypeVersion,
			&h.Status, &h.CurrentStage, &h.AssignedTo,
			&h.Metadata, &h.CreatedAt, &h.UpdatedAt, &h.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan sub-case: %w", err)
		}
		subs = append(subs, h)
	}
	return subs, rows.Err()
}

// GetDealSnapshot fetches the latest deal snapshot from case_deal_links for a case.
// Returns nil if no deal link exists (e.g., non-ingested cases).
func (r *Repository) GetDealSnapshot(ctx context.Context, caseID string) (json.RawMessage, error) {
	var snapshot json.RawMessage
	err := r.Pool.QueryRow(ctx, `
		SELECT deal_snapshot
		FROM case_deal_links
		WHERE case_id = $1::uuid
		ORDER BY last_snapshot_refreshed_at DESC
		LIMIT 1`, caseID,
	).Scan(&snapshot)
	if err != nil {
		return nil, err // caller handles "no rows" gracefully
	}
	return snapshot, nil
}

// TaskStatusRow is one row from the aggregated task query.
type TaskStatusRow struct {
	ActivityCode       string
	TaskID             string
	TaskDefinitionCode string
	Status             string
	Priority           int
	AssignedService    *string
	DueAt              *time.Time
	CompletedAt        *time.Time
}

// GetTasksForCurrentStage fetches all tasks for a case's current stage
// in a single query, ordered by activity then priority.
// This is the aggregated SQL query that avoids N+1.
func (r *Repository) GetTasksForCurrentStage(
	ctx context.Context, caseID string, stageCode string,
) ([]TaskStatusRow, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT activity_code,
		       id, task_definition_code, status,
		       priority, assigned_service,
		       due_at, completed_at
		FROM tasks
		WHERE case_id = $1::uuid
		  AND stage_code = $2
		ORDER BY activity_code, priority DESC, created_at ASC`,
		caseID, stageCode,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tasks for stage: %w", err)
	}
	defer rows.Close()

	var tasks []TaskStatusRow
	for rows.Next() {
		var t TaskStatusRow
		if err := rows.Scan(
			&t.ActivityCode,
			&t.TaskID, &t.TaskDefinitionCode, &t.Status,
			&t.Priority, &t.AssignedService,
			&t.DueAt, &t.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// TaskProgressRow holds the aggregated counts from the progress query.
type TaskProgressRow struct {
	TotalTasks     int
	CompletedTasks int
}

// GetOverallTaskProgress counts total and completed tasks for a case
// (excluding SKIPPED from the denominator).
func (r *Repository) GetOverallTaskProgress(ctx context.Context, caseID string) (TaskProgressRow, error) {
	var p TaskProgressRow
	err := r.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status != 'SKIPPED') AS total,
			COUNT(*) FILTER (WHERE status = 'COMPLETED') AS completed
		FROM tasks
		WHERE case_id = $1::uuid`, caseID,
	).Scan(&p.TotalTasks, &p.CompletedTasks)
	if err != nil {
		return TaskProgressRow{}, fmt.Errorf("failed to count task progress: %w", err)
	}
	return p, nil
}
