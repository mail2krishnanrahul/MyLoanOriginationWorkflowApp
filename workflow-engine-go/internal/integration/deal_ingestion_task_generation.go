package integration

import (
	"fmt"
	"sort"
	"strings"
)

func generateTasksForDeal(deal BusinessLendingDeal) []GeneratedDealTask {
	builder := newTaskBuilder()

	generatePartyTasks(builder, sortedParties(deal.LegalParties), nil)
	generateTrustTasks(builder, sortedTrusts(deal.TrustStructures), nil)
	generateDealLevelTasks(builder, deal)

	facilities := sortedFacilities(flattenFacilities(deal))
	generateFacilityTasks(builder, facilities, nil)

	collaterals := sortedCollaterals(deal.Security.Collaterals)
	generateCollateralTasks(builder, collaterals, nil)

	generateGuaranteeTasks(builder, facilities, nil)

	return builder.tasks
}

func generateTasksForMaterialAdditions(deal BusinessLendingDeal, diff *DealDiff) []GeneratedDealTask {
	if diff == nil {
		return nil
	}
	builder := newTaskBuilder()

	partyFilter := make(map[string]struct{}, len(diff.PartiesAdded))
	for _, id := range diff.PartiesAdded {
		partyFilter[id] = struct{}{}
	}
	generatePartyTasks(builder, sortedParties(deal.LegalParties), partyFilter)

	facilityFilter := make(map[string]struct{}, len(diff.FacilitiesAdded))
	for _, id := range diff.FacilitiesAdded {
		facilityFilter[id] = struct{}{}
	}
	facilities := sortedFacilities(flattenFacilities(deal))
	generateFacilityTasks(builder, facilities, facilityFilter)
	generateGuaranteeTasks(builder, facilities, facilityFilter)

	collateralFilter := make(map[string]struct{}, len(diff.CollateralsAdded))
	for _, id := range diff.CollateralsAdded {
		collateralFilter[id] = struct{}{}
	}
	generateCollateralTasks(builder, sortedCollaterals(deal.Security.Collaterals), collateralFilter)

	return builder.tasks
}

type taskBuilder struct {
	tasks []GeneratedDealTask
	seen  map[string]struct{}
}

func newTaskBuilder() *taskBuilder {
	return &taskBuilder{
		tasks: make([]GeneratedDealTask, 0),
		seen:  make(map[string]struct{}),
	}
}

func (b *taskBuilder) add(task GeneratedDealTask) {
	key := task.TaskType + "::" + strings.TrimSpace(task.ContextRef.EntityID)
	if _, exists := b.seen[key]; exists {
		return
	}
	b.seen[key] = struct{}{}
	b.tasks = append(b.tasks, task)
}

func generatePartyTasks(builder *taskBuilder, parties []LegalParty, filter map[string]struct{}) {
	for _, party := range parties {
		if filter != nil {
			if _, ok := filter[party.PartyID]; !ok {
				continue
			}
		}
		entityLabel := strings.TrimSpace(party.LegalName)
		if entityLabel == "" && party.Name != nil {
			entityLabel = strings.TrimSpace(strings.TrimSpace(party.Name.GivenName) + " " + strings.TrimSpace(party.Name.FamilyName))
		}
		ctx := TaskContextRef{EntityType: "PARTY", EntityID: party.PartyID, EntityLabel: entityLabel}

		builder.add(GeneratedDealTask{
			TaskType:  "KYC_AML_SCREENING",
			Title:     fmt.Sprintf("AML/CTF Screening — %s (%s)", displayLegalName(party), party.PartyID),
			Mandatory: true,
			Origin:    "AUTO_GENERATED",
			ContextRef: ctx,
		})

		builder.add(GeneratedDealTask{
			TaskType:  "KYC_BENEFICIAL_OWNERSHIP",
			Title:     fmt.Sprintf("Beneficial Ownership Declaration — %s", displayLegalName(party)),
			Mandatory: true,
			Origin:    "AUTO_GENERATED",
			ContextRef: ctx,
		})

		partyType := strings.ToUpper(strings.TrimSpace(party.PartyType))
		switch partyType {
		case "COMPANY":
			acn := "UNKNOWN"
			if party.Regulatory != nil && strings.TrimSpace(party.Regulatory.ACN) != "" {
				acn = strings.TrimSpace(party.Regulatory.ACN)
			}
			builder.add(GeneratedDealTask{
				TaskType:  "KYC_COMPANY_EXTRACT",
				Title:     fmt.Sprintf("ASIC Company Extract (< 3 months) — %s ACN %s", displayLegalName(party), acn),
				Mandatory: true,
				Origin:    "AUTO_GENERATED",
				ContextRef: ctx,
			})
		case "INDIVIDUAL":
			fullName := displayIndividualName(party)
			builder.add(GeneratedDealTask{
				TaskType:  "KYC_INDIVIDUAL_IDENTITY",
				Title:     fmt.Sprintf("Identity Verification (Passport or Licence) — %s", fullName),
				Mandatory: true,
				Origin:    "AUTO_GENERATED",
				ContextRef: ctx,
			})
		}
	}
}

func generateTrustTasks(builder *taskBuilder, trusts []TrustStructure, filter map[string]struct{}) {
	for _, trust := range trusts {
		if filter != nil {
			if _, ok := filter[trust.TrustID]; !ok {
				continue
			}
		}
		entityLabel := strings.TrimSpace(trust.TrustName)
		ctx := TaskContextRef{EntityType: "TRUST", EntityID: trust.TrustID, EntityLabel: entityLabel}

		builder.add(GeneratedDealTask{
			TaskType:  "TRUST_DEED_VERIFICATION",
			Title:     fmt.Sprintf("Trust Deed Verification — %s (deed date: %s)", trust.TrustName, safeOr(trust.TrustDeedDate, "unknown")),
			Mandatory: true,
			Origin:    "AUTO_GENERATED",
			ContextRef: ctx,
		})

		builder.add(GeneratedDealTask{
			TaskType:  "TRUST_DEED_AMENDMENT_CHECK",
			Title:     fmt.Sprintf("Confirm No Undisclosed Amendments — %s", trust.TrustName),
			Mandatory: true,
			Origin:    "AUTO_GENERATED",
			ContextRef: ctx,
		})

		capacity := trust.TrustName
		if len(trust.Trustees) > 0 && strings.TrimSpace(trust.Trustees[0].CapacityStatement) != "" {
			capacity = strings.TrimSpace(trust.Trustees[0].CapacityStatement)
		}
		builder.add(GeneratedDealTask{
			TaskType:  "TRUSTEE_AUTHORITY_CONFIRMATION",
			Title:     fmt.Sprintf("Trustee Borrowing Authority Confirmed — %s", capacity),
			Mandatory: true,
			Origin:    "AUTO_GENERATED",
			ContextRef: ctx,
		})
	}
}

func generateDealLevelTasks(builder *taskBuilder, deal BusinessLendingDeal) {
	ctx := TaskContextRef{EntityType: "UMBRELLA", EntityID: deal.DealID, EntityLabel: deal.DealName}

	builder.add(GeneratedDealTask{
		TaskType:  "FINANCIAL_STATEMENTS_REVIEW",
		Title:     fmt.Sprintf("Financial Statements (2 years) — %s", deal.DealName),
		Mandatory: true,
		Origin:    "AUTO_GENERATED",
		ContextRef: ctx,
	})
	builder.add(GeneratedDealTask{
		TaskType:  "TAX_RETURNS_REVIEW",
		Title:     fmt.Sprintf("Tax Returns (2 years) — %s", deal.DealName),
		Mandatory: true,
		Origin:    "AUTO_GENERATED",
		ContextRef: ctx,
	})
	builder.add(GeneratedDealTask{
		TaskType:  "MANAGEMENT_ACCOUNTS_REVIEW",
		Title:     fmt.Sprintf("Management Accounts (latest period) — %s", deal.DealName),
		Mandatory: false,
		Origin:    "AUTO_GENERATED",
		ContextRef: ctx,
	})
}

func generateFacilityTasks(builder *taskBuilder, facilities []Facility, filter map[string]struct{}) {
	for _, facility := range facilities {
		if filter != nil {
			if _, ok := filter[facility.FacilityID]; !ok {
				continue
			}
		}
		ctx := TaskContextRef{EntityType: "FACILITY", EntityID: facility.FacilityID, EntityLabel: facility.Product}
		builder.add(GeneratedDealTask{
			TaskType:  "FACILITY_PURPOSE_EVIDENCE",
			Title:     fmt.Sprintf("Purpose Evidence — %s %s (%s)", facility.FacilityID, facility.Product, safeOr(facility.Purpose, "purpose not provided")),
			Mandatory: true,
			Origin:    "AUTO_GENERATED",
			ContextRef: ctx,
		})

		if strings.EqualFold(strings.TrimSpace(facility.Product), "EQUIPMENT_FINANCE") {
			builder.add(GeneratedDealTask{
				TaskType:  "EQUIPMENT_SCHEDULE_VERIFICATION",
				Title:     fmt.Sprintf("Equipment/Asset Schedule — %s %s", facility.FacilityID, safeOr(facility.Purpose, "asset schedule")),
				Mandatory: true,
				Origin:    "AUTO_GENERATED",
				ContextRef: ctx,
			})
		}
	}
}

func generateCollateralTasks(builder *taskBuilder, collaterals []Collateral, filter map[string]struct{}) {
	for _, collateral := range collaterals {
		if filter != nil {
			if _, ok := filter[collateral.CollateralID]; !ok {
				continue
			}
		}
		assets := append([]Asset(nil), collateral.Assets...)
		sort.Slice(assets, func(i, j int) bool {
			return strings.TrimSpace(assets[i].AssetID) < strings.TrimSpace(assets[j].AssetID)
		})
		for _, asset := range assets {
			ctx := TaskContextRef{EntityType: "COLLATERAL", EntityID: asset.AssetID, EntityLabel: collateral.CollateralID}
			switch strings.ToUpper(strings.TrimSpace(asset.AssetType)) {
			case "PROPERTY":
				line1, suburb, state := propertyAddressParts(asset)
				builder.add(GeneratedDealTask{
					TaskType:  "VALUATION_REPORT_REVIEW",
					Title:     fmt.Sprintf("Valuation Report — %s, %s %s", line1, suburb, state),
					Mandatory: true,
					Origin:    "AUTO_GENERATED",
					ContextRef: ctx,
				})
				builder.add(GeneratedDealTask{
					TaskType:  "TITLE_SEARCH_CONFIRMATION",
					Title:     fmt.Sprintf("Title Search — %s", safeOr(asset.TitleReference, "title reference pending")),
					Mandatory: true,
					Origin:    "AUTO_GENERATED",
					ContextRef: ctx,
				})
				builder.add(GeneratedDealTask{
					TaskType:  "INSURANCE_EVIDENCE",
					Title:     fmt.Sprintf("Building & Contents Insurance — %s, %s", line1, suburb),
					Mandatory: true,
					Origin:    "AUTO_GENERATED",
					ContextRef: ctx,
				})
			case "GSA":
				builder.add(GeneratedDealTask{
					TaskType:  "PPSR_SEARCH_CONFIRMATION",
					Title:     fmt.Sprintf("PPSR Search — %s", safeOr(asset.PPSRRegistrationNumber, "to be registered")),
					Mandatory: true,
					Origin:    "AUTO_GENERATED",
					ContextRef: ctx,
				})
			}
		}
	}
}

func generateGuaranteeTasks(builder *taskBuilder, facilities []Facility, facilityFilter map[string]struct{}) {
	for _, facility := range facilities {
		if facilityFilter != nil {
			if _, ok := facilityFilter[facility.FacilityID]; !ok {
				continue
			}
		}
		guarantees := append([]Guarantee(nil), facility.Guarantees...)
		sort.Slice(guarantees, func(i, j int) bool {
			iID := guaranteeContextEntityID(guarantees[i], facility.FacilityID)
			jID := guaranteeContextEntityID(guarantees[j], facility.FacilityID)
			if iID == jID {
				return strings.TrimSpace(guarantees[i].GuaranteeType) < strings.TrimSpace(guarantees[j].GuaranteeType)
			}
			return iID < jID
		})

		for _, guarantee := range guarantees {
			guarantorID := strings.TrimSpace(guarantee.Guarantor.PartyID)
			if guarantorID == "" {
				guarantorID = strings.TrimSpace(guarantee.Guarantor.Name)
			}
			if guarantorID == "" {
				guarantorID = facility.FacilityID
			}
			ctx := TaskContextRef{EntityType: "GUARANTOR", EntityID: guarantorID, EntityLabel: guarantorID}
			titleSuffix := guarantorID
			builder.add(GeneratedDealTask{
				TaskType:  "GUARANTEE_DOCUMENT_REVIEW",
				Title:     fmt.Sprintf("Guarantee Document Review — %s by %s", safeOr(guarantee.GuaranteeType, "GUARANTEE"), titleSuffix),
				Mandatory: true,
				Origin:    "AUTO_GENERATED",
				ContextRef: ctx,
			})
			builder.add(GeneratedDealTask{
				TaskType:  "SOLICITORS_CERTIFICATE_OBTAINED",
				Title:     fmt.Sprintf("Solicitor's Certificate (Independent Legal Advice) — %s", titleSuffix),
				Mandatory: true,
				Origin:    "AUTO_GENERATED",
				ContextRef: ctx,
			})
		}
	}
}

func guaranteeContextEntityID(guarantee Guarantee, facilityID string) string {
	guarantorID := strings.TrimSpace(guarantee.Guarantor.PartyID)
	if guarantorID == "" {
		guarantorID = strings.TrimSpace(guarantee.Guarantor.Name)
	}
	if guarantorID == "" {
		guarantorID = strings.TrimSpace(facilityID)
	}
	return guarantorID
}

func sortedParties(parties []LegalParty) []LegalParty {
	result := append([]LegalParty(nil), parties...)
	sort.Slice(result, func(i, j int) bool {
		return strings.TrimSpace(result[i].PartyID) < strings.TrimSpace(result[j].PartyID)
	})
	return result
}

func sortedTrusts(trusts []TrustStructure) []TrustStructure {
	result := append([]TrustStructure(nil), trusts...)
	sort.Slice(result, func(i, j int) bool {
		return strings.TrimSpace(result[i].TrustID) < strings.TrimSpace(result[j].TrustID)
	})
	return result
}

func sortedFacilities(facilityMap map[string]Facility) []Facility {
	result := make([]Facility, 0, len(facilityMap))
	for _, facility := range facilityMap {
		result = append(result, facility)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.TrimSpace(result[i].FacilityID) < strings.TrimSpace(result[j].FacilityID)
	})
	return result
}

func sortedCollaterals(collaterals []Collateral) []Collateral {
	result := append([]Collateral(nil), collaterals...)
	sort.Slice(result, func(i, j int) bool {
		return strings.TrimSpace(result[i].CollateralID) < strings.TrimSpace(result[j].CollateralID)
	})
	return result
}

func displayLegalName(party LegalParty) string {
	if strings.TrimSpace(party.LegalName) != "" {
		return strings.TrimSpace(party.LegalName)
	}
	if party.Name != nil {
		return displayIndividualName(party)
	}
	return party.PartyID
}

func displayIndividualName(party LegalParty) string {
	if party.Name == nil {
		return party.PartyID
	}
	fullName := strings.TrimSpace(strings.TrimSpace(party.Name.GivenName) + " " + strings.TrimSpace(party.Name.FamilyName))
	if fullName == "" {
		return party.PartyID
	}
	return fullName
}

func propertyAddressParts(asset Asset) (string, string, string) {
	if asset.Address == nil {
		return "property", "unknown suburb", ""
	}
	line1 := safeOr(asset.Address.Line1, "property")
	suburb := safeOr(asset.Address.Suburb, "unknown suburb")
	state := strings.TrimSpace(asset.Address.State)
	return line1, suburb, state
}

func safeOr(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
