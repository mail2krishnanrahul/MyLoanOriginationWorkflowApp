package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"workflow-engine/pkg/model"

	"github.com/xeipuuv/gojsonschema"
)

// SchemaValidator caches compiled JSON Schema documents.
type SchemaValidator struct {
	cache map[string]*gojsonschema.Schema
	mu    sync.RWMutex
}

var defaultSchemaValidator = &SchemaValidator{
	cache: map[string]*gojsonschema.Schema{},
}

// NewSchemaValidator constructs a fresh schema validator cache.
func NewSchemaValidator() *SchemaValidator {
	return &SchemaValidator{cache: map[string]*gojsonschema.Schema{}}
}

// DefaultSchemaValidator returns the process-wide validator instance.
func DefaultSchemaValidator() *SchemaValidator {
	return defaultSchemaValidator
}

// ValidateAgainstSchema validates data against a JSON Schema.
func (v *SchemaValidator) ValidateAgainstSchema(
	ctx context.Context,
	schema map[string]interface{},
	data map[string]interface{},
) error {
	_ = ctx
	if v == nil {
		return fmt.Errorf("ValidateAgainstSchema: validator is nil")
	}
	if len(schema) == 0 {
		return nil
	}

	schemaKey, err := schemaCacheKey(schema)
	if err != nil {
		return fmt.Errorf("ValidateAgainstSchema: schema cache key: %w", err)
	}

	compiled, err := v.loadOrCompile(schemaKey, schema)
	if err != nil {
		return fmt.Errorf("ValidateAgainstSchema: compile schema: %w", err)
	}

	result, err := compiled.Validate(gojsonschema.NewGoLoader(data))
	if err != nil {
		return fmt.Errorf("ValidateAgainstSchema: execute validation: %w", err)
	}
	if result.Valid() {
		return nil
	}

	violations := make([]SchemaViolation, 0, len(result.Errors()))
	for _, item := range result.Errors() {
		violations = append(violations, SchemaViolation{
			Field:       item.Field(),
			Description: item.Description(),
			Value:       item.Value(),
		})
	}
	return &ValidationError{
		Operation:  "ValidateAgainstSchema",
		Violations: violations,
	}
}

// ValidateTaskInput validates a task's input payload.
func ValidateTaskInput(
	ctx context.Context,
	validator *SchemaValidator,
	taskDef model.TaskDefinitionV2,
	payload map[string]interface{},
) error {
	if len(taskDef.InputSchema) == 0 {
		return nil
	}
	if validator == nil {
		validator = DefaultSchemaValidator()
	}
	if err := validator.ValidateAgainstSchema(ctx, taskDef.InputSchema, payload); err != nil {
		return fmt.Errorf("ValidateTaskInput: %w", err)
	}
	return nil
}

// ValidateTaskOutput validates a task's output payload.
func ValidateTaskOutput(
	ctx context.Context,
	validator *SchemaValidator,
	taskDef model.TaskDefinitionV2,
	payload map[string]interface{},
) error {
	if len(taskDef.OutputSchema) == 0 {
		return nil
	}
	if validator == nil {
		validator = DefaultSchemaValidator()
	}
	if err := validator.ValidateAgainstSchema(ctx, taskDef.OutputSchema, payload); err != nil {
		return fmt.Errorf("ValidateTaskOutput: %w", err)
	}
	return nil
}

func (v *SchemaValidator) loadOrCompile(key string, schema map[string]interface{}) (*gojsonschema.Schema, error) {
	v.mu.RLock()
	compiled, ok := v.cache[key]
	v.mu.RUnlock()
	if ok && compiled != nil {
		return compiled, nil
	}

	schemaLoader := gojsonschema.NewGoLoader(schema)
	compiled, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	v.cache[key] = compiled
	v.mu.Unlock()
	return compiled, nil
}

func schemaCacheKey(schema map[string]interface{}) (string, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
