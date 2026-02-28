package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// CaseComplexity — typed enum for complexity tier
// ---------------------------------------------------------------------------

type CaseComplexity string

const (
	CaseComplexitySimple      CaseComplexity = "SIMPLE"
	CaseComplexityStandard1   CaseComplexity = "STANDARD_1"
	CaseComplexityStandard2   CaseComplexity = "STANDARD_2"
	CaseComplexityStandard3   CaseComplexity = "STANDARD_3"
	CaseComplexityNonStandard CaseComplexity = "NON_STANDARD"
)

func ValidCaseComplexity(c CaseComplexity) bool {
	switch c {
	case CaseComplexitySimple, CaseComplexityStandard1, CaseComplexityStandard2,
		CaseComplexityStandard3, CaseComplexityNonStandard:
		return true
	}
	return false
}

// ComplexityScore returns the prioritisation score for GetNext sorting.
func ComplexityScore(c CaseComplexity) int {
	switch c {
	case CaseComplexityNonStandard:
		return 50
	case CaseComplexityStandard3:
		return 40
	case CaseComplexityStandard2:
		return 30
	case CaseComplexityStandard1:
		return 20
	default:
		return 10
	}
}

// ---------------------------------------------------------------------------
// SkillCode — typed enum for skill requirements
// ---------------------------------------------------------------------------

type SkillCode string

const (
	SkillResidentialLending  SkillCode = "RESIDENTIAL_LENDING"
	SkillCommercialLending   SkillCode = "COMMERCIAL_LENDING"
	SkillTrustStructures     SkillCode = "TRUST_STRUCTURES"
	SkillCompanyBorrowers    SkillCode = "COMPANY_BORROWERS"
	SkillSMSFLending         SkillCode = "SMSF_LENDING"
	SkillConstructionFinance SkillCode = "CONSTRUCTION_FINANCE"
	SkillForeignNationals    SkillCode = "FOREIGN_NATIONALS"
	SkillHighNetWorth        SkillCode = "HIGH_NET_WORTH"
	SkillPanelSolicitorCoord SkillCode = "PANEL_SOLICITOR_COORD"
	SkillCreditMemoReview    SkillCode = "CREDIT_MEMO_REVIEW"
	SkillQAReview            SkillCode = "QA_REVIEW"
)

func ValidSkillCode(s SkillCode) bool {
	switch s {
	case SkillResidentialLending, SkillCommercialLending, SkillTrustStructures,
		SkillCompanyBorrowers, SkillSMSFLending, SkillConstructionFinance,
		SkillForeignNationals, SkillHighNetWorth, SkillPanelSolicitorCoord,
		SkillCreditMemoReview, SkillQAReview:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// DocumentCategory — types of documents fetched from ECM
// ---------------------------------------------------------------------------

type DocumentCategory string

const (
	DocCategoryIdentity        DocumentCategory = "IDENTITY_VERIFICATION"
	DocCategoryIncome          DocumentCategory = "INCOME_VERIFICATION"
	DocCategorySecurity        DocumentCategory = "SECURITY_DOCUMENTS"
	DocCategoryLoanAgreement   DocumentCategory = "LOAN_AGREEMENT"
	DocCategoryTrustDeed       DocumentCategory = "TRUST_DEED"
	DocCategoryCompanyDocs     DocumentCategory = "COMPANY_DOCUMENTS"
	DocCategoryCreditMemo      DocumentCategory = "CREDIT_MEMO"
	DocCategoryBankStatements  DocumentCategory = "BANK_STATEMENTS"
	DocCategorySolicitorReport DocumentCategory = "SOLICITOR_REPORT"
	DocCategoryInsurance       DocumentCategory = "INSURANCE"
	DocCategoryOther           DocumentCategory = "OTHER"
)

// ---------------------------------------------------------------------------
// CaseDocumentStatus — lifecycle of a case document in the ECM workflow.
// NOTE: DocumentStatus is already defined in model/document.go for a
// different purpose; this type is scoped to case-level ECM document tracking.
// ---------------------------------------------------------------------------

type CaseDocumentStatus string

const (
	CaseDocStatusPendingReview     CaseDocumentStatus = "PENDING_REVIEW"
	CaseDocStatusUnderReview       CaseDocumentStatus = "UNDER_REVIEW"
	CaseDocStatusReviewed          CaseDocumentStatus = "REVIEWED"
	CaseDocStatusErrorTagged       CaseDocumentStatus = "ERROR_TAGGED"
	CaseDocStatusAccepted          CaseDocumentStatus = "ACCEPTED"
	CaseDocStatusRequestedResubmit CaseDocumentStatus = "REQUESTED_RESUBMIT"
)

// ---------------------------------------------------------------------------
// DocumentErrorCode — typed codes for document error tags
// ---------------------------------------------------------------------------

type DocumentErrorCode string

const (
	DocErrMissingDoc          DocumentErrorCode = "DOC_MISSING"
	DocErrIllegible           DocumentErrorCode = "DOC_ILLEGIBLE"
	DocErrExpired             DocumentErrorCode = "DOC_EXPIRED"
	DocErrIncorrectEntity     DocumentErrorCode = "DOC_INCORRECT_ENTITY"
	DocErrIncorrectAmount     DocumentErrorCode = "DOC_INCORRECT_AMOUNT"
	DocErrUnsigned            DocumentErrorCode = "DOC_UNSIGNED"
	DocErrUndated             DocumentErrorCode = "DOC_UNDATED"
	DocErrIncomplete          DocumentErrorCode = "DOC_INCOMPLETE"
	DocErrWrongVersion        DocumentErrorCode = "DOC_WRONG_VERSION"
	DocErrCertifiedRequired   DocumentErrorCode = "DOC_CERTIFIED_REQUIRED"
	DocErrTranslationRequired DocumentErrorCode = "DOC_TRANSLATION_REQUIRED"
	DocErrDiscrepancy         DocumentErrorCode = "DOC_DISCREPANCY"
	DocErrOtherError          DocumentErrorCode = "OTHER_ERROR"
)

// ---------------------------------------------------------------------------
// ErrorSeverity — how severe a document error is
// ---------------------------------------------------------------------------

type ErrorSeverity string

const (
	ErrorSeverityMinor    ErrorSeverity = "MINOR"
	ErrorSeverityMajor    ErrorSeverity = "MAJOR"
	ErrorSeverityBlocking ErrorSeverity = "BLOCKING"
)

// ---------------------------------------------------------------------------
// CaseDocument — row in the case_documents table
// ---------------------------------------------------------------------------

type CaseDocument struct {
	DocumentID       string    `json:"document_id"       db:"document_id"`
	TenantID         string    `json:"tenant_id"         db:"tenant_id"`
	CaseID           string    `json:"case_id"           db:"case_id"`
	EcmDocumentID    string    `json:"ecm_document_id"   db:"ecm_document_id"`
	EcmReference     *string   `json:"ecm_reference"     db:"ecm_reference"`
	DocumentCategory string    `json:"document_category" db:"document_category"`
	DocumentName     string    `json:"document_name"     db:"document_name"`
	DocumentType     string    `json:"document_type"     db:"document_type"`
	PageCount        *int      `json:"page_count"        db:"page_count"`
	ReceivedAt       time.Time `json:"received_at"       db:"received_at"`
	// EcmURL is intentionally omitted from JSON — sensitive field; never log.
	EcmURL     string             `json:"-"                 db:"ecm_url"`
	Status     CaseDocumentStatus `json:"status"            db:"status"`
	ReviewedBy *string            `json:"reviewed_by"       db:"reviewed_by"`
	ReviewedAt *time.Time         `json:"reviewed_at"       db:"reviewed_at"`
	Metadata   json.RawMessage    `json:"metadata"          db:"metadata"`
	CreatedAt  time.Time          `json:"created_at"        db:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"        db:"updated_at"`
}

// ---------------------------------------------------------------------------
// DocumentErrorTag — row in the document_error_tags table
// ---------------------------------------------------------------------------

type DocumentErrorTag struct {
	TagID            string        `json:"tag_id"            db:"tag_id"`
	TenantID         string        `json:"tenant_id"         db:"tenant_id"`
	DocumentID       string        `json:"document_id"       db:"document_id"`
	CaseID           string        `json:"case_id"           db:"case_id"`
	ErrorCode        string        `json:"error_code"        db:"error_code"`
	ErrorDescription *string       `json:"error_description" db:"error_description"`
	Severity         ErrorSeverity `json:"severity"          db:"severity"`
	TaggedBy         string        `json:"tagged_by"         db:"tagged_by"`
	TaggedAt         time.Time     `json:"tagged_at"         db:"tagged_at"`
	ResolvedAt       *time.Time    `json:"resolved_at"       db:"resolved_at"`
	ResolvedBy       *string       `json:"resolved_by"       db:"resolved_by"`
	CreatedAt        time.Time     `json:"created_at"        db:"created_at"`
}

// AddErrorTagInput carries the validated inputs for AddDocumentErrorTag.
type AddErrorTagInput struct {
	TenantID         string
	DocumentID       string
	CaseID           string
	ErrorCode        DocumentErrorCode
	ErrorDescription *string
	Severity         ErrorSeverity
	TaggedBy         string
}

// ---------------------------------------------------------------------------
// CaseTag — row in the case_tags table
// ---------------------------------------------------------------------------

type CaseTag struct {
	TagID     string     `json:"tag_id"    db:"tag_id"`
	TenantID  string     `json:"tenant_id" db:"tenant_id"`
	CaseID    string     `json:"case_id"   db:"case_id"`
	TagCode   string     `json:"tag_code"  db:"tag_code"`
	TagValue  *string    `json:"tag_value" db:"tag_value"`
	TaggedBy  string     `json:"tagged_by" db:"tagged_by"`
	TaggedAt  time.Time  `json:"tagged_at" db:"tagged_at"`
	RemovedAt *time.Time `json:"removed_at" db:"removed_at"`
	RemovedBy *string    `json:"removed_by" db:"removed_by"`
}

// ---------------------------------------------------------------------------
// ChecklistItemStatus — state of one checklist item
// ---------------------------------------------------------------------------

type ChecklistItemStatus string

const (
	ChecklistItemNotChecked ChecklistItemStatus = "NOT_CHECKED"
	ChecklistItemPass       ChecklistItemStatus = "PASS"
	ChecklistItemFail       ChecklistItemStatus = "FAIL"
	ChecklistItemNA         ChecklistItemStatus = "N_A"
)

// ---------------------------------------------------------------------------
// ChecklistOverallStatus — aggregate result of the full checklist
// ---------------------------------------------------------------------------

type ChecklistOverallStatus string

const (
	ChecklistOverallIncomplete           ChecklistOverallStatus = "INCOMPLETE"
	ChecklistOverallPassed               ChecklistOverallStatus = "PASSED"
	ChecklistOverallFailed               ChecklistOverallStatus = "FAILED"
	ChecklistOverallPassedWithExceptions ChecklistOverallStatus = "PASSED_WITH_EXCEPTIONS"
)

// ---------------------------------------------------------------------------
// ChecklistItem — one item inside credit_memo_checklist.items JSONB
// ---------------------------------------------------------------------------

type ChecklistItem struct {
	ItemCode        string              `json:"item_code"`
	Label           string              `json:"label"`
	Section         string              `json:"section"`
	Status          ChecklistItemStatus `json:"status"`
	DiscrepancyNote *string             `json:"discrepancy_note,omitempty"`
	CheckedBy       string              `json:"checked_by"`
	CheckedAt       *time.Time          `json:"checked_at,omitempty"`
}

// ---------------------------------------------------------------------------
// CreditMemoChecklist — row in the credit_memo_checklist table
// ---------------------------------------------------------------------------

type CreditMemoChecklist struct {
	ChecklistID      string                 `json:"checklist_id"      db:"checklist_id"`
	TenantID         string                 `json:"tenant_id"         db:"tenant_id"`
	CaseID           string                 `json:"case_id"           db:"case_id"`
	TaskID           string                 `json:"task_id"           db:"task_id"`
	ChecklistVersion int                    `json:"checklist_version" db:"checklist_version"`
	Items            []ChecklistItem        `json:"items"             db:"items"`
	OverallStatus    ChecklistOverallStatus `json:"overall_status"    db:"overall_status"`
	CompletedBy      *string                `json:"completed_by"      db:"completed_by"`
	CompletedAt      *time.Time             `json:"completed_at"      db:"completed_at"`
	CreatedAt        time.Time              `json:"created_at"        db:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"        db:"updated_at"`
}

// ---------------------------------------------------------------------------
// AdditionalInfoRequest
// ---------------------------------------------------------------------------

type RequestedDocument struct {
	DocumentCategory DocumentCategory `json:"document_category"`
	Description      string           `json:"description"`
	IsMandatory      bool             `json:"is_mandatory"`
}

type AdditionalInfoRequest struct {
	RequestID          string              `json:"request_id"           db:"request_id"`
	TenantID           string              `json:"tenant_id"            db:"tenant_id"`
	CaseID             string              `json:"case_id"              db:"case_id"`
	RequestedBy        string              `json:"requested_by"         db:"requested_by"`
	RequestedDocuments []RequestedDocument `json:"requested_documents"  db:"requested_documents"`
	MessageToBanker    *string             `json:"message_to_banker"    db:"message_to_banker"`
	DueDate            time.Time           `json:"due_date"             db:"due_date"`
	Status             string              `json:"status"               db:"status"`
	BankerNotes        *string             `json:"banker_notes"         db:"banker_notes"`
	SubmittedEcmIDs    []string            `json:"submitted_ecm_ids"    db:"submitted_ecm_ids"`
	ReceivedAt         *time.Time          `json:"received_at"          db:"received_at"`
	CreatedAt          time.Time           `json:"created_at"           db:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"           db:"updated_at"`
}

type AdditionalInfoRequestInput struct {
	RequestedDocuments []RequestedDocument
	MessageToBanker    string
	DueDate            time.Time
}

type BankerResubmission struct {
	SubmittedEcmIDs []string
	BankerNotes     *string
}

// ---------------------------------------------------------------------------
// QAStagedChange
// ---------------------------------------------------------------------------

type QAStagedChangeType string

const (
	QAChangeDocumentUpdate     QAStagedChangeType = "DOCUMENT_UPDATE"
	QAChangeCaseMetadataUpdate QAStagedChangeType = "CASE_METADATA_UPDATE"
	QAChangeDocumentAdd        QAStagedChangeType = "DOCUMENT_ADD"
	QAChangeErrorTagUpdate     QAStagedChangeType = "ERROR_TAG_UPDATE"
)

type QAStagedChange struct {
	ChangeID    string             `json:"change_id"    db:"change_id"`
	TenantID    string             `json:"tenant_id"    db:"tenant_id"`
	CaseID      string             `json:"case_id"      db:"case_id"`
	ChangeType  QAStagedChangeType `json:"change_type"  db:"change_type"`
	Payload     json.RawMessage    `json:"payload"      db:"payload"`
	SubmittedBy string             `json:"submitted_by" db:"submitted_by"`
	SubmittedAt time.Time          `json:"submitted_at" db:"submitted_at"`
	Status      string             `json:"status"       db:"status"`
	AppliedAt   *time.Time         `json:"applied_at"   db:"applied_at"`
	AppliedBy   *string            `json:"applied_by"   db:"applied_by"`
}

// ---------------------------------------------------------------------------
// DealStructureView — read model for deal structure display
// ---------------------------------------------------------------------------

type BorrowingEntity struct {
	EntityName  string  `json:"entity_name"`
	EntityType  string  `json:"entity_type"`
	ABNACN      *string `json:"abn_acn,omitempty"`
	TrustName   *string `json:"trust_name,omitempty"`
	TrusteeName *string `json:"trustee_name,omitempty"`
}

type LoanFacility struct {
	FacilityID        string  `json:"facility_id"`
	FacilityType      string  `json:"facility_type"`
	LoanAmount        float64 `json:"loan_amount"`
	InterestRate      float64 `json:"interest_rate"`
	RateType          string  `json:"rate_type"`
	FixedPeriodMonths *int    `json:"fixed_period_months,omitempty"`
	RepaymentType     string  `json:"repayment_type"`
	IOPeriodMonths    *int    `json:"io_period_months,omitempty"`
	LoanTermYears     int     `json:"loan_term_years"`
	ProductName       string  `json:"product_name"`
	OffsetAccount     bool    `json:"offset_account"`
}

type SecurityProperty struct {
	SecurityAddress string  `json:"security_address"`
	SecurityType    string  `json:"security_type"`
	EstimatedValue  float64 `json:"estimated_value"`
	LVRPercentage   float64 `json:"lvr_percentage"`
	FirstMortgage   bool    `json:"first_mortgage"`
}

type LoanSummary struct {
	TotalExposure    float64  `json:"total_exposure"`
	TotalSecurity    float64  `json:"total_security"`
	BlendedLVR       float64  `json:"blended_lvr"`
	ServicingSurplus *float64 `json:"servicing_surplus,omitempty"`
}

type DealCondition struct {
	ConditionType   string `json:"condition_type"`
	ConditionText   string `json:"condition_text"`
	ConditionStatus string `json:"condition_status"`
}

type DealStructureView struct {
	BorrowingEntity    BorrowingEntity    `json:"borrowing_entity"`
	LoanFacilities     []LoanFacility     `json:"loan_facilities"`
	SecurityProperties []SecurityProperty `json:"security_properties"`
	LoanSummary        LoanSummary        `json:"loan_summary"`
	Conditions         []DealCondition    `json:"conditions"`
}

// ---------------------------------------------------------------------------
// CaseScorePreview — read model for GetNextCasePreview
// ---------------------------------------------------------------------------

type CaseScorePreview struct {
	CaseID          string         `json:"case_id"`
	ReferenceNumber string         `json:"reference_number"`
	Complexity      CaseComplexity `json:"complexity"`
	RequiredSkills  []SkillCode    `json:"required_skills"`
	TotalScore      float64        `json:"total_score"`
	SLAScore        float64        `json:"sla_score"`
	ComplexityScore float64        `json:"complexity_score"`
	SkillScore      float64        `json:"skill_score"`
	AgeScore        float64        `json:"age_score"`
	VIPScore        float64        `json:"vip_score"`
	SLARemainingHrs float64        `json:"sla_remaining_hours"`
}

// ---------------------------------------------------------------------------
// QARejectionItem — one issue identified during QA rejection
// ---------------------------------------------------------------------------

type QARejectionItem struct {
	Section             string `json:"section"`
	Issue               string `json:"issue"`
	Severity            string `json:"severity"`
	RemediationRequired string `json:"remediation_required"`
}

// ---------------------------------------------------------------------------
// AdHoc task input
// ---------------------------------------------------------------------------

type CreateAdHocTaskInput struct {
	CaseID            string
	DisplayName       string
	Description       string
	AssignedUserID    *string
	AssignedTeamID    *string
	DueDate           *time.Time
	IsBlocking        bool
	ActivityCode      string
	Priority          TaskPriority
	CreatedBy         string
	TenantID          string
	ExternalReference *string
}

// ---------------------------------------------------------------------------
// Sentinel errors for doc verification domain
// NOTE: We use fmt.Errorf here so the errors are proper value comparisons.
// ---------------------------------------------------------------------------

var (
	ErrDealStructureNotAvailable = fmt.Errorf("deal structure not available: required metadata fields are missing from case")
	ErrCaseQALocked              = fmt.Errorf("case is QA-locked; changes must be staged")
	ErrSkillMismatch             = fmt.Errorf("assignee does not hold any required skill for this case")
	ErrBlockingTasksRemain       = fmt.Errorf("one or more blocking tasks are still incomplete")
	ErrUnresolvedBlockingTags    = fmt.Errorf("one or more BLOCKING document error tags remain unresolved")
	ErrChecklistIncomplete       = fmt.Errorf("credit memo checklist is not complete")
	ErrChecklistFailed           = fmt.Errorf("credit memo checklist has a FAILED overall status")
)
