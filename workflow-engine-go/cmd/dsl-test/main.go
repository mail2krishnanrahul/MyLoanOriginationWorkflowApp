package main

import (
	"context"
	"log/slog"
	"os"

	"workflow-engine/internal/database"
	"workflow-engine/internal/engine"
	"workflow-engine/internal/engine/assignment"
	"workflow-engine/internal/parser"
	"workflow-engine/internal/repository"
)

var sampleYAML = `
id: "loan_application_v1"
version: 1
case_type: "personal_loan"
description: "Standard personal loan with document upload"
stages:
  - name: "ApplicationEntry"
    pre_hooks:
      - name: "NotifyStart"
        type: "NOTIFY"
        config:
           channel: "email"
    tasks:
      - name: "UploadID"
        type: "USER"
        ui_config:
           form: "UploadForm"
           fields: ["id_card", "pay_slip"]
    routing:
      next_stage: "CreditCheck"
  - name: "CreditCheck"
    tasks:
      - name: "RunCreditScore"
        type: "SYSTEM"
        integration_endpoint: "TOPIC_CREDIT_SCORE_REQ"
    routing:
      next_stage: "Approval"
  - name: "Approval"
    tasks: []
`

func main() {
	// 1. Test Parser
	reg := parser.NewRegistry()
	wf, err := reg.Load([]byte(sampleYAML))
	if err != nil {
		slog.Error("parser failed", "error", err)
		os.Exit(1)
	}
	slog.Info("parser success", "workflow_id", wf.ID, "version", wf.Version, "stages", len(wf.Stages))

	// 2. Test Activation Logic (Integration)
	connString := "postgres://myappuser:password@localhost:5432/LoanOriginationDB" // Params
	ctx := context.Background()
	db, err := database.Connect(ctx, connString)
	if err != nil {
		slog.Error("DB connect failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Apply Schema Updates (Task Definitions for DSL support)
	// We need to ensure we can insert task_definitions with NULL stage_definition_id as per our implementation assumption
	// Or we make it nullable.

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

	CREATE TABLE task_definitions (
		id BIGSERIAL PRIMARY KEY,
		stage_definition_id BIGINT REFERENCES stage_definitions(id),
		name VARCHAR(255) NOT NULL,
		task_type VARCHAR(50) NOT NULL, 
		required_data_schema JSONB,
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

	CREATE TABLE task_instances (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		case_id UUID REFERENCES cases(id),
		task_definition_id BIGINT REFERENCES task_definitions(id),
		status VARCHAR(50) NOT NULL, -- PENDING, DONE
		assigned_to VARCHAR(255),
		data_payload JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP
	);
	`
	_, err = db.Pool.Exec(ctx, schemaSQL)
	if err != nil {
		slog.Error("failed to create schema", "error", err)
		os.Exit(1)
	}

	_, err = db.Pool.Exec(ctx, `ALTER TABLE task_definitions ALTER COLUMN stage_definition_id DROP NOT NULL;`)
	if err != nil {
		slog.Warn("failed to alter table, might already be nullable", "error", err)
	}

	repo := repository.NewRepository(db.Pool)
	eng := engine.NewEngine(repo, assignment.NewManager(repo), 1)

	// Create a Dummy Case
	var caseID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO cases (global_status, version) VALUES ('OPEN', 1) RETURNING id`).Scan(&caseID)
	if err != nil {
		slog.Error("failed to create dummy case", "error", err)
		os.Exit(1)
	}

	// Activate Stage 1
	slog.Info("testing activation")
	err = eng.ActivateStage(ctx, db.Pool, caseID, &wf.Stages[0])
	if err != nil {
		slog.Error("activation failed", "error", err)
		os.Exit(1)
	}
	slog.Info("activation success, checking DB")

	// Verify Task Instances
	var count int
	err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM task_instances WHERE case_id = $1::uuid`, caseID).Scan(&count)
	if err != nil {
		slog.Error("DB check failed", "error", err)
		os.Exit(1)
	}
	if count != 1 {
		slog.Error("expected 1 task instance", "found", count)
		os.Exit(1)
	}
	slog.Info("confirmed task instances created", "count", count)

	slog.Info("DSL & activation verification complete")
}
