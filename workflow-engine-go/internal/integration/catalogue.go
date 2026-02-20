package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

var (
	catalogueSchemaCache sync.Map
)

// EventPayloadValidationError surfaces catalogue JSON schema violations.
type EventPayloadValidationError struct {
	EventType  string   `json:"event_type"`
	Violations []string `json:"violations"`
}

func (e *EventPayloadValidationError) Error() string {
	if e == nil {
		return "event payload validation failed"
	}
	if len(e.Violations) == 0 {
		return fmt.Sprintf("event payload validation failed for %s", e.EventType)
	}
	return fmt.Sprintf("event payload validation failed for %s: %s", e.EventType, strings.Join(e.Violations, "; "))
}

// ValidateEventPayload validates payload against event_type_catalogue JSON Schema.
func ValidateEventPayload(
	ctx context.Context,
	db *sqlx.DB,
	eventType string,
	payload []byte,
) error {
	if db == nil {
		return fmt.Errorf("ValidateEventPayload: db is nil")
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return fmt.Errorf("ValidateEventPayload: eventType is required")
	}
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	if !json.Valid(payload) {
		return fmt.Errorf("ValidateEventPayload: payload is not valid JSON")
	}

	var schemaBytes []byte
	if err := db.GetContext(ctx, &schemaBytes, `
		SELECT payload_schema
		FROM event_type_catalogue
		WHERE event_type_code = $1
	`, eventType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownEventType
		}
		return fmt.Errorf("ValidateEventPayload: load event schema: %w", err)
	}
	compiled, err := compileCatalogueSchema(schemaBytes)
	if err != nil {
		return fmt.Errorf("ValidateEventPayload: compile schema: %w", err)
	}

	var payloadAny interface{}
	if err := json.Unmarshal(payload, &payloadAny); err != nil {
		return fmt.Errorf("ValidateEventPayload: unmarshal payload: %w", err)
	}
	if err := compiled.Validate(payloadAny); err != nil {
		validationErr := &EventPayloadValidationError{EventType: eventType, Violations: []string{err.Error()}}
		var vErr *jsonschema.ValidationError
		if errors.As(err, &vErr) {
			violations := make([]string, 0)
			flattenSchemaValidationErrors(vErr, &violations)
			if len(violations) > 0 {
				validationErr.Violations = violations
			}
		}
		return validationErr
	}
	return nil
}

func flattenSchemaValidationErrors(err *jsonschema.ValidationError, violations *[]string) {
	if err == nil {
		return
	}
	if len(err.Causes) == 0 {
		location := strings.TrimSpace(err.InstanceLocation)
		if location == "" {
			location = "$"
		}
		*violations = append(*violations, fmt.Sprintf("%s: %s", location, strings.TrimSpace(err.Message)))
		return
	}
	for i := range err.Causes {
		flattenSchemaValidationErrors(err.Causes[i], violations)
	}
}

func compileCatalogueSchema(schemaBytes []byte) (*jsonschema.Schema, error) {
	sum := sha256.Sum256(schemaBytes)
	key := hex.EncodeToString(sum[:])
	if cached, ok := catalogueSchemaCache.Load(key); ok {
		if compiled, castOK := cached.(*jsonschema.Schema); castOK && compiled != nil {
			return compiled, nil
		}
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("catalogue-schema.json", bytes.NewReader(schemaBytes)); err != nil {
		return nil, err
	}
	compiled, err := compiler.Compile("catalogue-schema.json")
	if err != nil {
		return nil, err
	}
	catalogueSchemaCache.Store(key, compiled)
	return compiled, nil
}

// ListEventTypes returns paginated event catalogue rows.
func ListEventTypes(
	ctx context.Context,
	db *sqlx.DB,
	direction EventDirection,
	page, size int,
) ([]EventTypeCatalogueEntry, int, error) {
	if db == nil {
		return nil, 0, fmt.Errorf("ListEventTypes: db is nil")
	}
	page, size = normalizePagination(page, size)
	offset := (page - 1) * size

	where := ""
	args := make([]interface{}, 0, 2)
	if strings.TrimSpace(string(direction)) != "" {
		switch direction {
		case EventDirectionEmitted:
			where = "WHERE direction IN ('EMITTED', 'BOTH')"
		case EventDirectionConsumed:
			where = "WHERE direction IN ('CONSUMED', 'BOTH')"
		case EventDirectionBoth:
			where = ""
		default:
			return nil, 0, fmt.Errorf("ListEventTypes: unsupported direction %s", direction)
		}
	}

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*)::int
		FROM event_type_catalogue
		%s
	`, where)
	var total int
	if err := db.GetContext(ctx, &total, countQuery, args...); err != nil {
		return nil, 0, fmt.Errorf("ListEventTypes: count rows: %w", err)
	}

	listQuery := fmt.Sprintf(`
		SELECT
			event_type_code,
			direction,
			description,
			payload_schema,
			introduced_in_version,
			deprecated_at,
			example_payload,
			created_at,
			updated_at
		FROM event_type_catalogue
		%s
		ORDER BY event_type_code ASC
		LIMIT $1 OFFSET $2
	`, where)
	listArgs := append(args, size, offset)
	rows := make([]EventTypeCatalogueEntry, 0)
	if err := db.SelectContext(ctx, &rows, listQuery, listArgs...); err != nil {
		return nil, 0, fmt.Errorf("ListEventTypes: query rows: %w", err)
	}
	if rows == nil {
		rows = []EventTypeCatalogueEntry{}
	}
	return rows, total, nil
}
