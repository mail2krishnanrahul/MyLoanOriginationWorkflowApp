package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeSnapshotHash_CanonicalIsStable(t *testing.T) {
	a := json.RawMessage(`{"dealId":"D-1","snapshot":{"b":2,"a":1},"arr":[{"z":2,"a":1}]}`)
	b := json.RawMessage(`{"arr":[{"a":1,"z":2}],"snapshot":{"a":1,"b":2},"dealId":"D-1"}`)

	hashA, err := computeSnapshotHash(a)
	require.NoError(t, err)
	hashB, err := computeSnapshotHash(b)
	require.NoError(t, err)

	assert.Equal(t, hashA, hashB)
}

func TestGenerateTasksForDeal_HarbourLikeSnapshot_CountAndDedup(t *testing.T) {
	deal := BusinessLendingDeal{
		DealID:   "DL-20260221-000842",
		DealName: "Harbour Group Expansion - FY26",
		Status:   "DOCUMENT_VERIFICATION",
		UmbrellaLimit: UmbrellaLimit{
			ApprovedLimit: Money{Amount: 5_000_000, Currency: "AUD"},
		},
		LegalParties: []LegalParty{
			{PartyID: "PTY-001", PartyType: "COMPANY", LegalName: "Harbour Trading Pty Ltd", Regulatory: &PartyRegulatory{ACN: "222333444"}},
			{PartyID: "PTY-002", PartyType: "COMPANY", LegalName: "Harbour Logistics Pty Ltd", Regulatory: &PartyRegulatory{ACN: "666777888"}},
		},
		TrustStructures: []TrustStructure{
			{
				TrustID:       "TRUST-001",
				TrustName:     "The Harbour Family Trust",
				TrustDeedDate: "2014-06-30",
				Trustees: []Trustee{{
					PartyID:           "PTY-002",
					CapacityStatement: "Harbour Logistics Pty Ltd ATF The Harbour Family Trust",
				}},
			},
		},
		BorrowingEntities: []BorrowingEntity{
			{
				BorrowingEntityID: "BE-001",
				Facilities: []Facility{
					{
						FacilityID: "FAC-001",
						Product:    "COMMERCIAL_PROPERTY_FINANCE",
						Purpose:    "Acquire warehouse",
						Guarantees: []Guarantee{{GuaranteeType: "RELATED_ENTITY_GUARANTEE", Guarantor: GuaranteeParty{PartyID: "PTY-002"}}},
					},
					{
						FacilityID: "FAC-002",
						Product:    "BUSINESS_ONE_OVERDRAFT",
						Purpose:    "Working capital",
						Guarantees: []Guarantee{{GuaranteeType: "RELATED_ENTITY_GUARANTEE", Guarantor: GuaranteeParty{PartyID: "PTY-002"}}},
					},
				},
			},
			{
				BorrowingEntityID: "BE-TRUST-001",
				Facilities: []Facility{
					{
						FacilityID: "FAC-003",
						Product:    "EQUIPMENT_FINANCE",
						Purpose:    "Fleet upgrade",
						Guarantees: []Guarantee{{GuaranteeType: "RELATED_ENTITY_GUARANTEE", Guarantor: GuaranteeParty{PartyID: "PTY-001"}}},
					},
				},
			},
		},
		Security: Security{Collaterals: []Collateral{
			{
				CollateralID: "COL-001",
				Assets: []Asset{
					{AssetID: "AST-GSA-001", AssetType: "GSA", PPSRRegistrationNumber: "PPSR-NSW-88990011"},
					{AssetID: "AST-PR-001", AssetType: "PROPERTY", TitleReference: "DP123456", Address: &AssetAddress{Line1: "45 Industrial Ave", Suburb: "Alexandria", State: "NSW"}},
				},
			},
		}},
	}

	tasks := generateTasksForDeal(deal)
	require.Len(t, tasks, 24)

	assert.Equal(t, "KYC_AML_SCREENING", tasks[0].TaskType)
	assert.Equal(t, "PTY-001", tasks[0].ContextRef.EntityID)
	assert.Equal(t, "SOLICITORS_CERTIFICATE_OBTAINED", tasks[len(tasks)-1].TaskType)
	assert.Equal(t, "PTY-001", tasks[len(tasks)-1].ContextRef.EntityID)

	guaranteeDocTasks := 0
	for _, task := range tasks {
		if task.TaskType == "GUARANTEE_DOCUMENT_REVIEW" {
			guaranteeDocTasks++
		}
	}
	assert.Equal(t, 2, guaranteeDocTasks, "duplicate guarantor across facilities must be de-duplicated")
}

func TestComputeDealDiff_MaterialAndNonMaterial(t *testing.T) {
	previous := json.RawMessage(`
		{
			"dealId":"D-1",
			"dealName":"Deal A",
			"status":"DOCUMENT_VERIFICATION",
			"customerReference":"C-1",
			"umbrellaLimit":{"approvedLimit":{"amount":1000000,"currency":"AUD"}},
			"legalParties":[{"partyId":"P-1","partyType":"COMPANY","legalName":"Alpha"}],
			"borrowingEntities":[{"borrowingEntityId":"BE-1","facilities":[{"facilityId":"F-1","status":"DRAFT","product":"TERM","facilityLimit":{"amount":500000,"currency":"AUD"},"umbrellaConsumption":{"consumptionAmount":{"amount":500000,"currency":"AUD"}},"pricing":{"rate":1},"term":{"months":12}}]}],
			"security":{"collaterals":[{"collateralId":"COL-1","assets":[]}]}
		}
	`)

	nextMaterial := json.RawMessage(`
		{
			"dealId":"D-1",
			"dealName":"Deal A",
			"status":"DOCUMENT_VERIFICATION",
			"customerReference":"C-1",
			"umbrellaLimit":{"approvedLimit":{"amount":1000000,"currency":"AUD"}},
			"legalParties":[{"partyId":"P-1","partyType":"COMPANY","legalName":"Alpha"}],
			"borrowingEntities":[{"borrowingEntityId":"BE-1","facilities":[
				{"facilityId":"F-1","status":"DRAFT","product":"TERM","facilityLimit":{"amount":500000,"currency":"AUD"},"umbrellaConsumption":{"consumptionAmount":{"amount":500000,"currency":"AUD"}},"pricing":{"rate":1},"term":{"months":12}},
				{"facilityId":"F-2","status":"DRAFT","product":"OD","facilityLimit":{"amount":100000,"currency":"AUD"},"umbrellaConsumption":{"consumptionAmount":{"amount":100000,"currency":"AUD"}},"pricing":{"rate":2},"term":{"months":6}}
			]}],
			"security":{"collaterals":[{"collateralId":"COL-1","assets":[]}]}
		}
	`)

	nextNonMaterial := json.RawMessage(`
		{
			"dealId":"D-1",
			"dealName":"Deal A (display change)",
			"status":"DOCUMENT_VERIFICATION",
			"customerReference":"C-1",
			"umbrellaLimit":{"approvedLimit":{"amount":1000000,"currency":"AUD"}},
			"legalParties":[{"partyId":"P-1","partyType":"COMPANY","legalName":"Alpha"}],
			"borrowingEntities":[{"borrowingEntityId":"BE-1","facilities":[{"facilityId":"F-1","status":"DRAFT","product":"TERM","facilityLimit":{"amount":500000,"currency":"AUD"},"umbrellaConsumption":{"consumptionAmount":{"amount":500000,"currency":"AUD"}},"pricing":{"rate":1},"term":{"months":12}}]}],
			"security":{"collaterals":[{"collateralId":"COL-1","assets":[]}]}
		}
	`)

	diffMaterial, err := computeDealDiff(previous, nextMaterial)
	require.NoError(t, err)
	assert.True(t, diffMaterial.IsMaterial)
	assert.Contains(t, diffMaterial.FacilitiesAdded, "F-2")

	diffNonMaterial, err := computeDealDiff(previous, nextNonMaterial)
	require.NoError(t, err)
	assert.False(t, diffNonMaterial.IsMaterial)
	assert.False(t, diffNonMaterial.StatusChanged)
	assert.False(t, diffNonMaterial.UmbrellaLimitChanged)
}
