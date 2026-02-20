package versioning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"

	"workflow-engine/pkg/model"
)

func newSQLXMock(t *testing.T) (*sqlx.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	return sqlx.NewDb(sqlDB, "sqlmock"), mock
}

func mustJSONRaw(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	assert.NoError(t, err)
	return raw
}

func validCaseTypeConfig() CaseTypeConfig {
	return CaseTypeConfig{
		Stages: []StageDefinition{
			{
				Code:          "INITIAL_REVIEW",
				Name:          "Initial Review",
				SequenceOrder: 1,
				Activities: []model.ActivityConfig{
					{
						Code:          "DOC_CHECK",
						Name:          "Document Check",
						SequenceOrder: 1,
						TaskDefs: []TaskDefinition{
							{
								Code:          "VERIFY_DOCS",
								Name:          "Verify Documents",
								Type:          "SYSTEM",
								Required:      true,
								SequenceOrder: 1,
								Config:        mustJSONRawNoTest(map[string]interface{}{"assigned_service": "DOCUMENT_SERVICE"}),
								RetryPolicy: &RetryPolicy{
									MaxRetries:          3,
									BackoffStrategy:     "FIXED",
									BaseIntervalSeconds: 10,
									MaxIntervalSeconds:  60,
								},
							},
						},
					},
				},
			},
		},
	}
}

func invalidCaseTypeConfigWithMultipleViolations() CaseTypeConfig {
	return CaseTypeConfig{
		Stages: []StageDefinition{
			{
				Code:          "",
				Name:          "Broken Stage",
				SequenceOrder: 2,
				Activities:    []model.ActivityConfig{},
			},
		},
	}
}

func mustJSONRawNoTest(v interface{}) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func TestImmutabilityEnforcement_AssertCaseTypeIsMutable(t *testing.T) {
	tests := []struct {
		name      string
		statusRow string
		dbErr     error
		wantErr   bool
		wantType  bool
	}{
		{
			name:      "happy path draft is mutable",
			statusRow: "DRAFT",
			wantErr:   false,
		},
		{
			name:      "edge active is immutable",
			statusRow: "ACTIVE",
			wantErr:   true,
			wantType:  true,
		},
		{
			name:    "failure db error",
			dbErr:   errors.New("db unavailable"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := newSQLXMock(t)
			defer db.Close()

			q := mock.ExpectQuery(regexp.QuoteMeta("SELECT status\n\t\tFROM case_types\n\t\tWHERE id = $1::uuid"))
			q.WithArgs("ct-1")
			if tt.dbErr != nil {
				q.WillReturnError(tt.dbErr)
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(tt.statusRow))
			}

			err := AssertCaseTypeIsMutable(context.Background(), db, "ct-1")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if tt.wantType {
				var immutableErr *ImmutableCaseTypeError
				assert.ErrorAs(t, err, &immutableErr)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVersionPinning_ResolveCaseTypeConfig_DeprecatedPinnedVersion(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	configRaw := mustJSONRaw(t, map[string]interface{}{
		"stages": []map[string]interface{}{
			{
				"code":           "INITIAL_REVIEW",
				"name":           "Initial Review",
				"sequence_order": 1,
				"activities": []map[string]interface{}{
					{"code": "DOC_CHECK", "name": "Doc Check", "sequence_order": 1, "task_definitions": []interface{}{}},
				},
			},
		},
	})

	mock.ExpectQuery(regexp.QuoteMeta("SELECT ct.config\n\t\tFROM cases c\n\t\tJOIN case_types ct\n\t\t  ON ct.id = COALESCE(c.case_type_version_id, c.case_type_id)\n\t\tWHERE c.id = $1::uuid")).
		WithArgs("case-1").
		WillReturnRows(sqlmock.NewRows([]string{"config"}).AddRow(configRaw))

	cfg, err := ResolveCaseTypeConfig(context.Background(), db, "case-1")
	assert.NoError(t, err)
	assert.Len(t, cfg.Stages, 1)
	assert.Equal(t, "INITIAL_REVIEW", cfg.Stages[0].Code)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestConfigurationAuditTrail_RecordCaseTypeAudit_OutsideTransaction(t *testing.T) {
	err := RecordCaseTypeAudit(context.Background(), nil, CaseTypeAuditEntry{CaseTypeID: "ct-1", Action: CaseTypeAuditActionCreated, Actor: "tester"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tx is nil")
}

func TestVersionQueryFunctions_ListCaseTypeVersions_PaginationBeyondTotalCount(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)SELECT COALESCE.*FROM case_types WHERE code = \$1`).
		WithArgs("HOME_LOAN", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery(`(?s)FROM case_types.*WHERE code = \$1`).
		WithArgs("HOME_LOAN", sqlmock.AnyArg(), 10, 90).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "code", "version", "name", "description", "config", "status", "created_at", "updated_at", "activated_at", "activated_by", "deprecated_at", "deprecated_by",
		}))

	rows, total, err := ListCaseTypeVersions(context.Background(), db, "home_loan", 10, 10)
	assert.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Empty(t, rows)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestVersionQueryFunctions_GetActiveCaseTypeVersion_NoActiveVersion(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM case_types.*WHERE code = \$1\s+AND status = 'ACTIVE'`).
		WithArgs("HOME_LOAN", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	_, err := GetActiveCaseTypeVersion(context.Background(), db, "home_loan")
	assert.ErrorIs(t, err, ErrNoActiveVersion)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestVersionQueryFunctions_GetCaseTypeVersionDiff_NotFound(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	mock.ExpectQuery(`(?s)FROM case_type_version_diffs\s+WHERE from_case_type_id = \$1::uuid\s+AND to_case_type_id = \$2::uuid`).
		WithArgs("ct-v1", "ct-v2").
		WillReturnError(sql.ErrNoRows)

	_, err := GetCaseTypeVersionDiff(context.Background(), db, "ct-v1", "ct-v2")
	assert.ErrorIs(t, err, ErrDiffNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestActivationGate_ActivateCaseTypeVersion_FirstActivationNoPreviousActive(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	cfg := validCaseTypeConfig()
	cfgRaw := mustJSONRaw(t, cfg)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM case_types\s+WHERE id = \$1::uuid\s+FOR UPDATE`).
		WithArgs("ct-draft-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "code", "version", "name", "description", "config", "status", "created_at", "updated_at", "activated_at", "activated_by", "deprecated_at", "deprecated_by",
		}).AddRow("ct-draft-1", "HOME_LOAN", 2, "Home Loan v2", "desc", cfgRaw, "DRAFT", now, now, nil, nil, nil, nil))

	mock.ExpectQuery(`(?s)SELECT id::text\s+FROM case_types\s+WHERE code = \$1\s+FOR UPDATE`).
		WithArgs("HOME_LOAN").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ct-draft-1"))

	mock.ExpectQuery(`(?s)FROM activity_definitions\s+WHERE case_type_id = \$1::uuid\s+AND case_type_version = \$2`).
		WithArgs("ct-draft-1", 2).
		WillReturnRows(sqlmock.NewRows([]string{"stage_code", "activity_code"}).AddRow("INITIAL_REVIEW", "DOC_CHECK"))

	mock.ExpectQuery(`(?s)FROM case_types\s+WHERE code = \$1\s+AND status = 'ACTIVE'\s+AND id <> \$2::uuid`).
		WithArgs("HOME_LOAN", "ct-draft-1").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectExec(`(?s)UPDATE case_types\s+SET status = 'ACTIVE'`).
		WithArgs(sqlmock.AnyArg(), "architect", "ct-draft-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).
		WithArgs(sqlmock.AnyArg(), nil, nil, "CASE_TYPE_ACTIVATED", sqlmock.AnyArg(), "PENDING", "case-orchestrator", 5, nil, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs(sqlmock.AnyArg(), "CASE_TYPE_ACTIVATED", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectQuery(`(?s)INSERT INTO case_type_audit_log`).
		WithArgs("ct-draft-1", "ACTIVATED", "architect", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"audit_id"}).AddRow("audit-1"))

	mock.ExpectCommit()

	err := ActivateCaseTypeVersion(context.Background(), db, "ct-draft-1", "architect")
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestActivationGate_ActivateCaseTypeVersion_MultipleValidationViolations(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	cfg := invalidCaseTypeConfigWithMultipleViolations()
	cfgRaw := mustJSONRaw(t, cfg)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM case_types\s+WHERE id = \$1::uuid\s+FOR UPDATE`).
		WithArgs("ct-draft-bad").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "code", "version", "name", "description", "config", "status", "created_at", "updated_at", "activated_at", "activated_by", "deprecated_at", "deprecated_by",
		}).AddRow("ct-draft-bad", "HOME_LOAN", 3, "Bad", "bad", cfgRaw, "DRAFT", now, now, nil, nil, nil, nil))

	mock.ExpectQuery(`(?s)SELECT id::text\s+FROM case_types\s+WHERE code = \$1\s+FOR UPDATE`).
		WithArgs("HOME_LOAN").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ct-draft-bad"))

	mock.ExpectQuery(`(?s)INSERT INTO case_type_audit_log`).
		WithArgs("ct-draft-bad", "VALIDATION_FAILED", "architect", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"audit_id"}).AddRow("audit-bad"))
	mock.ExpectRollback()

	err := ActivateCaseTypeVersion(context.Background(), db, "ct-draft-bad", "architect")
	assert.Error(t, err)
	var validationErr *ValidationResult
	assert.ErrorAs(t, err, &validationErr)
	if validationErr != nil {
		assert.GreaterOrEqual(t, len(validationErr.Violations), 2)
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDiffAndChangelog_DiffCaseTypeVersions_NoFieldsChanged(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	cfg := validCaseTypeConfig()
	cfgRaw := mustJSONRaw(t, cfg)
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM case_type_version_diffs\s+WHERE from_case_type_id = \$1::uuid\s+AND to_case_type_id = \$2::uuid`).
		WithArgs("ct-v1", "ct-v2").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectQuery(`(?s)FROM case_types\s+WHERE id = \$1::uuid`).
		WithArgs("ct-v1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "version", "name", "description", "status", "config", "created_at", "updated_at", "activated_at", "activated_by", "deprecated_at", "deprecated_by"}).
			AddRow("ct-v1", "HOME_LOAN", 1, "Home Loan", "desc", "DEPRECATED", cfgRaw, now, now, nil, nil, nil, nil))

	mock.ExpectQuery(`(?s)FROM case_types\s+WHERE id = \$1::uuid`).
		WithArgs("ct-v2").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "version", "name", "description", "status", "config", "created_at", "updated_at", "activated_at", "activated_by", "deprecated_at", "deprecated_by"}).
			AddRow("ct-v2", "HOME_LOAN", 2, "Home Loan", "desc", "ACTIVE", cfgRaw, now, now, nil, nil, nil, nil))

	mock.ExpectQuery(`(?s)INSERT INTO case_type_version_diffs`).
		WithArgs("HOME_LOAN", "ct-v1", "ct-v2", 1, 2, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"diff_id", "computed_at"}).AddRow("diff-1", now))

	mock.ExpectCommit()

	diff, err := DiffCaseTypeVersions(context.Background(), db, "ct-v1", "ct-v2")
	assert.NoError(t, err)
	assert.True(t, diff.Empty())
	assert.Equal(t, "diff-1", diff.DiffID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInFlightCaseMigration_MigrateCaseToLatestVersion_CurrentStageMissingInTarget(t *testing.T) {
	db, mock := newSQLXMock(t)
	defer db.Close()

	targetCfgRaw := mustJSONRaw(t, map[string]interface{}{
		"stages": []map[string]interface{}{
			{
				"code":           "INITIAL_REVIEW",
				"name":           "Initial",
				"sequence_order": 1,
				"activities": []map[string]interface{}{
					{"code": "DOC_CHECK", "name": "Doc", "sequence_order": 1, "task_definitions": []interface{}{}},
				},
			},
		},
	})
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM cases c\s+JOIN case_types ct`).
		WithArgs("case-1").
		WillReturnRows(sqlmock.NewRows([]string{"case_id", "status", "current_stage_code", "current_stage_ordinal", "pinned_case_type_id", "pinned_case_type_code", "pinned_case_type_status", "pinned_version"}).
			AddRow("case-1", "IN_PROGRESS", "UNDERWRITING", 3, "ct-old", "HOME_LOAN", "DEPRECATED", 1))

	mock.ExpectQuery(`(?s)SELECT id::text\s+FROM case_types\s+WHERE code = \$1\s+FOR UPDATE`).
		WithArgs("HOME_LOAN").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("ct-old").AddRow("ct-new"))

	mock.ExpectQuery(`(?s)FROM case_types\s+WHERE code = \$1\s+AND status = 'ACTIVE'\s+AND id <> \$2::uuid`).
		WithArgs("HOME_LOAN", "ct-old").
		WillReturnRows(sqlmock.NewRows([]string{"id", "code", "version", "name", "description", "config", "status", "created_at", "updated_at", "activated_at", "activated_by", "deprecated_at", "deprecated_by"}).
			AddRow("ct-new", "HOME_LOAN", 2, "Home Loan v2", "desc", targetCfgRaw, "ACTIVE", now, now, now, "architect", nil, nil))

	mock.ExpectRollback()

	err := MigrateCaseToLatestVersion(context.Background(), db, "case-1", "architect")
	assert.Error(t, err)
	var stageErr *StageCompatibilityError
	assert.ErrorAs(t, err, &stageErr)
	assert.NoError(t, mock.ExpectationsWereMet())
}
