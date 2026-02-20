package document

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestApplyAggregationRules(t *testing.T) {
	tests := []struct {
		name    string
		task    model.Task
		rules   []model.AggregationRule
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path updates metadata",
			task: model.Task{
				TaskDefinitionCode: "CREDIT_CHECK",
				OutputPayload:      mustJSON(t, map[string]interface{}{"credit_score": 750.0}),
			},
			rules: []model.AggregationRule{
				{
					TargetField:    "metadata.credit_score",
					SourceTask:     "CREDIT_CHECK",
					SourceField:    "output_payload.credit_score",
					OnTaskComplete: true,
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(regexp.QuoteMeta("UPDATE cases")).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name: "edge nested path not existing yet",
			task: model.Task{
				TaskDefinitionCode: "LOAN_APPLICATION",
				InputPayload:       mustJSON(t, map[string]interface{}{"requested_amount": 250000.0}),
			},
			rules: []model.AggregationRule{
				{
					TargetField:    "metadata.loan.details.amount",
					SourceTask:     "LOAN_APPLICATION",
					SourceField:    "input_payload.requested_amount",
					OnTaskComplete: true,
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec(regexp.QuoteMeta("UPDATE cases")).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			name: "failure source field missing",
			task: model.Task{
				TaskDefinitionCode: "UNDERWRITING_DECISION",
				OutputPayload:      mustJSON(t, map[string]interface{}{"decision": "APPROVED"}),
			},
			rules: []model.AggregationRule{
				{
					TargetField:    "metadata.credit_score",
					SourceTask:     "UNDERWRITING_DECISION",
					SourceField:    "output_payload.credit_score",
					OnTaskComplete: true,
				},
			},
			setup:   func(mock sqlmock.Sqlmock) {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			mock.ExpectBegin()
			tx, err := db.BeginTxx(context.Background(), nil)
			assert.NoError(t, err)

			tt.setup(mock)

			err = ApplyAggregationRules(context.Background(), tx, "case-1", tt.task, tt.rules)
			if tt.wantErr {
				assert.Error(t, err)
				mock.ExpectRollback()
				_ = tx.Rollback()
			} else {
				assert.NoError(t, err)
				mock.ExpectCommit()
				assert.NoError(t, tx.Commit())
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func mustJSON(t *testing.T, value map[string]interface{}) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	assert.NoError(t, err)
	return raw
}
