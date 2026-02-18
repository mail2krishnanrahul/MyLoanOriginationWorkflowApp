package repository

import (
	"context"
	"fmt"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBExecutor is an interface that matches both *pgxpool.Pool and pgx.Tx
type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	Pool *pgxpool.Pool
	SQLX *sqlx.DB
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{Pool: pool}
}

// SetSQLX wires an sqlx handle for calendar-aware SLA computations.
func (r *Repository) SetSQLX(db *sqlx.DB) {
	r.SQLX = db
}

// PollPendingEvents fetches pending events from the outbox and atomically
// claims them by setting status to PROCESSING within a single transaction.
// This prevents duplicate delivery when multiple engine instances poll concurrently.
func (r *Repository) PollPendingEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	if limit <= 0 {
		limit = 10
	}

	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("PollPendingEvents: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, event_type, payload, status, created_at, attempts
		FROM events_outbox
		WHERE status = $1
		ORDER BY created_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED`,
		model.OutboxStatusPending, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("PollPendingEvents: query: %w", err)
	}
	defer rows.Close()

	var events []model.OutboxEvent
	var ids []string
	for rows.Next() {
		var event model.OutboxEvent
		if err := rows.Scan(
			&event.ID,
			&event.EventType,
			&event.Payload,
			&event.Status,
			&event.CreatedAt,
			&event.Attempts,
		); err != nil {
			return nil, fmt.Errorf("PollPendingEvents: scan: %w", err)
		}
		events = append(events, event)
		ids = append(ids, event.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("PollPendingEvents: rows iteration: %w", err)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// Atomically claim all polled events within the same transaction
	_, err = tx.Exec(ctx, `
		UPDATE events_outbox
		SET status = 'PROCESSING',
		    last_attempted_at = now()
		WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, fmt.Errorf("PollPendingEvents: claim events: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("PollPendingEvents: commit: %w", err)
	}

	return events, nil
}

// UpdateEventStatus updates the status of an outbox event.
func (r *Repository) UpdateEventStatus(ctx context.Context, executor DBExecutor, eventID string, status string, errorMessage *string) error {
	query := `
        UPDATE events_outbox
        SET status = $1::text, delivered_at = $2::timestamptz, attempts = CASE WHEN $1::text = 'FAILED' THEN attempts + 1 ELSE attempts END
        WHERE id = $3::uuid
          AND status NOT IN ('DELIVERED', 'FAILED')`

	if executor == nil {
		executor = r.Pool
	}

	tag, err := executor.Exec(ctx, query, status, time.Now(), eventID)
	if err != nil {
		return fmt.Errorf("failed to update event %s status to %s: %w", eventID, status, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s not found or already in terminal state", eventID)
	}
	return nil
}

// GetCaseWithLock fetches a case by ID and locks it for update
func (r *Repository) GetCaseWithLock(ctx context.Context, tx DBExecutor, caseID string) (*model.Case, error) {
	query := `
        SELECT id, workflow_definition_id, global_status, current_stage_id, applicant_data, version, created_at, updated_at
        FROM cases
        WHERE id = $1::uuid
        FOR UPDATE`

	var c model.Case
	err := tx.QueryRow(ctx, query, caseID).Scan(
		&c.ID,
		&c.WorkflowDefinitionID,
		&c.GlobalStatus,
		&c.CurrentStageID,
		&c.ApplicantData,
		&c.Version,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateCase updates the case state handling optimistic locking
func (r *Repository) UpdateCase(ctx context.Context, tx DBExecutor, c *model.Case) error {
	query := `
        UPDATE cases 
        SET global_status = $1::text, current_stage_id = $2::bigint, applicant_data = $3::jsonb, version = version + 1, updated_at = $4::timestamp
        WHERE id = $5::uuid AND version = $6::int`

	tag, err := tx.Exec(ctx, query,
		c.GlobalStatus,
		c.CurrentStageID,
		c.ApplicantData,
		time.Now(),
		c.ID,
		c.Version, // Optimistic Lock Check
	)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("optimistic lock failure: case %s has been modified", c.ID)
	}
	return nil
}

// GetWorkflowDefinition fetches the definition for a case
func (r *Repository) GetWorkflowDefinition(ctx context.Context, tx DBExecutor, id int64) (*model.WorkflowDefinition, error) {
	query := `
        SELECT id, name, version, is_active 
        FROM workflow_definitions 
        WHERE id = $1`

	var wd model.WorkflowDefinition
	// Not using version_hash and time for simplicity unless needed
	err := tx.QueryRow(ctx, query, id).Scan(&wd.ID, &wd.Name, &wd.Version, &wd.IsActive)
	if err != nil {
		return nil, err
	}
	return &wd, nil
}

// GetStageDefinition fetch stage details
func (r *Repository) GetStageDefinition(ctx context.Context, tx DBExecutor, id int64) (*model.LegacyStageDefinition, error) {
	query := `
        SELECT id, workflow_definition_id, name, sequence_order, config_json
        FROM stage_definitions
        WHERE id = $1`

	var sd model.LegacyStageDefinition
	err := tx.QueryRow(ctx, query, id).Scan(
		&sd.ID,
		&sd.WorkflowDefinitionID,
		&sd.Name,
		&sd.SequenceOrder,
		&sd.ConfigJSON,
	)
	if err != nil {
		return nil, err
	}
	return &sd, nil
}

// GetNextStageDefinition finds the next stage in sequence
func (r *Repository) GetNextStageDefinition(ctx context.Context, tx DBExecutor, workflowID int64, currentSequence int) (*model.LegacyStageDefinition, error) {
	query := `
        SELECT id, workflow_definition_id, name, sequence_order, config_json
        FROM stage_definitions
        WHERE workflow_definition_id = $1 AND sequence_order > $2
        ORDER BY sequence_order ASC
        LIMIT 1`

	var sd model.LegacyStageDefinition
	err := tx.QueryRow(ctx, query, workflowID, currentSequence).Scan(
		&sd.ID,
		&sd.WorkflowDefinitionID,
		&sd.Name,
		&sd.SequenceOrder,
		&sd.ConfigJSON,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No next stage (End of workflow)
		}
		return nil, err
	}
	return &sd, nil
}

// InsertOutboxEvent inserts a new event into the outbox.
// target_service defaults to "case-orchestrator" when not specified in the payload.
func (r *Repository) InsertOutboxEvent(ctx context.Context, executor DBExecutor, eventType string, payload map[string]interface{}) error {
	query := `
        INSERT INTO events_outbox (event_type, payload, status, target_service)
        VALUES ($1, $2, $3, $4)`

	if executor == nil {
		executor = r.Pool
	}

	// Derive target_service from payload if present, otherwise default
	targetService := "case-orchestrator"
	if ts, ok := payload["_target_service"].(string); ok && ts != "" {
		targetService = ts
	}

	_, err := executor.Exec(ctx, query, eventType, payload, model.OutboxStatusPending, targetService)
	return err
}

// WithTransaction executes a function within a transaction
func (r *Repository) WithTransaction(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
