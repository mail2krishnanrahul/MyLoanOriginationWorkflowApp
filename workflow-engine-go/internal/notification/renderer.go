package notification

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"
)

// TemplateRenderer renders notification templates with variable interpolation.
type TemplateRenderer struct {
	funcMap texttemplate.FuncMap
}

// NewTemplateRenderer returns a renderer with default helper functions.
func NewTemplateRenderer() *TemplateRenderer {
	funcs := texttemplate.FuncMap{
		"formatDate":     formatDate,
		"formatCurrency": formatCurrency,
		"toUpper":        toUpper,
		"truncate":       truncate,
	}
	return &TemplateRenderer{funcMap: funcs}
}

// Render parses and executes a template with the given context.
func (r *TemplateRenderer) Render(
	ctx context.Context,
	templateText string,
	contextData map[string]interface{},
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("Render: %w", err)
	}
	if strings.TrimSpace(templateText) == "" {
		return "", fmt.Errorf("Render: template text is empty")
	}
	if contextData == nil {
		contextData = map[string]interface{}{}
	}

	tmpl, err := texttemplate.New("notification").Funcs(r.effectiveFuncMap()).Option("missingkey=error").Parse(templateText)
	if err != nil {
		return "", fmt.Errorf("Render: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, contextData); err != nil {
		if isMissingVariableError(err) {
			return "", fmt.Errorf("Render: %w: %v", ErrMissingTemplateVariable, err)
		}
		return "", fmt.Errorf("Render: execute template: %w", err)
	}
	return buf.String(), nil
}

// renderHTML renders with html/template to enforce HTML escaping for email bodies.
func (r *TemplateRenderer) renderHTML(
	ctx context.Context,
	templateText string,
	contextData map[string]interface{},
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("renderHTML: %w", err)
	}
	if strings.TrimSpace(templateText) == "" {
		return "", fmt.Errorf("renderHTML: template text is empty")
	}
	if contextData == nil {
		contextData = map[string]interface{}{}
	}

	funcMap := htmltemplate.FuncMap{}
	for k, v := range r.effectiveFuncMap() {
		funcMap[k] = v
	}

	tmpl, err := htmltemplate.New("notification_html").Funcs(funcMap).Option("missingkey=error").Parse(templateText)
	if err != nil {
		return "", fmt.Errorf("renderHTML: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, contextData); err != nil {
		if isMissingVariableError(err) {
			return "", fmt.Errorf("renderHTML: %w: %v", ErrMissingTemplateVariable, err)
		}
		return "", fmt.Errorf("renderHTML: execute template: %w", err)
	}
	return buf.String(), nil
}

// ValidateTemplate checks if a template is syntactically valid without executing it.
func (r *TemplateRenderer) ValidateTemplate(templateText string) error {
	if strings.TrimSpace(templateText) == "" {
		return fmt.Errorf("ValidateTemplate: template text is empty")
	}
	_, err := texttemplate.New("notification_validate").Funcs(r.effectiveFuncMap()).Parse(templateText)
	if err != nil {
		return fmt.Errorf("ValidateTemplate: %w", err)
	}
	return nil
}

func (r *TemplateRenderer) effectiveFuncMap() texttemplate.FuncMap {
	if r == nil || r.funcMap == nil {
		return NewTemplateRenderer().funcMap
	}
	return r.funcMap
}

func isMissingVariableError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "map has no entry for key") ||
		strings.Contains(msg, "can't evaluate field") ||
		errors.Is(err, ErrMissingTemplateVariable)
}

func formatDate(value interface{}, layouts ...string) (string, error) {
	layout := time.RFC3339
	if len(layouts) > 0 && strings.TrimSpace(layouts[0]) != "" {
		layout = layouts[0]
	}
	tm, err := coerceTime(value)
	if err != nil {
		return "", fmt.Errorf("formatDate: %w", err)
	}
	return tm.UTC().Format(layout), nil
}

func formatCurrency(value interface{}, symbols ...string) (string, error) {
	symbol := "$"
	if len(symbols) > 0 && strings.TrimSpace(symbols[0]) != "" {
		symbol = symbols[0]
	}
	amt, err := coerceFloat(value)
	if err != nil {
		return "", fmt.Errorf("formatCurrency: %w", err)
	}

	negative := amt < 0
	if negative {
		amt = -amt
	}
	whole := int64(amt)
	fraction := int64((amt-float64(whole))*100 + 0.5)

	formatted := fmt.Sprintf("%s%s.%02d", symbol, withThousandsSeparator(whole), fraction)
	if negative {
		formatted = "-" + formatted
	}
	return formatted, nil
}

func toUpper(value interface{}) string {
	return strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
}

func truncate(value interface{}, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(fmt.Sprint(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max])
}

func coerceTime(value interface{}) (time.Time, error) {
	switch t := value.(type) {
	case time.Time:
		return t, nil
	case *time.Time:
		if t == nil {
			return time.Time{}, fmt.Errorf("nil time pointer")
		}
		return *t, nil
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return time.Time{}, fmt.Errorf("empty time string")
		}
		layouts := []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00"}
		for _, layout := range layouts {
			parsed, err := time.Parse(layout, trimmed)
			if err == nil {
				return parsed, nil
			}
		}
		return time.Time{}, fmt.Errorf("unsupported time format %q", t)
	default:
		return time.Time{}, fmt.Errorf("unsupported time type %T", value)
	}
}

func coerceFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid numeric string: %w", err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", value)
	}
}

func withThousandsSeparator(v int64) string {
	if v == 0 {
		return "0"
	}
	parts := make([]string, 0)
	for v > 0 {
		chunk := v % 1000
		v = v / 1000
		if v > 0 {
			parts = append(parts, fmt.Sprintf("%03d", chunk))
		} else {
			parts = append(parts, fmt.Sprintf("%d", chunk))
		}
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ",")
}
