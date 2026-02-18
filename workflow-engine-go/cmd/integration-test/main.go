package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"workflow-engine/internal/database"
	"workflow-engine/internal/engine"
	"workflow-engine/internal/engine/assignment"
	"workflow-engine/internal/repository"
	"workflow-engine/pkg/model"
)

func main() {
	// 1. Setup
	connString := "postgres://myappuser:password@localhost:5432/LoanOriginationDB"
	ctx := context.Background()

	db, err := database.Connect(ctx, connString)
	if err != nil {
		slog.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Apply Migrations (Simulated for Test)
	slog.Info("applying migrations")
	schemaSQL := `
	DROP TABLE IF EXISTS audit_logs CASCADE;
	DROP TABLE IF EXISTS task_instances CASCADE;
	DROP TABLE IF EXISTS tasks CASCADE;
	DROP TABLE IF EXISTS outbox CASCADE;
	DROP TABLE IF EXISTS stage_instances CASCADE;
	DROP TABLE IF EXISTS cases CASCADE;
	DROP TABLE IF EXISTS loan_cases CASCADE;
	DROP TABLE IF EXISTS task_definitions CASCADE;
	DROP TABLE IF EXISTS stage_definitions CASCADE;
	DROP TABLE IF EXISTS workflow_definitions CASCADE;

	CREATE TABLE workflow_definitions (
		id BIGSERIAL PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		version INT NOT NULL,
		version_hash VARCHAR(64) NOT NULL,
		is_active BOOLEAN DEFAULT TRUE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE stage_definitions (
		id BIGSERIAL PRIMARY KEY,
		workflow_definition_id BIGINT REFERENCES workflow_definitions(id),
		name VARCHAR(255) NOT NULL,
		sequence_order INT NOT NULL,
		config_json JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE cases (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		workflow_definition_id BIGINT REFERENCES workflow_definitions(id),
		global_status VARCHAR(50) NOT NULL,
		current_stage_id BIGINT REFERENCES stage_definitions(id),
		applicant_data JSONB,
		version INT DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE outbox (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		event_type VARCHAR(255) NOT NULL,
		payload JSONB NOT NULL,
		status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		processed_at TIMESTAMP,
		retry_count INT DEFAULT 0,
		error_message TEXT
	);
	`
	_, err = db.Pool.Exec(ctx, schemaSQL)
	if err != nil {
		slog.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}

	repo := repository.NewRepository(db.Pool)

	// 2. Seed Data
	slog.Info("seeding test data")

	// Create Workflow Definition
	var workflowID int64
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO workflow_definitions (name, version, version_hash, is_active)
		VALUES ('Test 1-Step Workflow', 1, 'hash123', true)
		RETURNING id`).Scan(&workflowID)
	if err != nil {
		slog.Error("failed to seed workflow", "error", err)
		os.Exit(1)
	}

	// Create Stage 1
	var stageID int64
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO stage_definitions (workflow_definition_id, name, sequence_order, config_json)
		VALUES ($1, 'Stage 1', 1, '{}')
		RETURNING id`, workflowID).Scan(&stageID)
	if err != nil {
		slog.Error("failed to seed stage", "error", err)
		os.Exit(1)
	}

	// Create Case
	var caseID string
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO cases (workflow_definition_id, global_status, current_stage_id, version)
		VALUES ($1, 'OPEN', $2, 1)
		RETURNING id`, workflowID, stageID).Scan(&caseID)
	if err != nil {
		slog.Error("failed to seed case", "error", err)
		os.Exit(1)
	}
	slog.Info("created case", "case_id", caseID)

	// Create Outbox Event
	// jobID := "job-" + caseID // Simple dummy ID
	err = repo.InsertOutboxEvent(ctx, db.Pool, model.EventTypeTaskCompleted, map[string]interface{}{
		"case_id": caseID,
		"task_id": "task-123", // Dummy
	})
	if err != nil {
		slog.Error("failed to create event", "error", err)
		os.Exit(1)
	}
	slog.Info("inserted TASK_COMPLETED event")

	// 3. Start Engine in Background
	eng := engine.NewEngine(repo, assignment.NewManager(repo), 2)
	ctxCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	go eng.Start(ctxCancel)
	slog.Info("engine started")

	// 4. Poll for Result (Expect transition to CLOSED as there is no Stage 2)
	slog.Info("waiting for transition")
	for i := 0; i < 10; i++ {
		var status string
		var currentStageID *int64
		err := db.Pool.QueryRow(ctx, `SELECT global_status, current_stage_id FROM cases WHERE id = $1`, caseID).Scan(&status, &currentStageID)
		if err != nil {
			slog.Error("error checking case", "error", err)
		}

		if status == "CLOSED" {
			slog.Info("SUCCESS! Case transitioned to CLOSED", "case_id", caseID)
			return
		}

		time.Sleep(1 * time.Second)
	}

	slog.Error("timed out waiting for case transition")
	os.Exit(1)
}
