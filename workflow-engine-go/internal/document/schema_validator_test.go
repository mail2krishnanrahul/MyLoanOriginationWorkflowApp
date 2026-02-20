package document

import (
	"context"
	"errors"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/stretchr/testify/assert"
)

func TestValidateTaskInput(t *testing.T) {
	taskDef := model.TaskDefinitionV2{
		Code: "CREDIT_CHECK",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"borrower_ssn": map[string]interface{}{
					"type":    "string",
					"pattern": "^\\d{3}-\\d{2}-\\d{4}$",
				},
				"credit_bureau": map[string]interface{}{
					"type": "string",
					"enum": []interface{}{"EQUIFAX", "EXPERIAN", "TRANSUNION"},
				},
				"details": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"score": map[string]interface{}{
							"type":    "integer",
							"minimum": 300,
							"maximum": 850,
						},
					},
					"required": []interface{}{"score"},
				},
			},
			"required": []interface{}{"borrower_ssn", "credit_bureau"},
		},
	}

	tests := []struct {
		name    string
		payload map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid payload passes",
			payload: map[string]interface{}{
				"borrower_ssn":   "123-45-6789",
				"credit_bureau":  "EQUIFAX",
				"details":        map[string]interface{}{"score": 760},
			},
			wantErr: false,
		},
		{
			name: "missing required field fails",
			payload: map[string]interface{}{
				"borrower_ssn": "123-45-6789",
			},
			wantErr: true,
		},
		{
			name: "type mismatch fails",
			payload: map[string]interface{}{
				"borrower_ssn":  "123-45-6789",
				"credit_bureau": 42,
			},
			wantErr: true,
		},
		{
			name: "enum violation fails",
			payload: map[string]interface{}{
				"borrower_ssn":  "123-45-6789",
				"credit_bureau": "OTHER",
			},
			wantErr: true,
		},
		{
			name: "pattern violation fails",
			payload: map[string]interface{}{
				"borrower_ssn":  "123456789",
				"credit_bureau": "EXPERIAN",
			},
			wantErr: true,
		},
		{
			name: "nested object validation fails",
			payload: map[string]interface{}{
				"borrower_ssn":  "123-45-6789",
				"credit_bureau": "TRANSUNION",
				"details":       map[string]interface{}{},
			},
			wantErr: true,
		},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTaskInput(context.Background(), validator, taskDef, tt.payload)
			if tt.wantErr {
				assert.Error(t, err)
				var validationErr *ValidationError
				assert.True(t, errors.As(err, &validationErr))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateTaskOutput(t *testing.T) {
	taskDef := model.TaskDefinitionV2{
		Code: "CREDIT_CHECK",
		OutputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"credit_score": map[string]interface{}{
					"type":    "integer",
					"minimum": 300,
					"maximum": 850,
				},
				"credit_report_url": map[string]interface{}{
					"type":   "string",
					"format": "uri",
				},
			},
			"required": []interface{}{"credit_score"},
		},
	}

	tests := []struct {
		name    string
		payload map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid output",
			payload: map[string]interface{}{
				"credit_score":      740,
				"credit_report_url": "https://example.com/report/123",
			},
		},
		{
			name: "missing credit score",
			payload: map[string]interface{}{
				"credit_report_url": "https://example.com/report/123",
			},
			wantErr: true,
		},
		{
			name: "score out of range",
			payload: map[string]interface{}{
				"credit_score": 900,
			},
			wantErr: true,
		},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTaskOutput(context.Background(), validator, taskDef, tt.payload)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
