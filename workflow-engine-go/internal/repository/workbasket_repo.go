package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// WorkbasketSummaryRow represents a workbasket with queue depth and oldest task age.
type WorkbasketSummaryRow struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	Depth                int    `json:"depth"`
	OldestTaskAgeMinutes int    `json:"oldestTaskAgeMinutes"`
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

// WorkbasketMemberRow represents a member of a workbasket.
type WorkbasketMemberRow struct {
	WorkerID  string  `json:"workerId"`
	JoinedAt  string  `json:"joinedAt"`
	ExpiresAt *string `json:"expiresAt,omitempty"`
}

// CreateWorkbasketInput holds the fields needed to create a new workbasket.
type CreateWorkbasketInput struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Strategy string `json:"strategy"`
}

// AddWorkbasketMemberInput holds the fields needed to add a member to a workbasket.
type AddWorkbasketMemberInput struct {
	WorkerID  string     `json:"workerId"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// ListWorkbaskets returns workbaskets that the given worker is a current (non-expired) member of,
// along with their queue depth and oldest task age. When workerID is empty all baskets are returned
// (admin use-case).
//
// ESCALATION baskets are always listed first (they skip normal priority ordering).
func (r *Repository) ListWorkbaskets(ctx context.Context, workerID string) ([]WorkbasketSummaryRow, error) {
	// Build the member-filter clause dynamically so we can support the
	// admin case (workerID == "") without a second query.
	memberFilter := ""
	var args []any
	if workerID != "" {
		memberFilter = `
		JOIN workbasket_members wbm
			ON wbm.workbasket_id = w.id
			AND wbm.worker_id    = $1
			AND (wbm.expires_at IS NULL OR wbm.expires_at > now())`
		args = append(args, workerID)
	}

	query := fmt.Sprintf(`
		SELECT
			w.id::text AS id,
			w.name,
			w.type,
			COALESCE(stats.depth, 0)               AS depth,
			COALESCE(stats.oldest_age_minutes, 0)  AS oldest_age_minutes
		FROM workbaskets w
		%s
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
			w.name`, memberFilter)

	rows, err := r.Pool.Query(ctx, query, args...)
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

// CreateWorkbasket inserts a new workbasket and returns its assigned ID.
func (r *Repository) CreateWorkbasket(ctx context.Context, in CreateWorkbasketInput) (string, error) {
	strategy := in.Strategy
	if strategy == "" {
		strategy = "ROUND_ROBIN"
	}

	var id string
	err := r.Pool.QueryRow(ctx, `
		INSERT INTO workbaskets (name, type, strategy)
		VALUES ($1, $2, $3)
		RETURNING id::text`, in.Name, in.Type, strategy).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("CreateWorkbasket: %w", err)
	}
	return id, nil
}

// AddWorkbasketMember adds a worker to a workbasket. If the worker is already a member the
// call is a no-op (upsert on conflict — we update expires_at in case it changed).
func (r *Repository) AddWorkbasketMember(ctx context.Context, workbasketID string, in AddWorkbasketMemberInput) error {
	_, err := r.Pool.Exec(ctx, `
		INSERT INTO workbasket_members (workbasket_id, worker_id, expires_at)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (workbasket_id, worker_id) DO UPDATE
			SET expires_at = EXCLUDED.expires_at,
			    joined_at  = now()`,
		workbasketID, in.WorkerID, in.ExpiresAt)
	if err != nil {
		return fmt.Errorf("AddWorkbasketMember: %w", err)
	}
	return nil
}

// RemoveWorkbasketMember removes a worker from a workbasket. Returns pgx.ErrNoRows if the
// membership did not exist.
func (r *Repository) RemoveWorkbasketMember(ctx context.Context, workbasketID, workerID string) error {
	tag, err := r.Pool.Exec(ctx, `
		DELETE FROM workbasket_members
		WHERE workbasket_id = $1::uuid
		  AND worker_id     = $2`,
		workbasketID, workerID)
	if err != nil {
		return fmt.Errorf("RemoveWorkbasketMember: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ListWorkbasketMembers returns all current (non-expired) members of the given workbasket.
func (r *Repository) ListWorkbasketMembers(ctx context.Context, workbasketID string) ([]WorkbasketMemberRow, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT worker_id, joined_at, expires_at
		FROM workbasket_members
		WHERE workbasket_id = $1::uuid
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY joined_at ASC`, workbasketID)
	if err != nil {
		return nil, fmt.Errorf("ListWorkbasketMembers: %w", err)
	}
	defer rows.Close()

	var results []WorkbasketMemberRow
	for rows.Next() {
		var (
			workerID  string
			joinedAt  time.Time
			expiresAt *time.Time
		)
		if err := rows.Scan(&workerID, &joinedAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("ListWorkbasketMembers scan: %w", err)
		}
		row := WorkbasketMemberRow{
			WorkerID: workerID,
			JoinedAt: joinedAt.Format(time.RFC3339),
		}
		if expiresAt != nil {
			formatted := expiresAt.Format(time.RFC3339)
			row.ExpiresAt = &formatted
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWorkbasketMembers rows: %w", err)
	}

	if results == nil {
		results = []WorkbasketMemberRow{}
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
// ESCALATION baskets skip normal ordering — ESCALATION tasks are always served first
// regardless of the basket type (the index handles this via priority DESC).
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
// and removing it from the workbasket. Returns pgx.ErrNoRows if the task is
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
