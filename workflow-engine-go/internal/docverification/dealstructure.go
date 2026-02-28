package docverification

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GetDealStructureView constructs a typed DealStructureView from the case
// metadata JSONB. The deal structure is originally ingested by the
// business-lending deal ingestion pipeline and stored under well-known keys.
func GetDealStructureView(ctx context.Context, pool *pgxpool.Pool, caseID, tenantID string) (*model.DealStructureView, error) {
	var rawMeta []byte
	err := pool.QueryRow(ctx, `
		SELECT metadata
		FROM cases
		WHERE id = $1::uuid AND tenant_id = $2::uuid
	`, caseID, tenantID).Scan(&rawMeta)
	if err != nil {
		return nil, fmt.Errorf("GetDealStructureView: load case metadata: %w", err)
	}

	// The metadata blob is expected to contain a "deal_structure" sub-key,
	// written by the deal ingestion pipeline.
	type metaWrapper struct {
		DealStructure *struct {
			BorrowingEntity    json.RawMessage `json:"borrowing_entity"`
			LoanFacilities     json.RawMessage `json:"loan_facilities"`
			SecurityProperties json.RawMessage `json:"security_properties"`
			LoanSummary        json.RawMessage `json:"loan_summary"`
			Conditions         json.RawMessage `json:"conditions"`
		} `json:"deal_structure"`
	}

	var meta metaWrapper
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		return nil, fmt.Errorf("GetDealStructureView: parse metadata: %w", err)
	}
	if meta.DealStructure == nil {
		return nil, model.ErrDealStructureNotAvailable
	}

	ds := meta.DealStructure
	view := &model.DealStructureView{}

	if len(ds.BorrowingEntity) > 0 {
		_ = json.Unmarshal(ds.BorrowingEntity, &view.BorrowingEntity)
	}
	if len(ds.LoanFacilities) > 0 {
		_ = json.Unmarshal(ds.LoanFacilities, &view.LoanFacilities)
	}
	if len(ds.SecurityProperties) > 0 {
		_ = json.Unmarshal(ds.SecurityProperties, &view.SecurityProperties)
	}
	if len(ds.LoanSummary) > 0 {
		_ = json.Unmarshal(ds.LoanSummary, &view.LoanSummary)
	}
	if len(ds.Conditions) > 0 {
		_ = json.Unmarshal(ds.Conditions, &view.Conditions)
	}

	return view, nil
}
