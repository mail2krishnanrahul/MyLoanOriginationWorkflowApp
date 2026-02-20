package document

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestRedactSensitiveData(t *testing.T) {
	tests := []struct {
		name      string
		requestor Actor
		input     map[string]interface{}
		setup     func(sqlmock.Sqlmock)
		assertFn  func(t *testing.T, got map[string]interface{}, original map[string]interface{})
		wantErr   bool
	}{
		{
			name:      "authorized user no redaction",
			requestor: Actor{ID: "u-1", Role: "UNDERWRITER"},
			input: map[string]interface{}{
				"borrower_ssn": "123-45-6789",
			},
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"field_path", "redaction_rule", "mask_pattern", "allowed_roles_json"}).
					AddRow("borrower_ssn", "MASK", "***-**-{last4}", `["UNDERWRITER"]`)
				mock.ExpectQuery(regexp.QuoteMeta("FROM sensitive_fields")).WillReturnRows(rows)
			},
			assertFn: func(t *testing.T, got map[string]interface{}, original map[string]interface{}) {
				assert.Equal(t, "123-45-6789", got["borrower_ssn"])
				assert.Equal(t, "123-45-6789", original["borrower_ssn"])
			},
		},
		{
			name:      "unauthorized user redaction applied",
			requestor: Actor{ID: "u-2", Role: "CASE_OWNER"},
			input: map[string]interface{}{
				"borrower_ssn": "123-45-6789",
				"email":        "john@example.com",
				"metadata": map[string]interface{}{
					"bank_account_number": "000111222333",
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"field_path", "redaction_rule", "mask_pattern", "allowed_roles_json"}).
					AddRow("borrower_ssn", "MASK", "***-**-{last4}", `["UNDERWRITER"]`).
					AddRow("email", "TRUNCATE", "3:2", `["SUPERVISOR"]`).
					AddRow("metadata.bank_account_number", "HIDE", nil, `["SUPERVISOR"]`)
				mock.ExpectQuery(regexp.QuoteMeta("FROM sensitive_fields")).WillReturnRows(rows)
			},
			assertFn: func(t *testing.T, got map[string]interface{}, original map[string]interface{}) {
				assert.Equal(t, "***-**-6789", got["borrower_ssn"])
				assert.Equal(t, "joh***om", got["email"])
				metadata, ok := got["metadata"].(map[string]interface{})
				assert.True(t, ok)
				_, exists := metadata["bank_account_number"]
				assert.False(t, exists)

				originalMetadata := original["metadata"].(map[string]interface{})
				assert.Equal(t, "000111222333", originalMetadata["bank_account_number"])
			},
		},
		{
			name:      "nested field special character mask",
			requestor: Actor{ID: "u-3", Role: "AUDITOR"},
			input: map[string]interface{}{
				"metadata": map[string]interface{}{
					"borrower": map[string]interface{}{
						"card_number": "4111111111111234",
					},
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"field_path", "redaction_rule", "mask_pattern", "allowed_roles_json"}).
					AddRow("metadata.borrower.card_number", "MASK", "****-****-****-{last4}", `["SUPERVISOR"]`)
				mock.ExpectQuery(regexp.QuoteMeta("FROM sensitive_fields")).WillReturnRows(rows)
			},
			assertFn: func(t *testing.T, got map[string]interface{}, original map[string]interface{}) {
				metadata := got["metadata"].(map[string]interface{})
				borrower := metadata["borrower"].(map[string]interface{})
				assert.Equal(t, "****-****-****-1234", borrower["card_number"])

				originalBorrower := original["metadata"].(map[string]interface{})["borrower"].(map[string]interface{})
				assert.Equal(t, "4111111111111234", originalBorrower["card_number"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			tt.setup(mock)
			original, err := copyMap(tt.input)
			assert.NoError(t, err)

			got, err := RedactSensitiveData(context.Background(), db, tt.input, tt.requestor)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				tt.assertFn(t, got, original)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
