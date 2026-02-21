package integration

import (
	"encoding/json"
	"time"
)

// Ingestion outcomes for POST /ingest/deals.
type DealIngestOutcome string

const (
	DealIngestOutcomeIdempotentNoOp DealIngestOutcome = "IDEMPOTENT_NO_OP"
	DealIngestOutcomeFirstIngestion DealIngestOutcome = "FIRST_INGESTION"
	DealIngestOutcomeUpdated        DealIngestOutcome = "UPDATED"
	DealIngestOutcomeException      DealIngestOutcome = "EXCEPTION_REQUIRES_REVIEW"
)

// Case actions emitted by ingestion.
type DealCaseAction string

const (
	DealCaseActionNone                          DealCaseAction = "NONE"
	DealCaseActionCreated                       DealCaseAction = "CASE_CREATED"
	DealCaseActionSnapshotRefreshed             DealCaseAction = "CASE_SNAPSHOT_REFRESHED"
	DealCaseActionDealChangeFlagged             DealCaseAction = "CASE_DEAL_CHANGE_FLAGGED"
	DealCaseActionClosedCaseRecreated           DealCaseAction = "CASE_ALREADY_CLOSED_NEW_CASE_CREATED"
	DealCaseActionCancelledDueToStatusTransition DealCaseAction = "CASE_CANCELLED_DUE_TO_DEAL_STATUS"
)

// ProblemDetail is RFC-9457 style problem payload.
type ProblemDetail struct {
	Type     string      `json:"type,omitempty"`
	Title    string      `json:"title"`
	Status   int         `json:"status"`
	Detail   string      `json:"detail,omitempty"`
	Instance string      `json:"instance,omitempty"`
	Code     string      `json:"code,omitempty"`
	Errors   interface{} `json:"errors,omitempty"`
}

// IngestDealRequest wraps the upstream snapshot.
type IngestDealRequest struct {
	Snapshot          json.RawMessage `json:"snapshot"`
	SnapshotTimestamp string          `json:"snapshotTimestamp"`
	SourceSystem      string          `json:"sourceSystem"`
	CorrelationID     string          `json:"correlationId,omitempty"`
}

// IngestExceptionDetail is returned when a scenario is not covered by rules.
type IngestExceptionDetail struct {
	Code            string `json:"code"`
	Description     string `json:"description"`
	SuggestedAction string `json:"suggestedAction"`
}

// IngestResponse is the POST /ingest/deals result.
type IngestResponse struct {
	IngestID         string                 `json:"ingestId"`
	DealID           string                 `json:"dealId"`
	SnapshotHash     string                 `json:"snapshotHash"`
	Outcome          DealIngestOutcome      `json:"outcome"`
	Diff             *DealDiff              `json:"diff,omitempty"`
	CaseID           *string                `json:"caseId,omitempty"`
	CaseAction       DealCaseAction         `json:"caseAction"`
	ProcessedAt      time.Time              `json:"processedAt"`
	Warnings         []string               `json:"warnings,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	ExceptionDetail  *IngestExceptionDetail `json:"exceptionDetail,omitempty"`
}

// DealDiff captures structured business-level changes between snapshots.
type DealDiff struct {
	StatusChanged         bool                   `json:"statusChanged"`
	PreviousStatus        string                 `json:"previousStatus,omitempty"`
	NewStatus             string                 `json:"newStatus,omitempty"`
	UmbrellaLimitChanged  bool                   `json:"umbrellaLimitChanged"`
	PreviousApprovedLimit *Money                 `json:"previousApprovedLimit,omitempty"`
	NewApprovedLimit      *Money                 `json:"newApprovedLimit,omitempty"`
	FacilitiesAdded       []string               `json:"facilitiesAdded,omitempty"`
	FacilitiesRemoved     []string               `json:"facilitiesRemoved,omitempty"`
	FacilitiesModified    []FacilityModification `json:"facilitiesModified,omitempty"`
	PartiesAdded          []string               `json:"partiesAdded,omitempty"`
	PartiesModified       []string               `json:"partiesModified,omitempty"`
	SecurityChanged       bool                   `json:"securityChanged"`
	CollateralsAdded      []string               `json:"collateralsAdded,omitempty"`
	CollateralsRemoved    []string               `json:"collateralsRemoved,omitempty"`
	IsMaterial            bool                   `json:"isMaterial"`
}

// FacilityModification captures changed domains for one facility.
type FacilityModification struct {
	FacilityID  string   `json:"facilityId"`
	ChangeTypes []string `json:"changeTypes"`
}

// IngestionRecord is returned by GET /ingest/deals/{dealId}/history.
type IngestionRecord struct {
	IngestID           string                 `json:"ingestId"`
	SnapshotTimestamp  time.Time              `json:"snapshotTimestamp"`
	ReceivedAt         time.Time              `json:"receivedAt"`
	SnapshotHash       string                 `json:"snapshotHash"`
	Outcome            DealIngestOutcome      `json:"outcome"`
	CaseAction         DealCaseAction         `json:"caseAction"`
	DiffSummary        *DealDiff              `json:"diffSummary,omitempty"`
	Warnings           []string               `json:"warnings,omitempty"`
	SourceSystem       string                 `json:"sourceSystem,omitempty"`
	CorrelationID      string                 `json:"correlationId,omitempty"`
	CaseID             *string                `json:"caseId,omitempty"`
	ExceptionDetail    *IngestExceptionDetail `json:"exceptionDetail,omitempty"`
}

// IngestionHistoryResponse is the history payload.
type IngestionHistoryResponse struct {
	DealID      string            `json:"dealId"`
	Ingestions  []IngestionRecord `json:"ingestions"`
}

// BusinessLendingDeal contains ingestion-relevant fields from the upstream snapshot.
type BusinessLendingDeal struct {
	DealID            string              `json:"dealId"`
	DealName          string              `json:"dealName"`
	Status            string              `json:"status"`
	CustomerReference string              `json:"customerReference"`
	UmbrellaLimit     UmbrellaLimit       `json:"umbrellaLimit"`
	LegalParties      []LegalParty        `json:"legalParties"`
	TrustStructures   []TrustStructure    `json:"trustStructures"`
	BorrowingEntities []BorrowingEntity   `json:"borrowingEntities"`
	Security          Security            `json:"security"`
}

type UmbrellaLimit struct {
	UmbrellaLimitID string `json:"umbrellaLimitId"`
	ApprovedLimit   Money  `json:"approvedLimit"`
}

type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type LegalParty struct {
	PartyID     string           `json:"partyId"`
	PartyType   string           `json:"partyType"`
	LegalName   string           `json:"legalName"`
	Name        *IndividualName  `json:"name,omitempty"`
	Regulatory  *PartyRegulatory `json:"regulatory,omitempty"`
}

type IndividualName struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
}

type PartyRegulatory struct {
	ACN string `json:"acn"`
}

type TrustStructure struct {
	TrustID        string         `json:"trustId"`
	TrustName      string         `json:"trustName"`
	TrustDeedDate  string         `json:"trustDeedDate"`
	Trustees       []Trustee      `json:"trustees"`
}

type Trustee struct {
	PartyID           string `json:"partyId"`
	CapacityStatement string `json:"capacityStatement"`
}

type BorrowingEntity struct {
	BorrowingEntityID   string             `json:"borrowingEntityId"`
	BorrowingEntityType string             `json:"borrowingEntityType"`
	TrustID             string             `json:"trustId,omitempty"`
	DisplayName         string             `json:"displayName"`
	Obligors            []BorrowingObligor `json:"obligors"`
	Facilities          []Facility         `json:"facilities"`
}

type BorrowingObligor struct {
	PartyID  string `json:"partyId"`
	Capacity string `json:"capacity"`
}

type Facility struct {
	FacilityID           string              `json:"facilityId"`
	Product              string              `json:"product"`
	Status               string              `json:"status"`
	Purpose              string              `json:"purpose"`
	FacilityLimit        Money               `json:"facilityLimit"`
	UmbrellaConsumption  FacilityConsumption `json:"umbrellaConsumption"`
	Pricing              json.RawMessage     `json:"pricing"`
	Term                 json.RawMessage     `json:"term"`
	Guarantees           []Guarantee         `json:"guarantees"`
}

type FacilityConsumption struct {
	ConsumptionAmount Money `json:"consumptionAmount"`
}

type Guarantee struct {
	GuaranteeType string         `json:"guaranteeType"`
	Guarantor     GuaranteeParty `json:"guarantor"`
	Scope         string         `json:"scope"`
	LimitAmount   *Money         `json:"limitAmount,omitempty"`
}

type GuaranteeParty struct {
	PartyID string `json:"partyId"`
	Name    string `json:"name"`
}

type Security struct {
	Collaterals []Collateral `json:"collaterals"`
}

type Collateral struct {
	CollateralID string  `json:"collateralId"`
	Assets       []Asset `json:"assets"`
}

type Asset struct {
	AssetID                 string       `json:"assetId"`
	AssetType               string       `json:"assetType"`
	Address                 *AssetAddress `json:"address,omitempty"`
	TitleReference          string       `json:"titleReference,omitempty"`
	PPSRRegistrationNumber  string       `json:"ppsrRegistrationNumber,omitempty"`
}

type AssetAddress struct {
	Line1  string `json:"line1"`
	Suburb string `json:"suburb"`
	State  string `json:"state"`
}

// GeneratedDealTask is one deterministic auto-generated checklist entry.
type GeneratedDealTask struct {
	TaskType   string         `json:"taskType"`
	Title      string         `json:"title"`
	Mandatory  bool           `json:"mandatory"`
	Origin     string         `json:"origin"`
	ContextRef TaskContextRef `json:"contextRef"`
}

// TaskContextRef links a task back to the originating deal entity.
type TaskContextRef struct {
	EntityType  string `json:"entityType"`
	EntityID    string `json:"entityId"`
	EntityLabel string `json:"entityLabel"`
}
