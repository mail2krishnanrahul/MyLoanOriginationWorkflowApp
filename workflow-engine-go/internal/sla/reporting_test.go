package sla

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestGetSLAComplianceReport(t *testing.T) {
	start := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		filters SLAReportFilters
		setup   func(sqlmock.Sqlmock)
		want    []SLAComplianceReport
		wantErr bool
	}{
		{
			name: "happy path",
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"case_type_code", "stage_code", "activity_code", "task_definition_code",
					"total_count", "completed_count", "breached_count", "compliance_rate",
					"avg_elapsed_minutes", "p95_elapsed_minutes",
				}).AddRow("HOME_LOAN", "UNDERWRITING", "DOC_CHECK", "VERIFY_INCOME", 100, 95, 5, 0.947368421, 34.5, 89)

				mock.ExpectQuery(regexp.QuoteMeta("FROM sla_metrics_summary")).
					WithArgs("2025-02-01", "2025-02-28").
					WillReturnRows(rows)
			},
			want: []SLAComplianceReport{{
				CaseTypeCode:       "HOME_LOAN",
				StageCode:          "UNDERWRITING",
				ActivityCode:       "DOC_CHECK",
				TaskDefinitionCode: "VERIFY_INCOME",
				TotalCount:         100,
				CompletedCount:     95,
				BreachedCount:      5,
				ComplianceRate:     0.947368421,
				AvgElapsedMinutes:  34.5,
				P95ElapsedMinutes:  89,
			}},
		},
		{
			name: "edge case with filters",
			filters: SLAReportFilters{
				CaseTypeCode: strPtr("HOME_LOAN"),
				StageCode:    strPtr("UNDERWRITING"),
			},
			setup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"case_type_code", "stage_code", "activity_code", "task_definition_code",
					"total_count", "completed_count", "breached_count", "compliance_rate",
					"avg_elapsed_minutes", "p95_elapsed_minutes",
				}).AddRow("HOME_LOAN", "UNDERWRITING", "DOC_CHECK", "VERIFY_INCOME", 10, 10, 0, 1.0, 12.0, 20)

				mock.ExpectQuery(regexp.QuoteMeta("FROM sla_metrics_summary")).
					WithArgs("2025-02-01", "2025-02-28", "HOME_LOAN", "UNDERWRITING").
					WillReturnRows(rows)
			},
			want: []SLAComplianceReport{{
				CaseTypeCode:       "HOME_LOAN",
				StageCode:          "UNDERWRITING",
				ActivityCode:       "DOC_CHECK",
				TaskDefinitionCode: "VERIFY_INCOME",
				TotalCount:         10,
				CompletedCount:     10,
				BreachedCount:      0,
				ComplianceRate:     1.0,
				AvgElapsedMinutes:  12.0,
				P95ElapsedMinutes:  20,
			}},
		},
		{
			name: "failure mode db error",
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(regexp.QuoteMeta("FROM sla_metrics_summary")).
					WithArgs("2025-02-01", "2025-02-28").
					WillReturnError(assert.AnError)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			assert.NoError(t, err)
			db := sqlx.NewDb(sqlDB, "sqlmock")
			defer db.Close()

			tt.setup(mock)

			got, err := GetSLAComplianceReport(context.Background(), db, start, end, tt.filters)
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

func strPtr(v string) *string {
	return &v
}
