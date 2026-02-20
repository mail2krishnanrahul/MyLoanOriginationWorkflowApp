package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-engine/internal/multitenancy"
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
	tenantID, tenantErr := multitenancy.TenantFromContext(ctx)
	hasTenant := tenantErr == nil

	if version > 0 {
		query = `
			SELECT id, tenant_id::text AS tenant_id, code, version, name, description, config,
			       status, created_at, updated_at, deprecated_at
			FROM case_types
			WHERE code = $1 AND version = $2 AND status = 'ACTIVE'`
		args = []interface{}{code, version}
		if hasTenant {
			query += " AND (tenant_id IS NULL OR tenant_id = $3::uuid)"
			args = append(args, tenantID)
		}
	} else {
		query = `
			SELECT id, tenant_id::text AS tenant_id, code, version, name, description, config,
			       status, created_at, updated_at, deprecated_at
			FROM case_types
			WHERE code = $1 AND status = 'ACTIVE'`
		args = []interface{}{code}
		if hasTenant {
			query += " AND (tenant_id IS NULL OR tenant_id = $2::uuid)"
			args = append(args, tenantID)
		}
		query += `
			ORDER BY CASE WHEN tenant_id IS NULL THEN 1 ELSE 0 END, version DESC
			LIMIT 1`
	}

	err := tx.QueryRow(ctx, query, args...).Scan(
		&ct.ID, &ct.TenantID, &ct.Code, &ct.Version, &ct.Name, &ct.Description, &configRaw,
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

	if ci.TenantID == "" {
		if tenantID, err := multitenancy.TenantFromContext(ctx); err == nil {
			ci.TenantID = tenantID
		} else {
			ci.TenantID = multitenancy.DefaultTenantID
		}
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO cases (
			tenant_id,
			case_type_id, case_type_version, case_type_version_id,
			parent_case_id, current_stage_code, current_stage_ordinal,
			status, metadata, assigned_to
		) VALUES (
			$1::uuid,
			$2::uuid, $3, $4::uuid,
			$5, $6, $7,
			$8, $9, $10
		)
		RETURNING id, reference_number, created_at, updated_at, row_version`,
		ci.TenantID,
		ci.CaseTypeID, ci.CaseTypeVersion, ci.CaseTypeID,
		ci.ParentCaseID, ci.CurrentStageCode, ci.CurrentStageOrdinal,
		ci.Status, ci.Metadata, ci.AssignedTo,
	).Scan(&ci.ID, &ci.ReferenceNumber, &ci.CreatedAt, &ci.UpdatedAt, &ci.RowVersion)

	if err != nil {
		return fmt.Errorf("failed to insert case: %w", err)
	}
	return nil
}
