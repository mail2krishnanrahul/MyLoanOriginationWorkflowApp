package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"
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
	tenantID, tenantErr := multitenancy.TenantFromContext(ctx)
	if tenantErr != nil {
		tenantID = multitenancy.DefaultTenantID
		ctx = multitenancy.WithTenant(ctx, tenantID)
	}
	if repo.SQLX != nil {
		if err := multitenancy.AssertTenantOperational(ctx, repo.SQLX, tenantID); err != nil {
			return CreateCaseResponse{}, fmt.Errorf("CreateCase: tenant not operational: %w", err)
		}
		if err := multitenancy.EnforceTenantCaseLimits(ctx, repo.SQLX, tenantID); err != nil {
			return CreateCaseResponse{}, fmt.Errorf("CreateCase: tenant capacity exceeded: %w", err)
		}
		visible, err := multitenancy.IsCaseTypeVisibleToTenant(ctx, repo.SQLX, req.CaseTypeCode, tenantID)
		if err != nil {
			return CreateCaseResponse{}, fmt.Errorf("CreateCase: resolve case type visibility: %w", err)
		}
		if !visible {
			return CreateCaseResponse{}, fmt.Errorf("CreateCase: %w", multitenancy.ErrCaseTypeForbidden)
		}
	}

	// 1. Validate case_type
	caseType, err := repo.GetCaseTypeByCodeAndVersion(ctx, nil, req.CaseTypeCode, req.CaseTypeVersion)
	if err != nil {
		return CreateCaseResponse{}, fmt.Errorf("invalid case_type: %w", err)
	}
	if strings.ToUpper(strings.TrimSpace(caseType.Status)) != model.CaseTypeStatusActive {
		return CreateCaseResponse{}, fmt.Errorf("case_type %s v%d is not ACTIVE", caseType.Code, caseType.Version)
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
		TenantID:            tenantID,
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

	// Materialize document type definitions and create placeholder requests
	// for requirements at the entry stage.
	if err := materializeDocumentConfigAndRequests(ctx, tx, caseInstance.ID, *caseType, entryStage.Code); err != nil {
		return CreateCaseResponse{}, fmt.Errorf("failed to initialize document requirements: %w", err)
	}

	slog.Info("case created",
		"tenant_id", tenantID,
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
	multitenancy.IncCasesCreated(tenantID, caseType.Code)

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
			TenantID:            parentCase.TenantID,
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

func materializeDocumentConfigAndRequests(
	ctx context.Context,
	tx repository.DBExecutor,
	caseID string,
	caseType model.CaseType,
	entryStageCode string,
) error {
	for _, definition := range caseType.Config.DocumentTypes {
		documentTypeCode := strings.TrimSpace(definition.DocumentTypeCode)
		if documentTypeCode == "" {
			return fmt.Errorf("materializeDocumentConfigAndRequests: document_type_code is required")
		}
		allowedExtensions := normalizeExtensions(definition.AllowedExtensions)
		if len(allowedExtensions) == 0 {
			return fmt.Errorf("materializeDocumentConfigAndRequests: allowed_extensions is required for %s", documentTypeCode)
		}

		maxSizeMB := definition.MaxSizeMB
		if maxSizeMB <= 0 {
			maxSizeMB = 10
		}
		requiredCountMin := definition.RequiredCountMin
		if requiredCountMin <= 0 {
			requiredCountMin = 1
		}
		requiredCountMax := definition.RequiredCountMax
		if requiredCountMax <= 0 || requiredCountMax < requiredCountMin {
			requiredCountMax = requiredCountMin
		}
		retentionDays := definition.RetentionDays
		if retentionDays <= 0 {
			retentionDays = 2555
		}
		retentionPolicy := strings.ToUpper(strings.TrimSpace(definition.RetentionPolicy))
		if retentionPolicy == "" {
			retentionPolicy = "ARCHIVE"
		}

		allowedViewers := normalizeRoles(definition.AllowedViewers)
		if len(allowedViewers) == 0 {
			allowedViewers = []string{"PUBLIC"}
		}

		var requiredAtStage interface{}
		if stage := strings.TrimSpace(definition.RequiredAtStage); stage != "" {
			requiredAtStage = stage
		}
		var verificationRole interface{}
		if definition.RequiresVerification {
			role := strings.TrimSpace(definition.VerificationRole)
			if role == "" {
				role = "DOCUMENT_REVIEWER"
			}
			verificationRole = role
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO document_types (
				case_type_code,
				case_type_version,
				document_type_code,
				display_name,
				description,
				allowed_extensions,
				max_size_mb,
				required_at_stage,
				required_count_min,
				required_count_max,
				is_sensitive,
				requires_verification,
				verification_role,
				retention_days,
				retention_policy,
				allowed_viewers
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				$5,
				$6,
				$7,
				$8,
				$9,
				$10,
				$11,
				$12,
				$13,
				$14,
				$15,
				$16
			)
			ON CONFLICT (case_type_code, case_type_version, document_type_code)
			DO UPDATE SET
				display_name = EXCLUDED.display_name,
				description = EXCLUDED.description,
				allowed_extensions = EXCLUDED.allowed_extensions,
				max_size_mb = EXCLUDED.max_size_mb,
				required_at_stage = EXCLUDED.required_at_stage,
				required_count_min = EXCLUDED.required_count_min,
				required_count_max = EXCLUDED.required_count_max,
				is_sensitive = EXCLUDED.is_sensitive,
				requires_verification = EXCLUDED.requires_verification,
				verification_role = EXCLUDED.verification_role,
				retention_days = EXCLUDED.retention_days,
				retention_policy = EXCLUDED.retention_policy,
				allowed_viewers = EXCLUDED.allowed_viewers,
				updated_at = now()
		`,
			caseType.Code,
			caseType.Version,
			documentTypeCode,
			strings.TrimSpace(definition.DisplayName),
			blankToNil(definition.Description),
			allowedExtensions,
			maxSizeMB,
			requiredAtStage,
			requiredCountMin,
			requiredCountMax,
			definition.IsSensitive,
			definition.RequiresVerification,
			verificationRole,
			retentionDays,
			retentionPolicy,
			allowedViewers,
		); err != nil {
			return fmt.Errorf("materializeDocumentConfigAndRequests: upsert %s: %w", documentTypeCode, err)
		}
	}

	if strings.TrimSpace(entryStageCode) == "" {
		return nil
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO document_requests (
			case_id,
			case_type_code,
			case_type_version,
			document_type_code,
			required_at_stage,
			required_count_min,
			required_count_max,
			current_count,
			status,
			requested_at
		)
		SELECT
			$1::uuid,
			dt.case_type_code,
			dt.case_type_version,
			dt.document_type_code,
			dt.required_at_stage,
			dt.required_count_min,
			dt.required_count_max,
			0,
			'PENDING',
			now()
		FROM document_types dt
		WHERE dt.case_type_code = $2
		  AND dt.case_type_version = $3
		  AND dt.required_at_stage = $4
		  AND dt.required_count_min > 0
		ON CONFLICT (case_id, document_type_code, required_at_stage)
		DO NOTHING
	`, caseID, caseType.Code, caseType.Version, entryStageCode); err != nil {
		return fmt.Errorf("materializeDocumentConfigAndRequests: insert requests: %w", err)
	}
	return nil
}

func normalizeExtensions(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(strings.TrimPrefix(value, ".")))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeRoles(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		role := strings.ToUpper(strings.TrimSpace(value))
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func blankToNil(value string) interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
