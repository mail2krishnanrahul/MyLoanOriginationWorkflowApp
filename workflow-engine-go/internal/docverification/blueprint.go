package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BlueprintCaseTypeConfig is the full JSONB config for HOME_LOAN_DOC_VERIFICATION.
// It defines 7 sequential stages, each with activities and task definitions.
var BlueprintCaseTypeConfig = model.CaseTypeConfig{
	DefaultCalendarID: "AUS_BUSINESS_HOURS",
	SLA: &model.SLAHierarchyConfig{
		Case: &model.SLADefinition{DurationHours: 120, WarningThresholdPct: 75, CriticalThresholdPct: 90},
	},
	DocumentTypes: []model.DocumentTypeDefinition{
		{DocumentTypeCode: "IDENTITY_VERIFICATION", DisplayName: "Identity Documents", AllowedExtensions: []string{"pdf", "jpg", "jpeg", "png"}, MaxSizeMB: 20, RequiresVerification: true, VerificationRole: RoleLoanOfficer},
		{DocumentTypeCode: "INCOME_VERIFICATION", DisplayName: "Income Evidence", AllowedExtensions: []string{"pdf"}, MaxSizeMB: 30, RequiresVerification: true, VerificationRole: RoleLoanOfficer},
		{DocumentTypeCode: "SECURITY_DOCUMENTS", DisplayName: "Security Documents", AllowedExtensions: []string{"pdf"}, MaxSizeMB: 50, RequiresVerification: true, VerificationRole: RoleLoanOfficer},
		{DocumentTypeCode: "LOAN_AGREEMENT", DisplayName: "Loan Agreement", AllowedExtensions: []string{"pdf"}, MaxSizeMB: 20, RequiresVerification: true},
		{DocumentTypeCode: "CREDIT_MEMO", DisplayName: "Credit Memo", AllowedExtensions: []string{"pdf"}, MaxSizeMB: 20, RequiresVerification: true, VerificationRole: RoleTeamLead},
	},
	AggregationRules: []model.AggregationRule{
		{TargetField: "classification_completed_at", SourceTask: "CLASSIFY_CASE", SourceField: "completed_at", OnTaskComplete: true},
		{TargetField: "qa_approved_at", SourceTask: "QA_APPROVE", SourceField: "completed_at", OnTaskComplete: true},
	},
	Stages: []model.StageDefinitionV2{
		{
			Code: StageIntake, Name: "Intake & Validation", SequenceOrder: 1,
			SLA: &model.SLADefinition{DurationHours: 4},
			Activities: []model.ActivityConfig{
				{
					Code: "INTAKE_VALIDATION", Name: "Intake Validation", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "VALIDATE_DEAL_REF", Name: "Validate Deal Reference", Type: model.TaskTypeSystem, Required: true, SequenceOrder: 1,
							Config: json.RawMessage(`{"endpoint":"/internal/validate-deal-ref","timeout_seconds":30}`)},
						{Code: "FETCH_INITIAL_DOCS", Name: "Fetch Initial Documents from ECM", Type: model.TaskTypeSystem, Required: true, SequenceOrder: 2,
							Config: json.RawMessage(`{"endpoint":"/internal/ecm/fetch-case-docs","timeout_seconds":60}`)},
					},
				},
			},
		},
		{
			Code: StageClassification, Name: "Case Classification", SequenceOrder: 2,
			SLA: &model.SLADefinition{DurationHours: 2},
			Activities: []model.ActivityConfig{
				{
					Code: "CLASSIFY", Name: "Classification", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "CLASSIFY_CASE", Name: "Classify Case Complexity & Skills", Type: model.TaskTypeUser, Required: true, SequenceOrder: 1,
							Config: json.RawMessage(`{"form":"case_classification_form","skill_selector":true}`)},
					},
				},
			},
		},
		{
			Code: StageAllocation, Name: "Work Allocation", SequenceOrder: 3,
			SLA: &model.SLADefinition{DurationHours: 1},
			Activities: []model.ActivityConfig{
				{
					Code: "ALLOCATE", Name: "Case Allocation", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "ALLOCATE_CASE", Name: "Allocate Case to Loan Officer", Type: model.TaskTypeSystem, Required: true, SequenceOrder: 1,
							Config: json.RawMessage(`{"strategy":"SKILL_SCORE_THEN_MANUAL","workbasket":"DOC_VER_QUEUE"}`)},
					},
				},
			},
		},
		{
			Code: StageDocumentVerification, Name: "Document Verification", SequenceOrder: 4,
			SLA: &model.SLADefinition{DurationHours: 48},
			Activities: []model.ActivityConfig{
				{
					Code: "DOCUMENT_REVIEW", Name: "Document Review", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "REVIEW_IDENTITY_DOCS", Name: "Review Identity Documents", Type: model.TaskTypeUser, Required: true, SequenceOrder: 1, IsDocumentVerification: true, DocumentTypeCode: "IDENTITY_VERIFICATION",
							SLA: &model.SLADefinition{DurationHours: 8}},
						{Code: "REVIEW_INCOME_DOCS", Name: "Review Income Evidence", Type: model.TaskTypeUser, Required: true, SequenceOrder: 2, IsDocumentVerification: true, DocumentTypeCode: "INCOME_VERIFICATION",
							SLA: &model.SLADefinition{DurationHours: 8}},
						{Code: "REVIEW_SECURITY_DOCS", Name: "Review Security Documents", Type: model.TaskTypeUser, Required: true, SequenceOrder: 3, IsDocumentVerification: true, DocumentTypeCode: "SECURITY_DOCUMENTS",
							SLA: &model.SLADefinition{DurationHours: 8}},
						{Code: "CROSS_CHECK_CREDIT_MEMO", Name: "Cross-check Credit Memo", Type: model.TaskTypeUser, Required: true, SequenceOrder: 4,
							Config: json.RawMessage(`{"requires_checklist":true,"checklist_version":1}`),
							SLA:    &model.SLADefinition{DurationHours: 4}},
						{Code: "REVIEW_DEAL_STRUCTURE", Name: "Review Deal Structure", Type: model.TaskTypeUser, Required: true, SequenceOrder: 5,
							SLA: &model.SLADefinition{DurationHours: 4}},
					},
				},
			},
		},
		{
			Code: StageAdditionalInfo, Name: "Additional Information Request", SequenceOrder: 5,
			SLA: &model.SLADefinition{DurationHours: 72},
			Activities: []model.ActivityConfig{
				{
					Code: "ADDITIONAL_INFO", Name: "Additional Info Collection", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "REQUEST_ADDITIONAL_INFO", Name: "Request Additional Info from Banker", Type: model.TaskTypeUser, Required: false, SequenceOrder: 1},
						{Code: "AWAIT_BANKER_RESUBMISSION", Name: "Await Banker Resubmission", Type: model.TaskTypeSystem, Required: false, SequenceOrder: 2},
					},
				},
			},
		},
		{
			Code: StageQAReview, Name: "QA Review", SequenceOrder: 6,
			SLA: &model.SLADefinition{DurationHours: 16},
			Activities: []model.ActivityConfig{
				{
					Code: "QA", Name: "Quality Assurance", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "QA_LOCK", Name: "QA Lock Case", Type: model.TaskTypeSystem, Required: true, SequenceOrder: 1},
						{Code: "QA_REVIEW", Name: "QA Officer Review", Type: model.TaskTypeUser, Required: true, SequenceOrder: 2,
							Config: json.RawMessage(`{"workbasket":"QA_QUEUE","required_role":"QA_OFFICER"}`)},
						{Code: "QA_APPROVE_OR_REJECT", Name: "QA Approve or Reject", Type: model.TaskTypeUser, Required: true, SequenceOrder: 3},
					},
				},
			},
		},
		{
			Code: StageQARemediation, Name: "QA Remediation", SequenceOrder: 7,
			SLA: &model.SLADefinition{DurationHours: 24},
			Activities: []model.ActivityConfig{
				{
					Code: "REMEDIATION", Name: "Remediation", SequenceOrder: 1,
					TaskDefs: []model.TaskDefinitionV2{
						{Code: "UNLOCK_FOR_REMEDIATION", Name: "Unlock Case for Loan Officer", Type: model.TaskTypeSystem, Required: true, SequenceOrder: 1},
						{Code: "REMEDIATE_AND_RESUBMIT", Name: "Loan Officer Remediation & Resubmission", Type: model.TaskTypeUser, Required: true, SequenceOrder: 2},
					},
				},
			},
		},
	},
}

// RegisterDocVerificationCaseType inserts the HOME_LOAN_DOC_VERIFICATION
// CaseType blueprint into the database if it doesn't already exist.
// Idempotent — safe to call on every startup.
func RegisterDocVerificationCaseType(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	configBytes, err := json.Marshal(BlueprintCaseTypeConfig)
	if err != nil {
		return "", fmt.Errorf("RegisterDocVerificationCaseType: marshal config: %w", err)
	}

	var caseTypeID string
	err = pool.QueryRow(ctx, `
		INSERT INTO case_types (code, version, name, description, config, status)
		VALUES ($1, 1, $2, $3, $4, 'ACTIVE')
		ON CONFLICT (code, version) DO UPDATE
		    SET name        = EXCLUDED.name,
		        description = EXCLUDED.description,
		        config      = EXCLUDED.config,
		        updated_at  = now()
		RETURNING id::text
	`,
		CaseTypeCode,
		"Home Loan Document Verification",
		"Manages end-to-end document verification for home loan origination, from intake through QA sign-off.",
		configBytes,
	).Scan(&caseTypeID)
	if err != nil {
		return "", fmt.Errorf("RegisterDocVerificationCaseType: upsert: %w", err)
	}

	slog.Info("case type blueprint registered",
		"code", CaseTypeCode,
		"id", caseTypeID,
		"stages", len(BlueprintCaseTypeConfig.Stages),
		"registered_at", time.Now().UTC())

	return caseTypeID, nil
}
