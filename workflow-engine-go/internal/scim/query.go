package scim

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

func shiftPlaceholders(clause string, offset int) string {
	if offset == 0 || strings.TrimSpace(clause) == "" {
		return clause
	}
	return placeholderPattern.ReplaceAllStringFunc(clause, func(m string) string {
		n, err := strconv.Atoi(strings.TrimPrefix(m, "$"))
		if err != nil {
			return m
		}
		return fmt.Sprintf("$%d", n+offset)
	})
}

func userSortClause(sortBy, sortOrder string) string {
	direction := "ASC"
	if strings.EqualFold(sortOrder, "descending") {
		direction = "DESC"
	}
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "username":
		return "LOWER(u.username) " + direction + ", u.user_id ASC"
	case "displayname":
		return "u.display_name " + direction + ", u.user_id ASC"
	case "externalid":
		return "u.external_id " + direction + " NULLS LAST, u.user_id ASC"
	case "meta.created":
		return "u.created_at " + direction + ", u.user_id ASC"
	case "meta.lastmodified":
		return "u.updated_at " + direction + ", u.user_id ASC"
	default:
		return "u.display_name ASC, u.user_id ASC"
	}
}

func groupSortClause(sortBy, sortOrder string) string {
	direction := "ASC"
	if strings.EqualFold(sortOrder, "descending") {
		direction = "DESC"
	}
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "displayname":
		return "g.display_name " + direction + ", g.team_id ASC"
	case "externalid":
		return "g.external_id " + direction + " NULLS LAST, g.team_id ASC"
	case "meta.created":
		return "g.created_at " + direction + ", g.team_id ASC"
	case "meta.lastmodified":
		return "g.updated_at " + direction + ", g.team_id ASC"
	default:
		return "g.display_name ASC, g.team_id ASC"
	}
}

func listSCIMUsers(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filterExpr string,
	sortBy string,
	sortOrder string,
	startIndex int,
	count int,
	baseURL string,
	includeExtension bool,
) ([]SCIMUser, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("listSCIMUsers: db is nil")
	}
	startIndex, count = sanitizePagination(startIndex, count)
	offset := startIndex - 1

	args := []interface{}{tenantID}
	where := []string{"u.tenant_id = $1::uuid"}
	if strings.TrimSpace(filterExpr) != "" {
		parsed, err := ParseSCIMFilter(filterExpr)
		if err != nil {
			return nil, 0, fmt.Errorf("listSCIMUsers: %w", err)
		}
		clause, filterArgs, err := parsed.ToSQL(SCIMResourceTypeUser)
		if err != nil {
			return nil, 0, fmt.Errorf("listSCIMUsers: %w", err)
		}
		if strings.TrimSpace(clause) != "" {
			where = append(where, shiftPlaceholders(clause, len(args)))
			args = append(args, filterArgs...)
		}
	}

	sortClause := userSortClause(sortBy, sortOrder)
	args = append(args, count, offset)
	query := fmt.Sprintf(`
		WITH filtered AS (
			SELECT u.user_id
			FROM users u
			WHERE %s
		)
		SELECT
			u.user_id::text AS user_id,
			u.tenant_id::text AS tenant_id,
			u.username,
			u.email,
			u.display_name,
			u.status,
			u.auth_provider,
			u.external_id,
			u.locale,
			u.timezone,
			u.created_at,
			u.updated_at,
			u.version,
			COALESCE(array_agg(DISTINCT r.role_code) FILTER (WHERE r.role_code IS NOT NULL), '{}') AS roles,
			MIN(tm.team_id::text) AS primary_team_id,
			COUNT(*) OVER() AS total_count
		FROM filtered f
		JOIN users u
		  ON u.user_id = f.user_id
		LEFT JOIN user_roles ur
		  ON ur.tenant_id = u.tenant_id
		 AND ur.user_id = u.user_id
		LEFT JOIN roles r
		  ON r.tenant_id = ur.tenant_id
		 AND r.role_id = ur.role_id
		LEFT JOIN team_members tm
		  ON tm.tenant_id = u.tenant_id
		 AND tm.user_id = u.user_id
		GROUP BY u.user_id, u.tenant_id, u.username, u.email, u.display_name, u.status,
		         u.auth_provider, u.external_id, u.locale, u.timezone, u.created_at, u.updated_at, u.version
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), sortClause, len(args)-1, len(args))

	rows := make([]scimUserListRow, 0)
	if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, 0, fmt.Errorf("listSCIMUsers: query users: %w", err)
	}

	resources := make([]SCIMUser, 0, len(rows))
	total := 0
	for i := range rows {
		if i == 0 {
			total = rows[i].TotalCount
		}
		resources = append(resources, mapUserRowToSCIM(rows[i].scimUserRow, rows[i].Roles, strings.TrimSpace(rows[i].PrimaryTeam.String), userLocation(baseURL, rows[i].UserID), includeExtension))
	}
	return resources, total, nil
}

func listSCIMGroups(
	ctx context.Context,
	db *sqlx.DB,
	tenantID string,
	filterExpr string,
	sortBy string,
	sortOrder string,
	startIndex int,
	count int,
	baseURL string,
) ([]SCIMGroup, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("listSCIMGroups: db is nil")
	}
	startIndex, count = sanitizePagination(startIndex, count)
	offset := startIndex - 1

	args := []interface{}{tenantID}
	where := []string{"g.tenant_id = $1::uuid"}
	if strings.TrimSpace(filterExpr) != "" {
		parsed, err := ParseSCIMFilter(filterExpr)
		if err != nil {
			return nil, 0, fmt.Errorf("listSCIMGroups: %w", err)
		}
		clause, filterArgs, err := parsed.ToSQL(SCIMResourceTypeGroup)
		if err != nil {
			return nil, 0, fmt.Errorf("listSCIMGroups: %w", err)
		}
		if strings.TrimSpace(clause) != "" {
			where = append(where, shiftPlaceholders(clause, len(args)))
			args = append(args, filterArgs...)
		}
	}

	sortClause := groupSortClause(sortBy, sortOrder)
	args = append(args, count, offset)
	query := fmt.Sprintf(`
		SELECT
			g.team_id::text AS team_id,
			g.tenant_id::text AS tenant_id,
			g.external_id,
			g.display_name,
			g.created_at,
			g.updated_at,
			g.version,
			COALESCE(array_agg(tm.user_id::text) FILTER (WHERE tm.user_id IS NOT NULL), '{}') AS members,
			COUNT(*) OVER() AS total_count
		FROM teams g
		LEFT JOIN team_members tm
		  ON tm.tenant_id = g.tenant_id
		 AND tm.team_id = g.team_id
		WHERE %s
		GROUP BY g.team_id, g.tenant_id, g.external_id, g.display_name, g.created_at, g.updated_at, g.version
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), sortClause, len(args)-1, len(args))

	rows := make([]scimGroupListRow, 0)
	if err := db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, 0, fmt.Errorf("listSCIMGroups: query groups: %w", err)
	}

	groups := make([]SCIMGroup, 0, len(rows))
	total := 0
	for i := range rows {
		if i == 0 {
			total = rows[i].TotalCount
		}
		members := make([]SCIMGroupMember, 0, len(rows[i].Members))
		for _, m := range rows[i].Members {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			members = append(members, SCIMGroupMember{Value: m})
		}
		group := SCIMGroup{
			Schemas:     []string{SchemaCoreGroup},
			ID:          rows[i].TeamID,
			DisplayName: rows[i].DisplayName,
			Members:     members,
			Meta: SCIMMeta{
				ResourceType: "Group",
				Created:      formatRFC3339UTC(rows[i].CreatedAt),
				LastModified: formatRFC3339UTC(rows[i].UpdatedAt),
				Location:     groupLocation(baseURL, rows[i].TeamID),
				Version:      SCIMETag(rows[i].Version),
			},
		}
		if rows[i].ExternalID.Valid {
			group.ExternalID = strings.TrimSpace(rows[i].ExternalID.String)
		}
		groups = append(groups, group)
	}
	return groups, total, nil
}
