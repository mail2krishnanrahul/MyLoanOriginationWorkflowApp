package db

import (
	"context"
	"fmt"
	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	Pool *pgxpool.Pool
}

func NewDB(connString string) (*DB, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Connect (backward compatibility wrapper or alias if needed, or we just renamed NewDB in other files?)
// verification-test/main.go used db.Connect. Let's add it.
func Connect(connString string) (*pgxpool.Pool, error) {
	db, err := NewDB(connString)
	if err != nil {
		return nil, err
	}
	return db.Pool, nil
}

// FetchPendingEvents uses SELECT ... FOR UPDATE SKIP LOCKED to get a batch of events
func (db *DB) FetchPendingEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	query := `
		UPDATE events_outbox
		SET status = 'PROCESSING', delivered_at = NOW()
		WHERE id IN (
			SELECT id
			FROM events_outbox
			WHERE status = 'PENDING'
			ORDER BY created_at ASC
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, event_type, payload, status, created_at, delivered_at, attempts
	`

	rows, err := db.Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch events: %w", err)
	}
	defer rows.Close()

	var events []model.OutboxEvent
	for rows.Next() {
		var e model.OutboxEvent
		err := rows.Scan(&e.ID, &e.EventType, &e.Payload, &e.Status, &e.CreatedAt, &e.DeliveredAt, &e.Attempts)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		events = append(events, e)
	}

	return events, nil
}
