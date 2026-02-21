package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func computeDealDiff(previousSnapshot json.RawMessage, newSnapshot json.RawMessage) (*DealDiff, error) {
	var previousDeal BusinessLendingDeal
	if err := json.Unmarshal(previousSnapshot, &previousDeal); err != nil {
		return nil, fmt.Errorf("decode previous snapshot: %w", err)
	}
	var newDeal BusinessLendingDeal
	if err := json.Unmarshal(newSnapshot, &newDeal); err != nil {
		return nil, fmt.Errorf("decode new snapshot: %w", err)
	}

	diff := &DealDiff{
		StatusChanged:         !strings.EqualFold(strings.TrimSpace(previousDeal.Status), strings.TrimSpace(newDeal.Status)),
		PreviousStatus:        previousDeal.Status,
		NewStatus:             newDeal.Status,
		UmbrellaLimitChanged:  !moneyEqual(previousDeal.UmbrellaLimit.ApprovedLimit, newDeal.UmbrellaLimit.ApprovedLimit),
		PreviousApprovedLimit: &previousDeal.UmbrellaLimit.ApprovedLimit,
		NewApprovedLimit:      &newDeal.UmbrellaLimit.ApprovedLimit,
	}

	previousFacilities := flattenFacilities(previousDeal)
	newFacilities := flattenFacilities(newDeal)

	diff.FacilitiesAdded, diff.FacilitiesRemoved, diff.FacilitiesModified = compareFacilities(previousFacilities, newFacilities)
	diff.PartiesAdded, diff.PartiesModified = compareParties(previousDeal.LegalParties, newDeal.LegalParties)
	diff.CollateralsAdded, diff.CollateralsRemoved = compareCollaterals(previousDeal.Security.Collaterals, newDeal.Security.Collaterals)
	diff.SecurityChanged = len(diff.CollateralsAdded) > 0 || len(diff.CollateralsRemoved) > 0

	diff.IsMaterial = determineMateriality(diff)
	return diff, nil
}

func determineMateriality(diff *DealDiff) bool {
	if diff == nil {
		return false
	}
	if diff.StatusChanged || diff.UmbrellaLimitChanged {
		return true
	}
	if len(diff.FacilitiesAdded) > 0 || len(diff.FacilitiesRemoved) > 0 {
		return true
	}
	for _, facility := range diff.FacilitiesModified {
		for _, changeType := range facility.ChangeTypes {
			if changeType == "LIMIT_CHANGE" || changeType == "PRICING_CHANGE" || changeType == "TERM_CHANGE" {
				return true
			}
		}
	}
	if len(diff.PartiesAdded) > 0 {
		return true
	}
	if diff.SecurityChanged && (len(diff.CollateralsAdded) > 0 || len(diff.CollateralsRemoved) > 0) {
		return true
	}
	return false
}

func compareFacilities(previous map[string]Facility, next map[string]Facility) ([]string, []string, []FacilityModification) {
	added := make([]string, 0)
	removed := make([]string, 0)
	modified := make([]FacilityModification, 0)

	for id := range next {
		if _, ok := previous[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range previous {
		if _, ok := next[id]; !ok {
			removed = append(removed, id)
		}
	}

	for id, prev := range previous {
		current, ok := next[id]
		if !ok {
			continue
		}
		changeTypes := make([]string, 0)
		if !moneyEqual(prev.FacilityLimit, current.FacilityLimit) {
			changeTypes = append(changeTypes, "LIMIT_CHANGE")
		}
		if !jsonEqual(prev.Pricing, current.Pricing) {
			changeTypes = append(changeTypes, "PRICING_CHANGE")
		}
		if !jsonEqual(prev.Term, current.Term) {
			changeTypes = append(changeTypes, "TERM_CHANGE")
		}
		if !strings.EqualFold(strings.TrimSpace(prev.Status), strings.TrimSpace(current.Status)) {
			changeTypes = append(changeTypes, "STATUS_CHANGE")
		}
		if len(changeTypes) > 0 {
			modified = append(modified, FacilityModification{FacilityID: id, ChangeTypes: changeTypes})
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Slice(modified, func(i, j int) bool {
		return modified[i].FacilityID < modified[j].FacilityID
	})
	return added, removed, modified
}

func compareParties(previous []LegalParty, next []LegalParty) ([]string, []string) {
	prevMap := make(map[string]LegalParty, len(previous))
	nextMap := make(map[string]LegalParty, len(next))
	for _, party := range previous {
		if strings.TrimSpace(party.PartyID) != "" {
			prevMap[party.PartyID] = party
		}
	}
	for _, party := range next {
		if strings.TrimSpace(party.PartyID) != "" {
			nextMap[party.PartyID] = party
		}
	}

	added := make([]string, 0)
	modified := make([]string, 0)
	for id, current := range nextMap {
		previousParty, ok := prevMap[id]
		if !ok {
			added = append(added, id)
			continue
		}
		if !legalPartyEquivalent(previousParty, current) {
			modified = append(modified, id)
		}
	}

	sort.Strings(added)
	sort.Strings(modified)
	return added, modified
}

func compareCollaterals(previous []Collateral, next []Collateral) ([]string, []string) {
	prevSet := make(map[string]struct{}, len(previous))
	nextSet := make(map[string]struct{}, len(next))

	for _, collateral := range previous {
		if strings.TrimSpace(collateral.CollateralID) != "" {
			prevSet[collateral.CollateralID] = struct{}{}
		}
	}
	for _, collateral := range next {
		if strings.TrimSpace(collateral.CollateralID) != "" {
			nextSet[collateral.CollateralID] = struct{}{}
		}
	}

	added := make([]string, 0)
	removed := make([]string, 0)
	for id := range nextSet {
		if _, ok := prevSet[id]; !ok {
			added = append(added, id)
		}
	}
	for id := range prevSet {
		if _, ok := nextSet[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func flattenFacilities(deal BusinessLendingDeal) map[string]Facility {
	result := make(map[string]Facility)
	for _, entity := range deal.BorrowingEntities {
		for _, facility := range entity.Facilities {
			if strings.TrimSpace(facility.FacilityID) == "" {
				continue
			}
			result[facility.FacilityID] = facility
		}
	}
	return result
}

func legalPartyEquivalent(a, b LegalParty) bool {
	if !strings.EqualFold(strings.TrimSpace(a.PartyType), strings.TrimSpace(b.PartyType)) {
		return false
	}
	if strings.TrimSpace(a.LegalName) != strings.TrimSpace(b.LegalName) {
		return false
	}
	var acnA, acnB string
	if a.Regulatory != nil {
		acnA = strings.TrimSpace(a.Regulatory.ACN)
	}
	if b.Regulatory != nil {
		acnB = strings.TrimSpace(b.Regulatory.ACN)
	}
	if acnA != acnB {
		return false
	}
	if (a.Name == nil) != (b.Name == nil) {
		return false
	}
	if a.Name != nil && b.Name != nil {
		if strings.TrimSpace(a.Name.GivenName) != strings.TrimSpace(b.Name.GivenName) {
			return false
		}
		if strings.TrimSpace(a.Name.FamilyName) != strings.TrimSpace(b.Name.FamilyName) {
			return false
		}
	}
	return true
}

func moneyEqual(a, b Money) bool {
	if strings.TrimSpace(strings.ToUpper(a.Currency)) != strings.TrimSpace(strings.ToUpper(b.Currency)) {
		return false
	}
	return fmt.Sprintf("%.2f", a.Amount) == fmt.Sprintf("%.2f", b.Amount)
}

func jsonEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	var objA interface{}
	var objB interface{}
	if err := json.Unmarshal(a, &objA); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &objB); err != nil {
		return false
	}
	canA, errA := canonicalizeJSON(objA)
	canB, errB := canonicalizeJSON(objB)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(canA, canB)
}
