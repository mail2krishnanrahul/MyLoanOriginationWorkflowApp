package document

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

var (
	firstTokenPattern = regexp.MustCompile(`\{first(\d+)\}`)
	lastTokenPattern  = regexp.MustCompile(`\{last(\d+)\}`)
)

// RedactSensitiveData applies read-time redaction policies for unauthorized roles.
func RedactSensitiveData(
	ctx context.Context,
	db *sqlx.DB,
	data map[string]interface{},
	requestor Actor,
) (map[string]interface{}, error) {
	if db == nil {
		return nil, fmt.Errorf("RedactSensitiveData: db is nil")
	}
	cloned, err := copyMap(data)
	if err != nil {
		return nil, fmt.Errorf("RedactSensitiveData: clone input data: %w", err)
	}
	if requestor.IsSystem {
		return cloned, nil
	}

	type sensitiveRuleRow struct {
		FieldPath       string `db:"field_path"`
		RedactionRule   string `db:"redaction_rule"`
		MaskPattern     *string `db:"mask_pattern"`
		AllowedRolesJSON string `db:"allowed_roles_json"`
	}
	var rows []sensitiveRuleRow
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			field_path,
			redaction_rule,
			mask_pattern,
			COALESCE(array_to_json(allowed_roles)::text, '[]') AS allowed_roles_json
		FROM sensitive_fields
	`); err != nil {
		return nil, fmt.Errorf("RedactSensitiveData: load sensitive fields: %w", err)
	}

	role := strings.ToUpper(strings.TrimSpace(requestor.Role))
	for _, row := range rows {
		fieldPath := strings.TrimSpace(row.FieldPath)
		if fieldPath == "" {
			continue
		}
		value, found := getByPath(cloned, fieldPath)
		if !found {
			continue
		}

		var allowedRoles []string
		if err := json.Unmarshal([]byte(row.AllowedRolesJSON), &allowedRoles); err != nil {
			return nil, fmt.Errorf("RedactSensitiveData: decode allowed roles for %s: %w", fieldPath, err)
		}
		if containsRole(allowedRoles, role) {
			continue
		}

		rule := model.RedactionRule(strings.ToUpper(strings.TrimSpace(row.RedactionRule)))
		switch rule {
		case model.RedactionRuleMask:
			maskPattern := "***"
			if row.MaskPattern != nil && strings.TrimSpace(*row.MaskPattern) != "" {
				maskPattern = strings.TrimSpace(*row.MaskPattern)
			}
			masked := applyMaskPattern(toJSONString(value), maskPattern)
			_ = setByPath(cloned, fieldPath, masked)
		case model.RedactionRuleTruncate:
			truncated := applyTruncateRule(toJSONString(value), row.MaskPattern)
			_ = setByPath(cloned, fieldPath, truncated)
		case model.RedactionRuleHide:
			_ = deleteByPath(cloned, fieldPath)
		default:
			return nil, fmt.Errorf("RedactSensitiveData: unsupported redaction rule %s for %s", row.RedactionRule, fieldPath)
		}
	}
	return cloned, nil
}

func applyMaskPattern(value string, pattern string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "***"
	}

	masked := pattern
	masked = strings.ReplaceAll(masked, "{domain}", extractDomain(value))
	masked = strings.ReplaceAll(masked, "{first3}", firstN(value, 3))
	masked = strings.ReplaceAll(masked, "{last4}", lastN(value, 4))

	masked = firstTokenPattern.ReplaceAllStringFunc(masked, func(token string) string {
		match := firstTokenPattern.FindStringSubmatch(token)
		if len(match) != 2 {
			return ""
		}
		n, err := strconv.Atoi(match[1])
		if err != nil || n <= 0 {
			return ""
		}
		return firstN(value, n)
	})
	masked = lastTokenPattern.ReplaceAllStringFunc(masked, func(token string) string {
		match := lastTokenPattern.FindStringSubmatch(token)
		if len(match) != 2 {
			return ""
		}
		n, err := strconv.Atoi(match[1])
		if err != nil || n <= 0 {
			return ""
		}
		return lastN(value, n)
	})

	return masked
}

func applyTruncateRule(value string, pattern *string) string {
	if value == "" {
		return value
	}

	keepPrefix := 2
	keepSuffix := 2
	if pattern != nil && strings.TrimSpace(*pattern) != "" {
		parts := strings.Split(strings.TrimSpace(*pattern), ":")
		if len(parts) == 2 {
			if parsed, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && parsed >= 0 {
				keepPrefix = parsed
			}
			if parsed, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && parsed >= 0 {
				keepSuffix = parsed
			}
		}
	}

	if len(value) <= keepPrefix+keepSuffix {
		return value
	}
	return fmt.Sprintf("%s***%s", firstN(value, keepPrefix), lastN(value, keepSuffix))
}

func firstN(value string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n])
}

func lastN(value string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[len(runes)-n:])
}

func extractDomain(value string) string {
	at := strings.LastIndex(value, "@")
	if at < 0 || at == len(value)-1 {
		return ""
	}
	return value[at+1:]
}
