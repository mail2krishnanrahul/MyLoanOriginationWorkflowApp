package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// WorkbasketSummaryRow represents a workbasket with queue depth and oldest task age.
type WorkbasketSummaryRow struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Type                  string `json:"type"`
	Depth                 int    `json:"depth"`
	OldestTaskAgeMinutes  int    `json:"oldestTaskAgeMinutes"`
}

// WorkbasketTaskRow represents a task sitting in a workbasket.
type WorkbasketTaskRow struct {
	ID            string  `json:"id"`
	TaskName      string  `json:"taskName"`
	CaseReference string  `json:"caseReference"`
	Priority      string  `json:"priority"`
	DueAt         *string `json:"dueAt,omitempty"`
	WaitingSince  string  `json:"waitingSince"`
	SLAStatus     string  `json:"slaStatus"`
}

// ListWorkbaskets returns all workbaskets with their queue depth and oldest task age.
func (r *Repository) ListWorkbaskets(ctx context.Context) ([]WorkbasketSummaryRow, error) {
	query := `
		SELECT
			w.id::text AS id,
			w.name,
			w.type,
			COALESCE(stats.depth, 0) AS depth,
			COALESCE(stats.oldest_age_minutes, 0) AS oldest_age_minutes
		FROM workbaskets w
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*)::int AS depth,
				EXTRACT(EPOCH FROM (now() - MIN(t.created_at)))::int / 60 AS oldest_age_minutes
			FROM tasks t
			WHERE t.workbasket_id = w.id
			  AND t.status IN ('PENDING', 'IN_PROGRESS')
			  AND t.assignee_id IS NULL
		) stats ON true
		ORDER BY
			CASE w.type
				WHEN 'ESCALATION' THEN 0
				WHEN 'SPECIALIST' THEN 1
				ELSE 2
			END,
			w.name`

	rows, err := r.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListWorkbaskets: %w", err)
	}
	defer rows.Close()

	var results []WorkbasketSummaryRow
	for rows.Next() {
		var row WorkbasketSummaryRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Type, &row.Depth, &row.OldestTaskAgeMinutes); err != nil {
			return nil, fmt.Errorf("ListWorkbaskets scan: %w", err)
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWorkbaskets rows: %w", err)
	}

	if results == nil {
		results = []WorkbasketSummaryRow{}
	}
	return results, nil
}

// priorityIntToLabel maps DB integer priority to the frontend string enum.
func priorityIntToLabel(p int) string {
	switch {
	case p >= 80:
		return "CRITICAL"
	case p >= 60:
		return "HIGH"
	case p >= 40:
		return "NORMAL"
	default:
		return "LOW"
	}
}

// ListWorkbasketTasks returns unclaimed tasks in the given workbasket,
// sorted by priority DESC then due date ASC (matching the DB index).
func (r *Repository) ListWorkbasketTasks(ctx context.Context, workbasketID string) ([]WorkbasketTaskRow, error) {
	query := `
		SELECT
			t.id::text         AS id,
			COALESCE(t.task_definition_code, t.assigned_service, 'Task') AS task_name,
			COALESCE(c.reference_number, t.case_id::text)                AS case_reference,
			t.priority,
			t.due_at,
			t.created_at       AS waiting_since
		FROM tasks t
		LEFT JOIN cases c ON c.id = t.case_id
		WHERE t.workbasket_id = $1::uuid
		  AND t.status IN ('PENDING', 'IN_PROGRESS')
		  AND t.assignee_id IS NULL
		ORDER BY t.priority DESC, t.due_at ASC NULLS LAST`

	rows, err := r.Pool.Query(ctx, query, workbasketID)
	if err != nil {
		return nil, fmt.Errorf("ListWorkbasketTasks: %w", err)
	}
	defer rows.Close()

	var results []WorkbasketTaskRow
	for rows.Next() {
		var (
			id            string
			taskName      string
			caseReference string
			priority      int
			dueAt         *time.Time
			waitingSince  time.Time
		)
		if err := rows.Scan(&id, &taskName, &caseReference, &priority, &dueAt, &waitingSince); err != nil {
			return nil, fmt.Errorf("ListWorkbasketTasks scan: %w", err)
		}

		row := WorkbasketTaskRow{
			ID:            id,
			TaskName:      taskName,
			CaseReference: caseReference,
			Priority:      priorityIntToLabel(priority),
			WaitingSince:  waitingSince.Format(time.RFC3339),
			SLAStatus:     "ON_TRACK",
		}

		if dueAt != nil {
			formatted := dueAt.Format(time.RFC3339)
			row.DueAt = &formatted

			remaining := time.Until(*dueAt)
			if remaining < 0 {
				row.SLAStatus = "BREACHED"
			} else if remaining.Hours() < 4 {
				row.SLAStatus = "WARNING"
			}
		}

		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWorkbasketTasks rows: %w", err)
	}

	if results == nil {
		results = []WorkbasketTaskRow{}
	}
	return results, nil
}

// ClaimTaskFromWorkbasket atomically claims a task by setting its assignee
// and removing it from the workbasket. Returns an error if the task is
// already claimed or doesn't exist.
func (r *Repository) ClaimTaskFromWorkbasket(ctx context.Context, taskID string, workerID string) error {
	query := `
		UPDATE tasks
		SET assignee_id   = $1,
		    workbasket_id = NULL,
		    status        = 'IN_PROGRESS',
		    updated_at    = now()
		WHERE id = $2::uuid
		  AND assignee_id IS NULL
		  AND status IN ('PENDING', 'IN_PROGRESS')`

	tag, err := r.Pool.Exec(ctx, query, workerID, taskID)
	if err != nil {
		return fmt.Errorf("ClaimTaskFromWorkbasket: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
