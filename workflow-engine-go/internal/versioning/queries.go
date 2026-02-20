package versioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"workflow-engine/internal/multitenancy"

	"github.com/jmoiron/sqlx"
)

// GetActiveCaseTypeVersion returns the single ACTIVE version for code.
func GetActiveCaseTypeVersion(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
) (CaseTypeVersion, error) {
	if db == nil {
		return CaseTypeVersion{}, fmt.Errorf("GetActiveCaseTypeVersion: db is nil")
	}
	caseTypeCode = strings.ToUpper(strings.TrimSpace(caseTypeCode))
	if caseTypeCode == "" {
		return CaseTypeVersion{}, fmt.Errorf("GetActiveCaseTypeVersion: caseTypeCode is required")
	}
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}

	var row struct {
		ID           string     `db:"id"`
		TenantID     *string    `db:"tenant_id"`
		Code         string     `db:"code"`
		Version      int        `db:"version"`
		Name         string     `db:"name"`
		Description  *string    `db:"description"`
		ConfigRaw    []byte     `db:"config"`
		Status       string     `db:"status"`
		CreatedAt    time.Time  `db:"created_at"`
		UpdatedAt    time.Time  `db:"updated_at"`
		ActivatedAt  *time.Time `db:"activated_at"`
		ActivatedBy  *string    `db:"activated_by"`
		DeprecatedAt *time.Time `db:"deprecated_at"`
		DeprecatedBy *string    `db:"deprecated_by"`
	}
	ownedQuery, ownedArgs, scopeErr := multitenancy.AssertTenantScope(ctx, `
		SELECT
			id::text AS id,
			tenant_id::text AS tenant_id,
			code,
			version,
			name,
			description,
			config,
			status,
			created_at,
			updated_at,
			activated_at,
			activated_by,
			deprecated_at,
			deprecated_by
		FROM case_types
		WHERE code = $1
		  AND status = 'ACTIVE'
		  AND tenant_id IS NOT NULL
	`, []interface{}{caseTypeCode})
	if scopeErr != nil {
		return CaseTypeVersion{}, fmt.Errorf("GetActiveCaseTypeVersion: %w", scopeErr)
	}

	query := fmt.Sprintf(`
		SELECT *
		FROM (
			%s
			UNION ALL
			SELECT
				id::text AS id,
				tenant_id::text AS tenant_id,
				code,
				version,
				name,
				description,
				config,
				status,
				created_at,
				updated_at,
				activated_at,
				activated_by,
				deprecated_at,
				deprecated_by
			FROM case_types
			WHERE code = $1
			  AND status = 'ACTIVE'
			  AND tenant_id IS NULL
		) ct
		ORDER BY CASE WHEN ct.tenant_id IS NULL THEN 1 ELSE 0 END, ct.version DESC
		LIMIT 1
	`, ownedQuery)

	if err := db.GetContext(ctx, &row, query, ownedArgs...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CaseTypeVersion{}, ErrNoActiveVersion
		}
		return CaseTypeVersion{}, fmt.Errorf("GetActiveCaseTypeVersion: query active version: %w", err)
	}

	config := CaseTypeConfig{}
	if err := json.Unmarshal(row.ConfigRaw, &config); err != nil {
		return CaseTypeVersion{}, fmt.Errorf("GetActiveCaseTypeVersion: unmarshal config: %w", err)
	}

	return CaseTypeVersion{
		ID:           row.ID,
		TenantID:     row.TenantID,
		Code:         row.Code,
		Version:      row.Version,
		Name:         row.Name,
		Description:  row.Description,
		Config:       config,
		Status:       CaseTypeStatus(row.Status),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
		ActivatedAt:  row.ActivatedAt,
		ActivatedBy:  row.ActivatedBy,
		DeprecatedAt: row.DeprecatedAt,
		DeprecatedBy: row.DeprecatedBy,
	}, nil
}

// ListCaseTypeVersions returns all versions for code ordered by version desc.
func ListCaseTypeVersions(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeCode string,
	page, size int,
) ([]CaseTypeVersion, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListCaseTypeVersions: db is nil")
	}
	caseTypeCode = strings.ToUpper(strings.TrimSpace(caseTypeCode))
	if caseTypeCode == "" {
		return nil, 0, fmt.Errorf("ListCaseTypeVersions: caseTypeCode is required")
	}
	if _, err := multitenancy.TenantFromContext(ctx); err != nil {
		ctx = multitenancy.WithTenant(ctx, multitenancy.DefaultTenantID)
	}

	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int
	ownedCountQuery, ownedCountArgs, scopeErr := multitenancy.AssertTenantScope(ctx, `
		SELECT COUNT(*)::int AS c
		FROM case_types
		WHERE code = $1
		  AND tenant_id IS NOT NULL
	`, []interface{}{caseTypeCode})
	if scopeErr != nil {
		return nil, 0, fmt.Errorf("ListCaseTypeVersions: %w", scopeErr)
	}
	countQuery := fmt.Sprintf(`
		SELECT COALESCE(SUM(c), 0)::int
		FROM (
			%s
			UNION ALL
			SELECT COUNT(*)::int AS c
			FROM case_types
			WHERE code = $1
			  AND tenant_id IS NULL
		) counts
	`, ownedCountQuery)
	if err := db.GetContext(ctx, &total, countQuery, ownedCountArgs...); err != nil {
		return nil, 0, fmt.Errorf("ListCaseTypeVersions: count versions: %w", err)
	}

	rows := make([]struct {
		ID           string     `db:"id"`
		TenantID     *string    `db:"tenant_id"`
		Code         string     `db:"code"`
		Version      int        `db:"version"`
		Name         string     `db:"name"`
		Description  *string    `db:"description"`
		ConfigRaw    []byte     `db:"config"`
		Status       string     `db:"status"`
		CreatedAt    time.Time  `db:"created_at"`
		UpdatedAt    time.Time  `db:"updated_at"`
		ActivatedAt  *time.Time `db:"activated_at"`
		ActivatedBy  *string    `db:"activated_by"`
		DeprecatedAt *time.Time `db:"deprecated_at"`
		DeprecatedBy *string    `db:"deprecated_by"`
	}, 0)

	ownedListQuery, ownedListArgs, scopeErr := multitenancy.AssertTenantScope(ctx, `
		SELECT
			id::text AS id,
			tenant_id::text AS tenant_id,
			code,
			version,
			name,
			description,
			config,
			status,
			created_at,
			updated_at,
			activated_at,
			activated_by,
			deprecated_at,
			deprecated_by
		FROM case_types
		WHERE code = $1
		  AND tenant_id IS NOT NULL
	`, []interface{}{caseTypeCode})
	if scopeErr != nil {
		return nil, 0, fmt.Errorf("ListCaseTypeVersions: %w", scopeErr)
	}
	listQuery := fmt.Sprintf(`
		SELECT *
		FROM (
			%s
			UNION ALL
			SELECT
				id::text AS id,
				tenant_id::text AS tenant_id,
				code,
				version,
				name,
				description,
				config,
				status,
				created_at,
				updated_at,
				activated_at,
				activated_by,
				deprecated_at,
				deprecated_by
			FROM case_types
			WHERE code = $1
			  AND tenant_id IS NULL
		) ct
		ORDER BY ct.version DESC, CASE WHEN ct.tenant_id IS NULL THEN 1 ELSE 0 END
		LIMIT $3 OFFSET $4
	`, ownedListQuery)
	listArgs := append(ownedListArgs, size, offset)
	if err := db.SelectContext(ctx, &rows, listQuery, listArgs...); err != nil {
		return nil, 0, fmt.Errorf("ListCaseTypeVersions: query versions: %w", err)
	}

	result := make([]CaseTypeVersion, 0, len(rows))
	for _, row := range rows {
		cfg := CaseTypeConfig{}
		if err := json.Unmarshal(row.ConfigRaw, &cfg); err != nil {
			return nil, 0, fmt.Errorf("ListCaseTypeVersions: unmarshal config for case_type %s v%d: %w", row.Code, row.Version, err)
		}
		result = append(result, CaseTypeVersion{
			ID:           row.ID,
			TenantID:     row.TenantID,
			Code:         row.Code,
			Version:      row.Version,
			Name:         row.Name,
			Description:  row.Description,
			Config:       cfg,
			Status:       CaseTypeStatus(row.Status),
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
			ActivatedAt:  row.ActivatedAt,
			ActivatedBy:  row.ActivatedBy,
			DeprecatedAt: row.DeprecatedAt,
			DeprecatedBy: row.DeprecatedBy,
		})
	}
	if result == nil {
		return []CaseTypeVersion{}, total, nil
	}
	return result, total, nil
}

// GetCaseTypeVersionDiff returns a stored immutable diff or ErrDiffNotFound.
func GetCaseTypeVersionDiff(
	ctx context.Context,
	db *sqlx.DB,
	fromVerID string,
	toVerID string,
) (CaseTypeVersionDiff, error) {
	if db == nil {
		return CaseTypeVersionDiff{}, fmt.Errorf("GetCaseTypeVersionDiff: db is nil")
	}
	fromVerID = strings.TrimSpace(fromVerID)
	toVerID = strings.TrimSpace(toVerID)
	if fromVerID == "" || toVerID == "" {
		return CaseTypeVersionDiff{}, fmt.Errorf("GetCaseTypeVersionDiff: fromVerID and toVerID are required")
	}

	var row struct {
		DiffID        string    `db:"diff_id"`
		CaseTypeCode  string    `db:"case_type_code"`
		FromVersionID string    `db:"from_case_type_id"`
		ToVersionID   string    `db:"to_case_type_id"`
		FromVersion   int       `db:"from_version"`
		ToVersion     int       `db:"to_version"`
		DiffJSON      []byte    `db:"diff_json"`
		ComputedBy    string    `db:"computed_by"`
		ComputedAt    time.Time `db:"computed_at"`
	}
	if err := db.GetContext(ctx, &row, `
		SELECT
			diff_id::text AS diff_id,
			case_type_code,
			from_case_type_id::text AS from_case_type_id,
			to_case_type_id::text AS to_case_type_id,
			from_version,
			to_version,
			diff_json,
			computed_by,
			computed_at
		FROM case_type_version_diffs
		WHERE from_case_type_id = $1::uuid
		  AND to_case_type_id = $2::uuid
	`, fromVerID, toVerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CaseTypeVersionDiff{}, ErrDiffNotFound
		}
		return CaseTypeVersionDiff{}, fmt.Errorf("GetCaseTypeVersionDiff: query stored diff: %w", err)
	}

	diff := CaseTypeVersionDiff{}
	if len(row.DiffJSON) > 0 {
		if err := json.Unmarshal(row.DiffJSON, &diff); err != nil {
			return CaseTypeVersionDiff{}, fmt.Errorf("GetCaseTypeVersionDiff: unmarshal diff json: %w", err)
		}
	}
	diff.DiffID = row.DiffID
	diff.CaseTypeCode = row.CaseTypeCode
	diff.FromVersionID = row.FromVersionID
	diff.ToVersionID = row.ToVersionID
	diff.FromVersion = row.FromVersion
	diff.ToVersion = row.ToVersion
	diff.ComputedBy = row.ComputedBy
	diff.ComputedAt = row.ComputedAt
	return diff, nil
}

// GetCaseTypeAuditLog returns append-only audit entries ordered by occurred_at descending.
func GetCaseTypeAuditLog(
	ctx context.Context,
	db *sqlx.DB,
	caseTypeID string,
	page, size int,
) ([]CaseTypeAuditEntry, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("GetCaseTypeAuditLog: db is nil")
	}
	caseTypeID = strings.TrimSpace(caseTypeID)
	if caseTypeID == "" {
		return nil, 0, fmt.Errorf("GetCaseTypeAuditLog: caseTypeID is required")
	}

	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	var total int
	if err := db.GetContext(ctx, &total, `
		SELECT COUNT(*)
		FROM case_type_audit_log
		WHERE case_type_id = $1::uuid
	`, caseTypeID); err != nil {
		return nil, 0, fmt.Errorf("GetCaseTypeAuditLog: count audit entries: %w", err)
	}

	rows := make([]CaseTypeAuditEntry, 0)
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			audit_id::text AS audit_id,
			case_type_id::text AS case_type_id,
			action,
			actor,
			changed_fields,
			previous_value,
			new_value,
			occurred_at
		FROM case_type_audit_log
		WHERE case_type_id = $1::uuid
		ORDER BY occurred_at DESC, audit_id DESC
		LIMIT $2 OFFSET $3
	`, caseTypeID, size, offset); err != nil {
		return nil, 0, fmt.Errorf("GetCaseTypeAuditLog: query audit entries: %w", err)
	}
	if rows == nil {
		return []CaseTypeAuditEntry{}, total, nil
	}
	return rows, total, nil
}
