package repository

import (
	"context"
	"fmt"
	"strings"
)

// CaseListFilters represents the query parameters for listing cases.
type CaseListFilters struct {
	TenantID   string
	Scope      string   // "my", "team", "all"
	UserID     string   // set when scope = "my"
	TeamIDs    []string // set when scope = "team"
	Status     []string
	Stage      []string
	CaseType   string
	DateFrom   string
	DateTo     string
	Query      string   // text search on reference_number + metadata->>'borrower_name'
	SLAStatus  []string // "ON_TRACK", "WARNING", "BREACHED"
	Priority   []string
	AssignedTo string
	Tags       []string
	Page       int
	Limit      int
}

// CaseTagItem represents an active tag on a case.
type CaseTagItem struct {
	TagCode  string  `json:"tagCode"`
	TagValue *string `json:"tagValue"`
}

// CaseListItem is the shape returned to the frontend for each row.
type CaseListItem struct {
	CaseID          string        `json:"id"`
	ReferenceNumber string        `json:"referenceNumber"`
	CaseType        string        `json:"caseType"`
	Status          string        `json:"status"`
	CurrentStage    *string       `json:"currentStage"`
	Priority        *string       `json:"priority"`
	BorrowerName    *string       `json:"borrowerName"`
	AssignedTo      *string       `json:"assignedTo"`
	AssignedToName  *string       `json:"assignedToName"`
	SLAStatus       string        `json:"slaStatus"`
	SLARemainingMin float64       `json:"slaRemainingMinutes"`
	CreatedAt       string        `json:"createdAt"`
	UpdatedAt       string        `json:"updatedAt"`
	Tags            []CaseTagItem `json:"tags"`
}

// ListCasesResult wraps paged results with total count for the frontend Pagination component.
type ListCasesResult struct {
	Items      []CaseListItem `json:"items"`
	TotalCount int            `json:"total"`
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
}

// ListCases fetches a paginated list of cases with optional filters.
// All filters are applied at the database level. Text search hits reference_number
// and metadata->>'borrower_name' (case-insensitive prefix match).
func (r *Repository) ListCases(ctx context.Context, filters CaseListFilters) (*ListCasesResult, error) {
	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.Limit <= 0 || filters.Limit > 100 {
		filters.Limit = 25
	}
	offset := (filters.Page - 1) * filters.Limit

	args := []interface{}{filters.TenantID}
	argIdx := 2

	clauses := []string{"c.tenant_id = $1::uuid"}

	// Scope filter
	if filters.Scope == "my" && filters.UserID != "" {
		args = append(args, filters.UserID)
		clauses = append(clauses, fmt.Sprintf("c.assigned_user_id = $%d::uuid", argIdx))
		argIdx++
	} else if filters.Scope == "team" && len(filters.TeamIDs) > 0 {
		args = append(args, filters.TeamIDs)
		clauses = append(clauses, fmt.Sprintf("c.assigned_team_id = ANY($%d::uuid[])", argIdx))
		argIdx++
	}

	// Status filter
	if len(filters.Status) > 0 {
		args = append(args, filters.Status)
		clauses = append(clauses, fmt.Sprintf("c.status = ANY($%d::text[])", argIdx))
		argIdx++
	}

	// Stage filter
	if len(filters.Stage) > 0 {
		args = append(args, filters.Stage)
		clauses = append(clauses, fmt.Sprintf("c.current_stage_code = ANY($%d::text[])", argIdx))
		argIdx++
	}

	// CaseType filter
	if filters.CaseType != "" {
		args = append(args, filters.CaseType)
		clauses = append(clauses, fmt.Sprintf("ct.code = $%d", argIdx))
		argIdx++
	}

	// Date range
	if filters.DateFrom != "" {
		args = append(args, filters.DateFrom)
		clauses = append(clauses, fmt.Sprintf("c.created_at >= $%d::timestamptz", argIdx))
		argIdx++
	}
	if filters.DateTo != "" {
		args = append(args, filters.DateTo)
		clauses = append(clauses, fmt.Sprintf("c.created_at <= $%d::timestamptz", argIdx))
		argIdx++
	}

	// Text search: reference_number OR borrower_name in metadata
	if filters.Query != "" {
		searchTerm := "%" + strings.ToLower(filters.Query) + "%"
		args = append(args, searchTerm)
		clauses = append(clauses, fmt.Sprintf(
			"(LOWER(c.reference_number) LIKE $%d OR LOWER(c.metadata->>'borrower_name') LIKE $%d)",
			argIdx, argIdx))
		argIdx++
	}

	// AssignedTo filter
	if filters.AssignedTo != "" {
		args = append(args, filters.AssignedTo)
		clauses = append(clauses, fmt.Sprintf("c.assigned_user_id = $%d::uuid", argIdx))
		argIdx++
	}

	// Tags filter: case must have ALL specified tags active
	for _, tag := range filters.Tags {
		args = append(args, tag)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM case_tags ct2
			WHERE ct2.case_id = c.id AND ct2.tag_code = $%d AND ct2.removed_at IS NULL
		)`, argIdx))
		argIdx++
	}

	where := "WHERE " + strings.Join(clauses, " AND ")

	// SLA classification inline (no sla_logs dependency).
	// Computes remaining minutes from case_due_at (or created_at + 5 days if unset).
	slaExpr := `
		EXTRACT(EPOCH FROM (
			COALESCE(c.case_due_at, c.created_at + interval '5 days') - now()
		)) / 60.0`
	slaStatusExpr := fmt.Sprintf(`
		CASE
			WHEN (%s) <= 0   THEN 'BREACHED'
			WHEN (%s) < 480  THEN 'WARNING'
			ELSE 'ON_TRACK'
		END`, slaExpr, slaExpr)

	// Total count
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		%s
	`, where)

	var total int
	if err := r.Pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("ListCases: count: %w", err)
	}

	// SLA status filter applied AFTER count (uses HAVING equivalent)
	slaClause := ""
	if len(filters.SLAStatus) > 0 {
		args = append(args, filters.SLAStatus)
		slaClause = fmt.Sprintf("HAVING (%s) = ANY($%d::text[])", slaStatusExpr, argIdx)
		argIdx++
	}

	// Priority filter — stored in metadata->>'priority'
	if len(filters.Priority) > 0 {
		args = append(args, filters.Priority)
		clauses = append(clauses, fmt.Sprintf("c.metadata->>'priority' = ANY($%d::text[])", argIdx))
		argIdx++
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	args = append(args, filters.Limit, offset)
	limitArg := argIdx
	offsetArg := argIdx + 1

	dataQuery := fmt.Sprintf(`
		SELECT
			c.id::text,
			c.reference_number,
			ct.code,
			c.status,
			c.current_stage_code,
			c.metadata->>'priority',
			c.metadata->>'borrower_name',
			c.assigned_user_id::text,
			u.display_name AS assigned_to_name,
			%s AS sla_status,
			COALESCE(%s, 0) AS sla_remaining_minutes,
			to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			to_char(c.updated_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		LEFT JOIN users u ON u.id = c.assigned_user_id::text
		%s
		GROUP BY c.id, ct.code, u.display_name
		%s
		ORDER BY c.updated_at DESC
		LIMIT $%d OFFSET $%d
	`, slaStatusExpr, slaExpr, where, slaClause, limitArg, offsetArg)

	rows, err := r.Pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("ListCases: query: %w", err)
	}
	defer rows.Close()

	var items []CaseListItem
	for rows.Next() {
		var item CaseListItem
		if err := rows.Scan(
			&item.CaseID, &item.ReferenceNumber, &item.CaseType,
			&item.Status, &item.CurrentStage,
			&item.Priority, &item.BorrowerName, &item.AssignedTo,
			&item.AssignedToName,
			&item.SLAStatus, &item.SLARemainingMin,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListCases: scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListCases: rows: %w", err)
	}
	if items == nil {
		items = []CaseListItem{}
	}

	// Batch-fetch active tags for all returned cases.
	if len(items) > 0 {
		caseIDs := make([]string, len(items))
		for i, item := range items {
			caseIDs[i] = item.CaseID
		}

		tagQuery := `
			SELECT case_id::text, tag_code, tag_value
			FROM case_tags
			WHERE tenant_id = $1::uuid
			  AND case_id = ANY($2::uuid[])
			  AND removed_at IS NULL
			ORDER BY tagged_at ASC
		`
		tagRows, err := r.Pool.Query(ctx, tagQuery, filters.TenantID, caseIDs)
		if err != nil {
			return nil, fmt.Errorf("ListCases: tags query: %w", err)
		}
		defer tagRows.Close()

		tagMap := make(map[string][]CaseTagItem)
		for tagRows.Next() {
			var caseID, tagCode string
			var tagValue *string
			if err := tagRows.Scan(&caseID, &tagCode, &tagValue); err != nil {
				return nil, fmt.Errorf("ListCases: tags scan: %w", err)
			}
			tagMap[caseID] = append(tagMap[caseID], CaseTagItem{
				TagCode:  tagCode,
				TagValue: tagValue,
			})
		}
		if err := tagRows.Err(); err != nil {
			return nil, fmt.Errorf("ListCases: tags rows: %w", err)
		}

		for i := range items {
			if tags, ok := tagMap[items[i].CaseID]; ok {
				items[i].Tags = tags
			} else {
				items[i].Tags = []CaseTagItem{}
			}
		}
	}

	return &ListCasesResult{
		Items:      items,
		TotalCount: total,
		Page:       filters.Page,
		Limit:      filters.Limit,
	}, nil
}

// CaseSummaryStats holds aggregate metrics for cases in a tenant.
type CaseSummaryStats struct {
	TotalCases    int `json:"totalCases"`
	ActiveCases   int `json:"activeCases"`
	ResolvedCases int `json:"resolvedCases"`
	AtRiskCases   int `json:"atRiskCases"`
	MyActiveCases int `json:"myActiveCases"`
}

// GetCaseSummaryStats computes dashboard overview counters for the given user and tenant.
func (r *Repository) GetCaseSummaryStats(ctx context.Context, tenantID, userID string) (*CaseSummaryStats, error) {
	query := `
		SELECT
			COUNT(*) AS total_cases,
			COUNT(*) FILTER (WHERE status NOT IN ('COMPLETED', 'WITHDRAWN')) AS active_cases,
			COUNT(*) FILTER (WHERE status IN ('COMPLETED', 'WITHDRAWN')) AS resolved_cases,
			COUNT(*) FILTER (WHERE assigned_user_id = $2::uuid AND status NOT IN ('COMPLETED', 'WITHDRAWN')) AS my_active_cases,
			COUNT(*) FILTER (
			    WHERE status NOT IN ('COMPLETED', 'WITHDRAWN') AND
			    (EXTRACT(EPOCH FROM (COALESCE(case_due_at, created_at + interval '5 days') - now())) / 60.0) < 480
			) AS at_risk_cases
		FROM cases
		WHERE tenant_id = $1::uuid
	`
	var stats CaseSummaryStats

	// Use a fallback empty UUID for $2 if userID is empty to avoid SQL syntactical UUID cast panics
	if userID == "" {
		userID = "00000000-0000-0000-0000-000000000000"
	}

	if err := r.Pool.QueryRow(ctx, query, tenantID, userID).Scan(
		&stats.TotalCases,
		&stats.ActiveCases,
		&stats.ResolvedCases,
		&stats.MyActiveCases,
		&stats.AtRiskCases,
	); err != nil {
		return nil, fmt.Errorf("GetCaseSummaryStats: %w", err)
	}
	return &stats, nil
}
