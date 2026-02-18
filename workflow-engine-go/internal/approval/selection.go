package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

var dynamicRulePattern = regexp.MustCompile(`(?i)^\s*if\s+(.+)\s+then\s+([A-Za-z0-9_]+)\s+else\s+([A-Za-z0-9_]+)\s*$`)
var thousandSuffixPattern = regexp.MustCompile(`(?i)\b([0-9]+(?:\.[0-9]+)?)k\b`)

// SelectApprovers returns eligible approver user IDs for a gate.
func SelectApprovers(
	ctx context.Context,
	db *sqlx.DB,
	gate model.ApprovalGate,
	caseData model.CaseInstance,
) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("SelectApprovers: db is nil")
	}

	eligibleAmount := gate.AuthorityLimit
	if gate.ApprovalAmount != nil {
		eligibleAmount = gate.ApprovalAmount
	}

	switch gate.ApproverSelection {
	case model.ApproverSelectionExplicitList:
		seed, err := decodeApproverSeeds(gate.Approvers)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: decode explicit approvers: %w", err)
		}
		ids, err := filterUsersByAuthority(ctx, db, seed, eligibleAmount)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: explicit list authority filter: %w", err)
		}
		if len(ids) == 0 {
			return nil, model.ErrNoEligibleApprover
		}
		return ids, nil

	case model.ApproverSelectionRoleBased:
		roles, err := decodeApproverSeeds(gate.Approvers)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: decode role list: %w", err)
		}
		ids, err := selectUsersByRoles(ctx, db, roles, eligibleAmount)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: role-based lookup: %w", err)
		}
		if len(ids) == 0 {
			return nil, model.ErrNoEligibleApprover
		}
		return ids, nil

	case model.ApproverSelectionReportingChain:
		if caseData.AssignedTo == nil || strings.TrimSpace(*caseData.AssignedTo) == "" {
			return nil, model.ErrNoEligibleApprover
		}
		ids, err := findInReportingChain(ctx, db, *caseData.AssignedTo, gate.Approvers, eligibleAmount)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: reporting chain lookup: %w", err)
		}
		if len(ids) == 0 {
			return nil, model.ErrNoEligibleApprover
		}
		return ids, nil

	case model.ApproverSelectionDynamicRule:
		if gate.DynamicRuleExpression == nil || strings.TrimSpace(*gate.DynamicRuleExpression) == "" {
			return nil, fmt.Errorf("SelectApprovers: dynamic rule expression is required")
		}
		metadata, err := decodeCaseMetadata(caseData.Metadata)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: decode case metadata: %w", err)
		}
		role, err := resolveDynamicRole(metadata, strings.TrimSpace(*gate.DynamicRuleExpression))
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: dynamic rule evaluation: %w", err)
		}
		ids, err := selectUsersByRoles(ctx, db, []string{role}, eligibleAmount)
		if err != nil {
			return nil, fmt.Errorf("SelectApprovers: dynamic role lookup: %w", err)
		}
		if len(ids) == 0 {
			return nil, model.ErrNoEligibleApprover
		}
		return ids, nil

	default:
		return nil, fmt.Errorf("SelectApprovers: unsupported strategy %s", gate.ApproverSelection)
	}
}

func decodeCaseMetadata(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	data := map[string]interface{}{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func decodeApproverSeeds(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var seeds []string
	if err := json.Unmarshal(raw, &seeds); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seeds))
	seen := make(map[string]struct{}, len(seeds))
	for _, v := range seeds {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

func selectUsersByRoles(ctx context.Context, db *sqlx.DB, roles []string, amount *float64) ([]string, error) {
	if len(roles) == 0 {
		return []string{}, nil
	}

	base := `
		SELECT DISTINCT u.id
		FROM users u
		WHERE u.status = 'ACTIVE'
		  AND u.role_code IN (?)
	`
	args := []interface{}{roles}

	if amount != nil {
		base += `
		  AND EXISTS (
			SELECT 1
			FROM user_authority ua
			WHERE ua.user_id = u.id
			  AND ua.revoked_at IS NULL
			  AND (ua.expires_at IS NULL OR ua.expires_at > now())
			  AND ua.max_amount >= ?
		  )
		`
		args = append(args, *amount)
	}

	query, inArgs, err := sqlx.In(base, args...)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var ids []string
	if err := db.SelectContext(ctx, &ids, query, inArgs...); err != nil {
		return nil, err
	}
	return ids, nil
}

func filterUsersByAuthority(ctx context.Context, db *sqlx.DB, userIDs []string, amount *float64) ([]string, error) {
	if len(userIDs) == 0 {
		return []string{}, nil
	}

	if amount == nil {
		query, args, err := sqlx.In(`
			SELECT id
			FROM users
			WHERE status = 'ACTIVE'
			  AND id IN (?)
		`, userIDs)
		if err != nil {
			return nil, err
		}
		query = db.Rebind(query)
		var out []string
		if err := db.SelectContext(ctx, &out, query, args...); err != nil {
			return nil, err
		}
		return out, nil
	}

	query, args, err := sqlx.In(`
		SELECT DISTINCT u.id
		FROM users u
		JOIN user_authority ua ON ua.user_id = u.id
		WHERE u.status = 'ACTIVE'
		  AND u.id IN (?)
		  AND ua.revoked_at IS NULL
		  AND (ua.expires_at IS NULL OR ua.expires_at > now())
		  AND ua.max_amount >= ?
	`, userIDs, *amount)
	if err != nil {
		return nil, err
	}
	query = db.Rebind(query)

	var out []string
	if err := db.SelectContext(ctx, &out, query, args...); err != nil {
		return nil, err
	}
	return out, nil
}

func findInReportingChain(
	ctx context.Context,
	db *sqlx.DB,
	startUserID string,
	allowedRolesRaw json.RawMessage,
	amount *float64,
) ([]string, error) {
	allowedRoles, err := decodeApproverSeeds(allowedRolesRaw)
	if err != nil {
		return nil, err
	}
	roleFilter := make(map[string]struct{}, len(allowedRoles))
	for _, role := range allowedRoles {
		roleFilter[role] = struct{}{}
	}

	current := startUserID
	for i := 0; i < 20; i++ {
		var managerID sql.NullString
		err := db.GetContext(ctx, &managerID, `
			SELECT manager_id
			FROM users
			WHERE id = $1
		`, current)
		if err != nil {
			return nil, err
		}
		if !managerID.Valid || strings.TrimSpace(managerID.String) == "" {
			return []string{}, nil
		}

		var mgr struct {
			ID       string `db:"id"`
			RoleCode string `db:"role_code"`
			Status   string `db:"status"`
		}
		err = db.GetContext(ctx, &mgr, `
			SELECT id, role_code, status
			FROM users
			WHERE id = $1
		`, managerID.String)
		if err != nil {
			return nil, err
		}
		if mgr.Status != "ACTIVE" {
			current = managerID.String
			continue
		}
		if len(roleFilter) > 0 {
			if _, ok := roleFilter[mgr.RoleCode]; !ok {
				current = managerID.String
				continue
			}
		}

		if amount != nil {
			var count int
			err = db.GetContext(ctx, &count, `
				SELECT COUNT(*)
				FROM user_authority
				WHERE user_id = $1
				  AND revoked_at IS NULL
				  AND (expires_at IS NULL OR expires_at > now())
				  AND max_amount >= $2
			`, mgr.ID, *amount)
			if err != nil {
				return nil, err
			}
			if count == 0 {
				current = managerID.String
				continue
			}
		}

		return []string{mgr.ID}, nil
	}

	return []string{}, nil
}

func resolveDynamicRole(metadata map[string]interface{}, rule string) (string, error) {
	match := dynamicRulePattern.FindStringSubmatch(rule)
	if len(match) != 4 {
		return "", fmt.Errorf("unsupported dynamic rule format: %s", rule)
	}

	condition := strings.TrimSpace(match[1])
	condition = thousandSuffixPattern.ReplaceAllStringFunc(condition, func(v string) string {
		raw := strings.TrimSuffix(strings.ToLower(v), "k")
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return v
		}
		return fmt.Sprintf("%.0f", n*1000)
	})
	trueRole := strings.TrimSpace(match[2])
	falseRole := strings.TrimSpace(match[3])

	evaluator := &ExpressionEvaluator{}
	ok, err := evaluator.Evaluate(context.Background(), condition, metadata)
	if err != nil {
		return "", err
	}
	if ok {
		return trueRole, nil
	}
	return falseRole, nil
}
