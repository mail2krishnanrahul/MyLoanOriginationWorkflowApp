package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/internal/repository"
	"workflow-engine/internal/sla"
	"workflow-engine/pkg/model"
)

// ---------------------------------------------------------------------------
// Request / Response DTOs
// ---------------------------------------------------------------------------

// CreateCaseRequest is the input for creating a new loan-origination case.
type CreateCaseRequest struct {
	CaseTypeCode    string          `json:"case_type_code"`
	CaseTypeVersion int             `json:"case_type_version"` // 0 = latest ACTIVE
	Metadata        json.RawMessage `json:"metadata"`          // borrower_id, product_id, channel, etc.
	RequestedBy     string          `json:"requested_by"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"` // optional dedup guard
}

// CreateCaseResponse is returned after successful case creation.
type CreateCaseResponse struct {
	CaseID          string `json:"case_id"`
	ReferenceNumber string `json:"reference_number"`
	CaseTypeCode    string `json:"case_type_code"`
	CaseTypeVersion int    `json:"case_type_version"`
	InitialStage    string `json:"initial_stage"`
	TasksCreated    int    `json:"tasks_created"`
}

// ---------------------------------------------------------------------------
// CreateCase — fully transactional case creation
// ---------------------------------------------------------------------------

// CreateCase is the entry point for a new loan application. It:
// 1. Validates the case_type is ACTIVE
// 2. Creates the Case row (status=OPEN)
// 3. Creates any sub-cases defined in the config
// 4. Transitions to the ENTRY stage
// 5. Creates all PENDING tasks for the ENTRY stage
// 6. Publishes a CASE_CREATED event
// All within a single database transaction.
func CreateCase(
	ctx context.Context,
	repo *repository.Repository,
	req CreateCaseRequest,
) (CreateCaseResponse, error) {

	// 1. Validate case_type
	caseType, err := repo.GetCaseTypeByCodeAndVersion(ctx, nil, req.CaseTypeCode, req.CaseTypeVersion)
	if err != nil {
		return CreateCaseResponse{}, fmt.Errorf("invalid case_type: %w", err)
	}

	if len(caseType.Config.Stages) == 0 {
		return CreateCaseResponse{}, fmt.Errorf("case_type %s has no stages defined", caseType.Code)
	}

	entryStage := caseType.Config.Stages[0] // First stage is the entry point

	// 2. Begin transaction
	tx, err := repo.Pool.Begin(ctx)
	if err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 3. Create the case
	metadata := req.Metadata
	if metadata == nil {
		metadata = json.RawMessage("{}")
	}

	caseInstance := &model.CaseInstance{
		CaseTypeID:          caseType.ID,
		CaseTypeVersion:     caseType.Version,
		CurrentStageCode:    &entryStage.Code,
		CurrentStageOrdinal: entryStage.SequenceOrder,
		Status:              model.CaseStatusOpen,
		Metadata:            metadata,
	}

	if err := repo.InsertCaseInstance(ctx, tx, caseInstance); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to insert case: %w", err)
	}

	if caseType.Config.MaxReworkAttempts > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE cases
			SET max_rework_attempts = $1,
			    updated_at = now(),
			    row_version = row_version + 1
			WHERE id = $2::uuid
		`, caseType.Config.MaxReworkAttempts, caseInstance.ID); err != nil {
			return CreateCaseResponse{}, fmt.Errorf("failed to apply max_rework_attempts: %w", err)
		}
	}

	// Initialize immutable case-level SLA snapshot at creation time.
	if err := applyCaseSLAAtCreation(ctx, tx, repo, caseInstance.ID, caseType.Config); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to initialize case SLA: %w", err)
	}

	slog.Info("case created",
		"case_id", caseInstance.ID,
		"reference_number", caseInstance.ReferenceNumber,
		"case_type", caseType.Code,
		"version", caseType.Version)

	// 4. Create sub-cases (if config defines them)
	if err := CreateSubCases(ctx, tx, repo, *caseInstance, caseType.Config); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to create sub-cases: %w", err)
	}

	// 5. Record entry stage transition
	if err := repo.RecordStageTransition(ctx, tx, model.TransitionInput{
		CaseID:         caseInstance.ID,
		ToStageCode:    entryStage.Code,
		ToStageOrdinal: entryStage.SequenceOrder,
		TriggeredBy:    req.RequestedBy,
	}); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to record entry stage transition: %w", err)
	}

	// 6. Create tasks for the entry stage
	tasksCreated, err := CreateTasksForStage(ctx, tx, repo,
		caseInstance.ID, entryStage.Code, entryStage, caseType.Config)
	if err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to create entry tasks: %w", err)
	}

	slog.Info("entry stage tasks created", "count", tasksCreated, "stage", entryStage.Code)

	// 7. Publish CASE_CREATED event
	eventPayload, _ := json.Marshal(map[string]interface{}{
		"case_id":           caseInstance.ID,
		"reference_number":  caseInstance.ReferenceNumber,
		"case_type_code":    caseType.Code,
		"case_type_version": caseType.Version,
		"initial_stage":     entryStage.Code,
		"requested_by":      req.RequestedBy,
	})

	if err := repo.PublishEvent(ctx, tx, model.Event{
		CaseID:        &caseInstance.ID,
		EventType:     model.EventCaseCreated,
		Payload:       eventPayload,
		TargetService: "case-orchestrator",
	}); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to publish CASE_CREATED: %w", err)
	}

	// Audit: case created (non-critical — log on failure)
	if auditErr := repo.InsertAuditEntry(ctx, tx, model.AuditEntry{
		CaseID:      caseInstance.ID,
		Action:      model.AuditCaseCreated,
		EntityType:  model.AuditEntityCase,
		ActorID:     req.RequestedBy,
		ActorType:   model.AuditActorAPI,
		ChangeDelta: eventPayload,
	}); auditErr != nil {
		slog.Warn("audit insert failed", "error", auditErr, "action", model.AuditCaseCreated)
	}

	// 9. Commit
	if err := tx.Commit(ctx); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to commit: %w", err)
	}

	return CreateCaseResponse{
		CaseID:          caseInstance.ID,
		ReferenceNumber: caseInstance.ReferenceNumber,
		CaseTypeCode:    caseType.Code,
		CaseTypeVersion: caseType.Version,
		InitialStage:    entryStage.Code,
		TasksCreated:    tasksCreated,
	}, nil
}

// ---------------------------------------------------------------------------
// CreateSubCases — creates child cases for sub-case types in config
// ---------------------------------------------------------------------------

// CreateSubCases checks the case_type config for sub_case_types and creates
// child cases linked via parent_case_id. Sub-case types are referenced by
// code in the config (e.g. "sub_case_types": ["CREDIT_CHECK", "VALUATION"]).
func CreateSubCases(
	ctx context.Context,
	tx repository.DBExecutor,
	repo *repository.Repository,
	parentCase model.CaseInstance,
	config model.CaseTypeConfig,
) error {
	if len(config.SubCaseTypes) == 0 {
		return nil
	}

	for _, subCode := range config.SubCaseTypes {
		subCaseType, err := repo.GetCaseTypeByCodeAndVersion(ctx, tx, subCode, 0)
		if err != nil {
			slog.Warn("sub-case type not found, skipping", "sub_case_type", subCode, "error", err)
			continue
		}

		var initialStageCode *string
		var initialOrdinal int
		if len(subCaseType.Config.Stages) > 0 {
			initialStageCode = &subCaseType.Config.Stages[0].Code
			initialOrdinal = subCaseType.Config.Stages[0].SequenceOrder
		}

		subCase := &model.CaseInstance{
			CaseTypeID:          subCaseType.ID,
			CaseTypeVersion:     subCaseType.Version,
			ParentCaseID:        &parentCase.ID,
			CurrentStageCode:    initialStageCode,
			CurrentStageOrdinal: initialOrdinal,
			Status:              model.CaseStatusOpen,
			Metadata:            parentCase.Metadata, // inherit parent metadata
		}

		if err := repo.InsertCaseInstance(ctx, tx, subCase); err != nil {
			return fmt.Errorf("failed to create sub-case %s: %w", subCode, err)
		}

		if subCaseType.Config.MaxReworkAttempts > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE cases
				SET max_rework_attempts = $1,
				    updated_at = now(),
				    row_version = row_version + 1
				WHERE id = $2::uuid
			`, subCaseType.Config.MaxReworkAttempts, subCase.ID); err != nil {
				return fmt.Errorf("failed to apply sub-case max_rework_attempts %s: %w", subCode, err)
			}
		}

		if err := applyCaseSLAAtCreation(ctx, tx, repo, subCase.ID, subCaseType.Config); err != nil {
			return fmt.Errorf("failed to initialize sub-case SLA %s: %w", subCode, err)
		}

		slog.Info("sub-case created",
			"sub_case_id", subCase.ID,
			"reference_number", subCase.ReferenceNumber,
			"sub_case_type", subCode,
			"parent_case_id", parentCase.ID)
	}

	return nil
}

func applyCaseSLAAtCreation(
	ctx context.Context,
	tx repository.DBExecutor,
	repo *repository.Repository,
	caseID string,
	config model.CaseTypeConfig,
) error {
	def, err := sla.ResolveEffectiveSLADefinition(config, "", "", "")
	if err != nil {
		return fmt.Errorf("applyCaseSLAAtCreation: %w", err)
	}
	if def == nil {
		return nil
	}
	if repo.SQLX == nil {
		return fmt.Errorf("applyCaseSLAAtCreation: sqlx db is not configured")
	}

	dueAt, durationMS, calendarID, err := sla.ComputeSLADeadline(
		ctx,
		repo.SQLX,
		time.Now().UTC(),
		config.DefaultCalendarID,
		*def,
	)
	if err != nil {
		return fmt.Errorf("applyCaseSLAAtCreation: compute deadline: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE cases
		SET case_due_at = $1,
		    case_effective_start_time = now(),
		    case_sla_duration_ms = $2,
		    case_sla_warning_threshold_pct = $3,
		    case_sla_critical_threshold_pct = $4,
		    case_sla_breach_action = $5,
		    case_sla_calendar_id = $6::uuid,
		    updated_at = now(),
		    row_version = row_version + 1
		WHERE id = $7::uuid
	`, dueAt, durationMS, def.WarningThresholdPct, def.CriticalThresholdPct, string(def.BreachAction), calendarID, caseID)
	if err != nil {
		return fmt.Errorf("applyCaseSLAAtCreation: update case: %w", err)
	}

	return nil
}
