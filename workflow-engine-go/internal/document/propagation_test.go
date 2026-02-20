package document

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestResolveTaskInputs(t *testing.T) {
	tests := []struct {
		name    string
		taskDef model.TaskDefinitionV2
		setup   func(sqlmock.Sqlmock)
		want    map[string]interface{}
		wantErr bool
	}{
		{
			name: "all dependencies satisfied",
			taskDef: model.TaskDefinitionV2{
				Code: "UNDERWRITING_DECISION",
				Inputs: []model.TaskInputDefinition{
					{
						Field:       "credit_score",
						SourceTask:  "CREDIT_CHECK",
						SourceField: "credit_score",
						Required:    true,
					},
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				payload, _ := json.Marshal(map[string]interface{}{"credit_score": 750.0})
				rows := sqlmock.NewRows([]string{"task_definition_code", "output_payload"}).
					AddRow("CREDIT_CHECK", payload)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT task_definition_code, output_payload")).
					WillReturnRows(rows)
			},
			want: map[string]interface{}{"credit_score": 750.0},
		},
		{
			name: "required dependency field missing",
			taskDef: model.TaskDefinitionV2{
				Code: "UNDERWRITING_DECISION",
				Inputs: []model.TaskInputDefinition{
					{
						Field:       "credit_score",
						SourceTask:  "CREDIT_CHECK",
						SourceField: "credit_score",
						Required:    true,
					},
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				payload, _ := json.Marshal(map[string]interface{}{"credit_report_url": "https://example.com"})
				rows := sqlmock.NewRows([]string{"task_definition_code", "output_payload"}).
					AddRow("CREDIT_CHECK", payload)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT task_definition_code, output_payload")).
					WillReturnRows(rows)
			},
			wantErr: true,
		},
		{
			name: "optional dependency missing succeeds",
			taskDef: model.TaskDefinitionV2{
				Code: "UNDERWRITING_DECISION",
				Inputs: []model.TaskInputDefinition{
					{
						Field:       "credit_report_url",
						SourceTask:  "CREDIT_CHECK",
						SourceField: "credit_report_url",
						Required:    false,
					},
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				payload, _ := json.Marshal(map[string]interface{}{"credit_score": 700.0})
				rows := sqlmock.NewRows([]string{"task_definition_code", "output_payload"}).
					AddRow("CREDIT_CHECK", payload)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT task_definition_code, output_payload")).
					WillReturnRows(rows)
			},
			want: map[string]interface{}{},
		},
		{
			name: "source task not completed blocks",
			taskDef: model.TaskDefinitionV2{
				Code: "UNDERWRITING_DECISION",
				Inputs: []model.TaskInputDefinition{
					{
						Field:       "credit_score",
						SourceTask:  "CREDIT_CHECK",
						SourceField: "credit_score",
						Required:    true,
					},
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"task_definition_code", "output_payload"})
				mock.ExpectQuery(regexp.QuoteMeta("SELECT task_definition_code, output_payload")).
					WillReturnRows(rows)
			},
			wantErr: true,
		},
		{
			name: "latest completed source task used",
			taskDef: model.TaskDefinitionV2{
				Code: "UNDERWRITING_DECISION",
				Inputs: []model.TaskInputDefinition{
					{
						Field:       "credit_score",
						SourceTask:  "CREDIT_CHECK",
						SourceField: "credit_score",
						Required:    true,
					},
				},
			},
			setup: func(mock sqlmock.Sqlmock) {
				latestPayload, _ := json.Marshal(map[string]interface{}{"credit_score": 805.0})
				rows := sqlmock.NewRows([]string{"task_definition_code", "output_payload"}).
					AddRow("CREDIT_CHECK", latestPayload)
				mock.ExpectQuery(regexp.QuoteMeta("SELECT task_definition_code, output_payload")).
					WillReturnRows(rows)
			},
			want: map[string]interface{}{"credit_score": 805.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			tt.setup(mock)

			got, err := ResolveTaskInputs(context.Background(), db, "case-1", tt.taskDef)
			if tt.wantErr {
				assert.Error(t, err)
				var depErr *DependencyError
				assert.True(t, errors.As(err, &depErr))
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
