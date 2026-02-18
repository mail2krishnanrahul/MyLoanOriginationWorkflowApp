package approval

import (
	"context"
	"encoding/json"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestSelectApprovers(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		gate    model.ApprovalGate
		caseData model.CaseInstance
		want    []string
		wantErr bool
	}{
		{
			name: "happy path explicit list",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id"}).AddRow("u-1").AddRow("u-2")
				mock.ExpectQuery(`(?s)FROM users`).WillReturnRows(rows)
			},
			gate: model.ApprovalGate{
				ApproverSelection: model.ApproverSelectionExplicitList,
				Approvers:         mustJSON(t, []string{"u-1", "u-2"}),
			},
			caseData: model.CaseInstance{},
			want:     []string{"u-1", "u-2"},
		},
		{
			name: "edge case dynamic rule selects director",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id"}).AddRow("director-1")
				mock.ExpectQuery(`(?s)FROM users`).WillReturnRows(rows)
			},
			gate: model.ApprovalGate{
				ApproverSelection:     model.ApproverSelectionDynamicRule,
				DynamicRuleExpression: strPtr("if amount > 500000 then DIRECTOR else MANAGER"),
			},
			caseData: model.CaseInstance{Metadata: mustJSON(t, map[string]interface{}{"amount": 750000})},
			want:     []string{"director-1"},
		},
		{
			name: "failure mode no eligible approvers",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"id"})
				mock.ExpectQuery(`(?s)FROM users`).WillReturnRows(rows)
			},
			gate: model.ApprovalGate{
				ApproverSelection: model.ApproverSelectionRoleBased,
				Approvers:         mustJSON(t, []string{"MANAGER"}),
			},
			caseData: model.CaseInstance{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			tt.setup(mock)
			got, err := SelectApprovers(context.Background(), db, tt.gate, tt.caseData)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	assert.NoError(t, err)
	return raw
}
