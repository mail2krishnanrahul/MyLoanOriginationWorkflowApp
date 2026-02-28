package docverification

// Case type code for the document verification workflow.
const CaseTypeCode = "HOME_LOAN_DOC_VERIFICATION"

// Workflow stage codes (in sequential order).
const (
	StageIntake               = "INTAKE"
	StageClassification       = "CLASSIFICATION"
	StageAllocation           = "ALLOCATION"
	StageDocumentVerification = "DOCUMENT_VERIFICATION"
	StageAdditionalInfo       = "ADDITIONAL_INFO_REQUEST"
	StageQAReview             = "QA_REVIEW"
	StageQARemediation        = "QA_REMEDIATION"
)

// Workbasket codes.
const (
	WorkbasketDocVerQueue      = "DOC_VER_QUEUE"
	WorkbasketQAQueue          = "QA_QUEUE"
	WorkbasketNonStandardQueue = "NON_STANDARD_QUEUE"
)

// Team codes seeded during bootstrap.
const (
	TeamSupportLead = "SUPPORT_LEAD_TEAM"
	TeamDocPrep     = "DOC_PREP_TEAM"
	TeamQA          = "QA_TEAM"
	TeamBanker      = "BANKER_TEAM"
)

// Role codes seeded during bootstrap.
const (
	RoleLoanOfficer    = "LOAN_OFFICER"
	RoleQAOfficer      = "QA_OFFICER"
	RoleTeamLead       = "TEAM_LEAD"
	RoleBanker         = "BANKER"
	RoleSupportOfficer = "SUPPORT_OFFICER"
)
