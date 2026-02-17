package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/pkg/model"
)

// GetCaseTypeByCodeAndVersion fetches an ACTIVE case type by code and version.
// If version is 0, the latest ACTIVE version is returned.
func (r *Repository) GetCaseTypeByCodeAndVersion(
	ctx context.Context, tx DBExecutor,
	code string, version int,
) (*model.CaseType, error) {
	if tx == nil {
		tx = r.Pool
	}

	var ct model.CaseType
	var configRaw []byte
	var query string
	var args []interface{}

	if version > 0 {
		query = `
			SELECT id, code, version, name, description, config,
			       status, created_at, updated_at, deprecated_at
			FROM case_types
			WHERE code = $1 AND version = $2 AND status = 'ACTIVE'`
		args = []interface{}{code, version}
	} else {
		query = `
			SELECT id, code, version, name, description, config,
			       status, created_at, updated_at, deprecated_at
			FROM case_types
			WHERE code = $1 AND status = 'ACTIVE'
			ORDER BY version DESC
			LIMIT 1`
		args = []interface{}{code}
	}

	err := tx.QueryRow(ctx, query, args...).Scan(
		&ct.ID, &ct.Code, &ct.Version, &ct.Name, &ct.Description, &configRaw,
		&ct.Status, &ct.CreatedAt, &ct.UpdatedAt, &ct.DeprecatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("case_type %s (v%d) not found or not ACTIVE: %w", code, version, err)
	}
	if err := json.Unmarshal(configRaw, &ct.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal case_type config: %w", err)
	}
	return &ct, nil
}

// InsertCaseInstance inserts a new case row and returns the generated ID and reference_number.
func (r *Repository) InsertCaseInstance(
	ctx context.Context, tx DBExecutor,
	ci *model.CaseInstance,
) error {
	if tx == nil {
		tx = r.Pool
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO cases (
			case_type_id, case_type_version,
			parent_case_id, current_stage_code, current_stage_ordinal,
			status, metadata, assigned_to
		) VALUES (
			$1::uuid, $2,
			$3, $4, $5,
			$6, $7, $8
		)
		RETURNING id, reference_number, created_at, updated_at, row_version`,
		ci.CaseTypeID, ci.CaseTypeVersion,
		ci.ParentCaseID, ci.CurrentStageCode, ci.CurrentStageOrdinal,
		ci.Status, ci.Metadata, ci.AssignedTo,
	).Scan(&ci.ID, &ci.ReferenceNumber, &ci.CreatedAt, &ci.UpdatedAt, &ci.RowVersion)

	if err != nil {
		return fmt.Errorf("failed to insert case: %w", err)
	}
	return nil
}
