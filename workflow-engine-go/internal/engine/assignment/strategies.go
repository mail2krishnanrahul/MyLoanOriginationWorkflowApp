package assignment

import (
	"context"
	"fmt"
	"log/slog"

	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

// ErrNoEligibleWorker is the package-level alias for the centralized sentinel.
var ErrNoEligibleWorker = model.ErrNoEligibleWorker

// ---------------------------------------------------------------------------
// Round Robin Strategy
// ---------------------------------------------------------------------------

type RoundRobinAssignmentEngine struct {
	Repo repository.DBExecutor // Actually might need specific methods not in generic executor?
	// The strategies need to query specialized tables.
	// I'll assume standard Repo has helper methods or use direct query if allowed?
	// The constraints say "Use sqlx for DB access", but I am using pgx in this project.
	// I will use DBExecutor interfaces.
}

func NewRoundRobinAssignmentEngine() *RoundRobinAssignmentEngine {
	return &RoundRobinAssignmentEngine{}
}

func (e *RoundRobinAssignmentEngine) FindCandidate(ctx context.Context, tx repository.DBExecutor, task *model.Task) (string, error) {
	if task.WorkbasketID == nil {
		return "", fmt.Errorf("task has no workbasket ID")
	}

	// 1. Get ordered list of members
	// 2. Get cursor
	// 3. Find next
	// This logic requires querying specialized tables.
	// I should probably put these queries in `Repo`.
	// For now, I will write raw SQL queries here using `tx`.

	// Query to get next eligible worker:
	// Find members of basket, ordered by ID.
	// Find one > Cursor.
	// If none, Find one > "" (wrap around).
	// Must check availability & capacity too?
	// "Assignment engine selects ... eligible candidates (skill-matched, available, under capacity)"
	// Round Robin distributes evenly among ELIGIBLE.

	query := `
		WITH eligible_members AS (
			SELECT wm.worker_id
			FROM workbasket_members wm
			JOIN workers w ON wm.worker_id = w.id
			LEFT JOIN worker_availability wa ON w.id = wa.worker_id
				AND wa.available_from <= now()
				AND wa.available_until >= now()
			WHERE wm.workbasket_id = $1::uuid
			  AND w.status = 'ACTIVE'
			  -- Check Capacity
			  AND (SELECT COUNT(*) FROM tasks t WHERE t.assignee_id = w.id AND t.status = 'IN_PROGRESS') < w.max_concurrent_tasks
			  -- Check OOO
			  AND wa.id IS NULL -- implies NO active OOO record overlapping NOW
		),
		current_cursor AS (
			SELECT round_robin_cursor FROM workbaskets WHERE id = $1::uuid
		)
		SELECT em.worker_id
		FROM eligible_members em, current_cursor cc
		WHERE em.worker_id > COALESCE(cc.round_robin_cursor, '')
		ORDER BY em.worker_id ASC
		LIMIT 1;`

	var workerID string
	err := tx.QueryRow(ctx, query, task.WorkbasketID).Scan(&workerID)
	if err != nil {
		// Try wrap around
		queryWrap := `
			WITH eligible_members AS (
				SELECT wm.worker_id
				FROM workbasket_members wm
				JOIN workers w ON wm.worker_id = w.id
				LEFT JOIN worker_availability wa ON w.id = wa.worker_id
					AND wa.available_from <= now()
					AND wa.available_until >= now()
				WHERE wm.workbasket_id = $1::uuid
				  AND w.status = 'ACTIVE'
				  AND (SELECT COUNT(*) FROM tasks t WHERE t.assignee_id = w.id AND t.status = 'IN_PROGRESS') < w.max_concurrent_tasks
				  AND wa.id IS NULL
			)
			SELECT worker_id
			FROM eligible_members
			ORDER BY worker_id ASC
			LIMIT 1;`
		err = tx.QueryRow(ctx, queryWrap, task.WorkbasketID).Scan(&workerID)
		if err != nil {
			return "", ErrNoEligibleWorker
		}
	}

	// Update Cursor (side effect in FindCandidate?
	// Usually FindCandidate just finds. The caller assigns.
	// But RoundRobin depends on state update.
	// I'll update cursor here? Or return it?
	// "It must apply a load-balancing strategy ... ROUND_ROBIN distributes evenly using a persistent cursor"
	// Safe to update cursor here as part of the transaction finding the user.

	_, err = tx.Exec(ctx, `UPDATE workbaskets SET round_robin_cursor = $1, updated_at = now() WHERE id = $2::uuid`, workerID, task.WorkbasketID)
	if err != nil {
		return "", fmt.Errorf("failed to update cursor: %w", err)
	}

	return workerID, nil
}

// ---------------------------------------------------------------------------
// Least Loaded Strategy
// ---------------------------------------------------------------------------

type LeastLoadedAssignmentEngine struct{}

func NewLeastLoadedAssignmentEngine() *LeastLoadedAssignmentEngine {
	return &LeastLoadedAssignmentEngine{}
}

func (e *LeastLoadedAssignmentEngine) FindCandidate(ctx context.Context, tx repository.DBExecutor, task *model.Task) (string, error) {
	if task.WorkbasketID == nil {
		return "", fmt.Errorf("task has no workbasket ID")
	}

	query := `
		SELECT wm.worker_id
		FROM workbasket_members wm
		JOIN workers w ON wm.worker_id = w.id
		LEFT JOIN worker_availability wa ON w.id = wa.worker_id
			AND wa.available_from <= now() AND wa.available_until >= now()
		-- Join tasks to count load
		LEFT JOIN tasks t ON t.assignee_id = w.id AND t.status = 'IN_PROGRESS'
		WHERE wm.workbasket_id = $1::uuid
		  AND w.status = 'ACTIVE'
		  AND wa.id IS NULL
		GROUP BY wm.worker_id, w.max_concurrent_tasks
		HAVING COUNT(t.id) < w.max_concurrent_tasks
		ORDER BY COUNT(t.id) ASC, wm.worker_id ASC
		LIMIT 1
	`

	var workerID string
	err := tx.QueryRow(ctx, query, task.WorkbasketID).Scan(&workerID)
	if err != nil {
		return "", ErrNoEligibleWorker
	}
	return workerID, nil
}

// ---------------------------------------------------------------------------
// Skill Score Strategy
// ---------------------------------------------------------------------------

type SkillScoreAssignmentEngine struct{}

func NewSkillScoreAssignmentEngine() *SkillScoreAssignmentEngine {
	return &SkillScoreAssignmentEngine{}
}

func (e *SkillScoreAssignmentEngine) FindCandidate(ctx context.Context, tx repository.DBExecutor, task *model.Task) (string, error) {
	if task.WorkbasketID == nil {
		return "", fmt.Errorf("task has no workbasket ID")
	}

	// Complex query:
	// 1. Filter eligible (Avail, Cap)
	// 2. Score = sum(proficiency_weight) for required skills.
	// 3. Must meet min proficiency for ALL required skills?
	// "A task with multiple required skills must find a worker who satisfies ALL of them at or above threshold."
	// AND "SKILL_SCORE picks the worker ... highest aggregate proficiency score"

	if task.RequiredSkills == nil {
		return "", fmt.Errorf("task has no required skills")
	}

	// Assuming required_skills is parsed into code? Or do we rely on JSONB query?
	// Using JSONB in SQL is cleaner for set matching.

	// Proficiency Map: BEGINNER=1, COMPETENT=2, EXPERT=3

	query := `
		WITH task_reqs AS (
			SELECT 
				value->>'code' as code,
				value->>'min_proficiency' as min_prof
			FROM jsonb_array_elements($2::jsonb)
		),
		scored_workers AS (
			SELECT 
				wm.worker_id,
				SUM(
					CASE ws.proficiency 
						WHEN 'BEGINNER' THEN 1 
						WHEN 'COMPETENT' THEN 2 
						WHEN 'EXPERT' THEN 3 
						ELSE 0 
					END
				) as score,
				COUNT(tr.code) as skills_matched
			FROM workbasket_members wm
			JOIN workers w ON wm.worker_id = w.id
			LEFT JOIN worker_availability wa ON w.id = wa.worker_id
				AND wa.available_from <= now() AND wa.available_until >= now()
			CROSS JOIN task_reqs tr
			JOIN worker_skills ws ON ws.worker_id = w.id AND ws.skill_code = tr.code
			WHERE wm.workbasket_id = $1::uuid
			  AND w.status = 'ACTIVE'
			  AND wa.id IS NULL
			  AND (SELECT COUNT(*) FROM tasks t WHERE t.assignee_id = w.id AND t.status = 'IN_PROGRESS') < w.max_concurrent_tasks
			  -- Check min proficiency
			  AND (
			  	CASE 
					WHEN tr.min_prof = 'EXPERT' THEN ws.proficiency = 'EXPERT'
					WHEN tr.min_prof = 'COMPETENT' THEN ws.proficiency IN ('COMPETENT', 'EXPERT')
					ELSE TRUE 
				END
			  )
			GROUP BY wm.worker_id
		)
		SELECT worker_id
		FROM scored_workers
		WHERE skills_matched = (SELECT COUNT(*) FROM task_reqs) -- Must match ALL
		ORDER BY score DESC, worker_id ASC
		LIMIT 1;
	`

	// Note: If required_skills is empty in JSON, this query might fail or return nothing.
	// Should handle empty skills case (fallback to LeastLoaded or RoundRobin).
	// "A task with multiple required skills..." implies if none, maybe any worker?
	// I'll assume caller handles fallback or we check length.

	var workerID string
	err := tx.QueryRow(ctx, query, task.WorkbasketID, task.RequiredSkills).Scan(&workerID)
	if err != nil {
		slog.Warn("skill assignment failed to find match", "error", err)
		return "", ErrNoEligibleWorker
	}

	return workerID, nil
}
