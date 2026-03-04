//go:build ignore

// seed_workbaskets.go — seeds three demo workbaskets and a default worker
// member for local development. Safe to run multiple times (upserts).
//
//	Usage:
//	  cd workflow-engine-go
//	  DATABASE_URL="postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable" \
//	    go run scripts/seed_workbaskets.go
//
//	Override the default worker ID with:
//	  SEED_WORKER_ID="admin@example.com" go run scripts/seed_workbaskets.go

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
)

type basket struct {
	name     string
	btype    string
	strategy string
}

var baskets = []basket{
	{name: "General Processing Queue", btype: "GENERAL", strategy: "ROUND_ROBIN"},
	{name: "Specialist Review Queue", btype: "SPECIALIST", strategy: "SKILL_SCORE"},
	{name: "Escalation Queue", btype: "ESCALATION", strategy: "LEAST_LOADED"},
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable"
		log.Println("DATABASE_URL not set — using default")
	}

	workerID := os.Getenv("SEED_WORKER_ID")
	if workerID == "" {
		workerID = "admin@example.com"
		log.Printf("SEED_WORKER_ID not set — using default worker: %s", workerID)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Ensure the default worker exists (upsert).
	if _, err := conn.Exec(ctx, `
		INSERT INTO workers (id, max_concurrent_tasks, status)
		VALUES ($1, 10, 'ACTIVE')
		ON CONFLICT (id) DO UPDATE SET status = 'ACTIVE'`, workerID); err != nil {
		log.Fatalf("upsert worker: %v", err)
	}
	log.Printf("✓ worker %q ready", workerID)

	for _, b := range baskets {
		// Upsert workbasket by name.
		var basketID string
		err := conn.QueryRow(ctx, `
			INSERT INTO workbaskets (name, type, strategy)
			VALUES ($1, $2, $3)
			ON CONFLICT (name) DO UPDATE
				SET type     = EXCLUDED.type,
				    strategy = EXCLUDED.strategy
			RETURNING id::text`, b.name, b.btype, b.strategy).Scan(&basketID)
		if err != nil {
			log.Fatalf("upsert workbasket %q: %v", b.name, err)
		}

		// Add the default worker as a permanent member.
		if _, err := conn.Exec(ctx, `
			INSERT INTO workbasket_members (workbasket_id, worker_id, expires_at)
			VALUES ($1::uuid, $2, NULL)
			ON CONFLICT (workbasket_id, worker_id) DO UPDATE
				SET expires_at = NULL`, basketID, workerID); err != nil {
			log.Fatalf("add member to %q: %v", b.name, err)
		}

		fmt.Printf("✓ workbasket %-35s  id=%s  type=%-10s  member=%s\n",
			fmt.Sprintf("%q", b.name), basketID, b.btype, workerID)
	}

	log.Println("Seed complete. Run `http://localhost:5173/workbaskets` to view baskets.")
}
