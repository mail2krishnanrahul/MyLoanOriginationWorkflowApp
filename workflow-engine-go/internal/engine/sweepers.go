package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5"
)

// ---------------------------------------------------------------------------
// [B-01 FIX] RunExpirySweep — fully implemented, batched, with events
// ---------------------------------------------------------------------------

// RunExpirySweep closes active cases that have exceeded their TTL as defined
// in the case_type config JSONB (config->>'ttl_days'). It processes in batches
// of 100 using FOR UPDATE SKIP LOCKED to allow concurrent engine instances.
//
// For each expired case it: cancels all open tasks, sets case status to
// CANCELLED with withdrawal_reason 'TTL_EXPIRED', and publishes a
// CASE_EXPIRED event — all within a single transaction per batch.
func (e *Engine) RunExpirySweep(ctx context.Context) error {
	slog.Info("starting expiry sweep")
	const batchSize = 100

	for {
		// Check context between batches for graceful shutdown
		if err := ctx.Err(); err != nil {
			slog.Info("expiry sweep cancelled", "reason", err)
			return nil
		}

		processed := 0
		err := e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT c.id, COALESCE((ct.config->>'ttl_days')::int, 0) AS ttl_days
				FROM cases c
				JOIN case_types ct ON c.case_type_id = ct.id
				WHERE c.status IN ('OPEN', 'IN_PROGRESS')
				  AND ct.config->>'ttl_days' IS NOT NULL
				  AND c.created_at + ((ct.config->>'ttl_days')::int || ' days')::interval < now()
				ORDER BY c.created_at ASC
				LIMIT $1
				FOR UPDATE OF c SKIP LOCKED`, batchSize)
			if err != nil {
				return fmt.Errorf("RunExpirySweep: query expired cases: %w", err)
			}
			defer rows.Close()

			type expiredCase struct {
				ID      string
				TTLDays int
			}
			var cases []expiredCase
			for rows.Next() {
				var ec expiredCase
				if err := rows.Scan(&ec.ID, &ec.TTLDays); err != nil {
					slog.Error("RunExpirySweep: scan", "error", err)
					continue
				}
				cases = append(cases, ec)
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("RunExpirySweep: rows iteration: %w", err)
			}

			for _, ec := range cases {
				if err := ctx.Err(); err != nil {
					return nil // graceful shutdown mid-batch
				}

				// Cancel all open tasks for this case
				_, err := tx.Exec(ctx, `
					UPDATE tasks
					SET status = 'CANCELLED',
					    completed_at = now(),
					    updated_at = now(),
					    version = version + 1
					WHERE case_id = $1::uuid
					  AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED', 'FAILED')`, ec.ID)
				if err != nil {
					return fmt.Errorf("RunExpirySweep: cancel tasks for case %s: %w", ec.ID, err)
				}

				// Close the case as expired
				_, err = tx.Exec(ctx, `
					UPDATE cases
					SET status = 'CANCELLED',
					    completed_at = now(),
					    withdrawal_reason = 'TTL_EXPIRED',
					    row_version = row_version + 1,
					    updated_at = now()
					WHERE id = $1::uuid`, ec.ID)
				if err != nil {
					return fmt.Errorf("RunExpirySweep: close case %s: %w", ec.ID, err)
				}

				// Publish CASE_EXPIRED event
				payload, err := json.Marshal(map[string]interface{}{
					"case_id":    ec.ID,
					"ttl_days":   ec.TTLDays,
					"expired_at": time.Now().UTC(),
				})
				if err != nil {
					return fmt.Errorf("RunExpirySweep: marshal payload for case %s: %w", ec.ID, err)
				}

				caseID := ec.ID
				if err := e.Repo.PublishEvent(ctx, tx, model.Event{
					CaseID:    &caseID,
					EventType: model.EventCaseExpired,
					Payload:   payload,
					Status:    model.EventStatusPending,
				}); err != nil {
					return fmt.Errorf("RunExpirySweep: publish event for case %s: %w", ec.ID, err)
				}

				processed++
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("RunExpirySweep: %w", err)
		}

		slog.Info("expiry sweep batch complete", "processed", processed)
		if processed < batchSize {
			break // no more to process
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// RunSLAUrgencySweep
// ---------------------------------------------------------------------------

// RunSLAUrgencySweep promotes task priority based on due_at proximity.
// Interval: Recommended 1-5 minutes.
func (e *Engine) RunSLAUrgencySweep(ctx context.Context) error {
	slog.Info("starting SLA urgency sweep")

	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		// 1. Promote to HIGH (>80% of SLA elapsed)
		_, err := tx.Exec(ctx, `
			UPDATE tasks
			SET priority = 3,
			    updated_at = now()
			WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS')
			  AND priority < 3
			  AND due_at IS NOT NULL
			  AND due_at > now()
			  AND EXTRACT(EPOCH FROM (now() - created_at)) / NULLIF(EXTRACT(EPOCH FROM (due_at - created_at)), 0) > 0.8
		`)
		if err != nil {
			return fmt.Errorf("RunSLAUrgencySweep: promote to HIGH: %w", err)
		}

		// 2. Promote to CRITICAL & move to ESCALATION workbasket (>95% elapsed)
		_, err = tx.Exec(ctx, `
			UPDATE tasks
			SET priority = 4,
			    workbasket_id = (SELECT id FROM workbaskets WHERE type = 'ESCALATION' LIMIT 1),
			    updated_at = now()
			WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS')
			  AND priority < 4
			  AND due_at IS NOT NULL
			  AND due_at > now()
			  AND EXTRACT(EPOCH FROM (now() - created_at)) / NULLIF(EXTRACT(EPOCH FROM (due_at - created_at)), 0) > 0.95
		`)
		if err != nil {
			return fmt.Errorf("RunSLAUrgencySweep: promote to CRITICAL: %w", err)
		}

		// 3. Detect new breaches (due_at < now, not already logged)
		rows, err := tx.Query(ctx, `
			SELECT id, case_id, assignee_id, due_at
			FROM tasks
			WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS')
			  AND due_at IS NOT NULL
			  AND due_at < now()
			  AND NOT EXISTS (SELECT 1 FROM sla_breach_log WHERE task_id = tasks.id)
		`)
		if err != nil {
			return fmt.Errorf("RunSLAUrgencySweep: query breaches: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("RunSLAUrgencySweep: context cancelled: %w", err)
			}

			var tID, cID string
			var dueAt time.Time
			var assigneeIDPtr *string

			if err := rows.Scan(&tID, &cID, &assigneeIDPtr, &dueAt); err != nil {
				slog.Error("RunSLAUrgencySweep: scan breach row", "error", err)
				continue
			}

			// Insert breach log entry
			_, err := tx.Exec(ctx, `
				INSERT INTO sla_breach_log (task_id, case_id, original_due_at, breach_detected_at, assignee_at_breach, elapsed_percentage)
				VALUES ($1::uuid, $2::uuid, $3, now(), $4, 100)
			`, tID, cID, dueAt, assigneeIDPtr)
			if err != nil {
				slog.Error("RunSLAUrgencySweep: insert breach log", "task_id", tID, "error", err)
				continue
			}

			// Publish TASK_SLA_BREACHED event
			payload, _ := json.Marshal(map[string]interface{}{
				"task_id":     tID,
				"case_id":     cID,
				"due_at":      dueAt,
				"breach_time": time.Now().UTC(),
			})
			if err := e.Repo.PublishEvent(ctx, tx, model.Event{
				TaskID:    &tID,
				CaseID:    &cID,
				EventType: model.EventTaskSLABreached,
				Payload:   payload,
				Status:    model.EventStatusPending,
			}); err != nil {
				return fmt.Errorf("RunSLAUrgencySweep: publish breach event for task %s: %w", tID, err)
			}
		}

		return rows.Err()
	})
}

// ---------------------------------------------------------------------------
// RunCapacitySweep
// ---------------------------------------------------------------------------

// RunCapacitySweep redistributes tasks from OOO workers back to their
// originating workbasket and publishes TASK_QUEUED events so the
// auto-distributor can re-assign them.
// Interval: 15 mins.
func (e *Engine) RunCapacitySweep(ctx context.Context) error {
	slog.Info("starting capacity/OOO sweep")
	return e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
		// OOO Handling: Find tasks assigned to workers with an active OOO record
		// and return them to the originating workbasket (or GENERAL fallback).
		// Use RETURNING to get the task/workbasket IDs for event publishing.
		rows, err := tx.Query(ctx, `
			UPDATE tasks
			SET assignee_id = NULL,
			    status = 'PENDING',
			    workbasket_id = COALESCE(workbasket_id, (SELECT id FROM workbaskets WHERE type='GENERAL' LIMIT 1)),
			    updated_at = now(),
			    version = version + 1
			FROM workers w
			JOIN worker_availability wa ON w.id = wa.worker_id
			WHERE tasks.assignee_id = w.id
			  AND tasks.status IN ('ASSIGNED', 'IN_PROGRESS')
			  AND wa.available_from <= now()
			  AND wa.available_until >= now()
			RETURNING tasks.id, tasks.case_id, tasks.workbasket_id
		`)
		if err != nil {
			return fmt.Errorf("RunCapacitySweep: update OOO tasks: %w", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var taskID, caseID string
			var workbasketID *string
			if err := rows.Scan(&taskID, &caseID, &workbasketID); err != nil {
				slog.Error("RunCapacitySweep: scan returned row", "error", err)
				continue
			}
			count++

			// Publish TASK_QUEUED so the auto-distributor re-assigns
			if workbasketID != nil {
				payload, _ := json.Marshal(map[string]interface{}{
					"task_id":       taskID,
					"workbasket_id": *workbasketID,
					"reason":        "ooo_redistribution",
				})
				if err := e.Repo.PublishEvent(ctx, tx, model.Event{
					CaseID:    &caseID,
					TaskID:    &taskID,
					EventType: model.EventTaskQueued,
					Payload:   payload,
					Status:    model.EventStatusPending,
				}); err != nil {
					return fmt.Errorf("RunCapacitySweep: publish TASK_QUEUED for task %s: %w", taskID, err)
				}
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("RunCapacitySweep: rows iteration: %w", err)
		}

		if count > 0 {
			slog.Info("capacity sweep redistributed tasks", "count", count)
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// [B-02 FIX] RunArchivalSweep — fully implemented, batched, with events
// ---------------------------------------------------------------------------

// RunArchivalSweep moves completed/cancelled cases to archive tables after
// they exceed the archive TTL from case_type config (config->>'archive_after_days',
// defaults to 90 days). It processes in batches of 50 using FOR UPDATE SKIP LOCKED.
//
// For each archivable case it: copies case+tasks to archive tables, deletes
// originals, and publishes a CASE_ARCHIVED event — all within a single
// transaction per batch.
func (e *Engine) RunArchivalSweep(ctx context.Context) error {
	slog.Info("starting archival sweep")
	const batchSize = 50

	for {
		if err := ctx.Err(); err != nil {
			slog.Info("archival sweep cancelled", "reason", err)
			return nil
		}

		processed := 0
		err := e.Repo.WithTransaction(ctx, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT c.id
				FROM cases c
				JOIN case_types ct ON c.case_type_id = ct.id
				WHERE c.status IN ('COMPLETED', 'CANCELLED')
				  AND c.completed_at IS NOT NULL
				  AND c.completed_at + COALESCE(
				  	((ct.config->>'archive_after_days')::int || ' days')::interval,
				  	'90 days'::interval
				  ) < now()
				ORDER BY c.completed_at ASC
				LIMIT $1
				FOR UPDATE OF c SKIP LOCKED`, batchSize)
			if err != nil {
				return fmt.Errorf("RunArchivalSweep: query archivable cases: %w", err)
			}
			defer rows.Close()

			var caseIDs []string
			for rows.Next() {
				var caseID string
				if err := rows.Scan(&caseID); err != nil {
					slog.Error("RunArchivalSweep: scan", "error", err)
					continue
				}
				caseIDs = append(caseIDs, caseID)
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("RunArchivalSweep: rows iteration: %w", err)
			}

			for _, caseID := range caseIDs {
				if err := ctx.Err(); err != nil {
					return nil // graceful shutdown mid-batch
				}

				if err := e.Repo.ArchiveCase(ctx, tx, caseID); err != nil {
					return fmt.Errorf("RunArchivalSweep: archive case %s: %w", caseID, err)
				}

				// Publish CASE_ARCHIVED event
				payload, err := json.Marshal(map[string]string{
					"case_id":     caseID,
					"archived_at": time.Now().UTC().Format(time.RFC3339),
				})
				if err != nil {
					return fmt.Errorf("RunArchivalSweep: marshal payload for case %s: %w", caseID, err)
				}

				id := caseID
				if err := e.Repo.PublishEvent(ctx, tx, model.Event{
					CaseID:    &id,
					EventType: model.EventCaseArchived,
					Payload:   payload,
					Status:    model.EventStatusPending,
				}); err != nil {
					return fmt.Errorf("RunArchivalSweep: publish event for case %s: %w", caseID, err)
				}

				processed++
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("RunArchivalSweep: %w", err)
		}

		slog.Info("archival sweep batch complete", "processed", processed)
		if processed < batchSize {
			break // no more to process
		}
	}
	return nil
}
