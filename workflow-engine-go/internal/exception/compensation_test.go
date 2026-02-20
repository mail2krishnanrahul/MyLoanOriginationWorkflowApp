package exception

import (
	"context"
	"regexp"
	"testing"
	"time"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestSagaCompensation_HappyPath(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)

	cfg := &model.CaseTypeConfig{
		Stages: []model.StageDefinitionV2{
			{
				Code: "UNDERWRITING",
				Activities: []model.ActivityConfig{
					{
						Code: "UW_ACTIVITY",
						TaskDefs: []model.TaskDefinitionV2{
							{Code: "ROLLBACK_LOAN", RetryPolicy: &model.TaskRetryPolicy{MaxRetries: 1, BackoffStrategy: model.RetryBackoffStrategyFixed, BaseIntervalSeconds: 5, MaxIntervalSeconds: 10}},
						},
					},
				},
			},
		},
	}

	failureCtx := failureContextRow{
		TaskID:             "task-1",
		CaseID:             "case-1",
		TaskDefinitionCode: "BOOK_LOAN",
		ActivityCode:       "BOOKING",
		StageCode:          "DISBURSEMENT",
	}
	failedTask := taskLocation{
		TaskDef:      model.TaskDefinitionV2{Code: "BOOK_LOAN", CompensatingTaskCode: "ROLLBACK_LOAN"},
		ActivityCode: "BOOKING",
		StageCode:    "DISBURSEMENT",
	}

	mock.ExpectQuery(regexp.QuoteMeta("WITH inserted AS (")).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("comp-task-1"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO task_compensations")).WithArgs("case-1", "task-1", "BOOK_LOAN", "ROLLBACK_LOAN", "comp-task-1", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).
		WithArgs(sqlmock.AnyArg(), "case-1", "comp-task-1", "COMPENSATION_STARTED", sqlmock.AnyArg(), "PENDING", "case-orchestrator", 5, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs(sqlmock.AnyArg(), "COMPENSATION_STARTED", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = startCompensation(context.Background(), tx, failureCtx, cfg, failedTask, model.TaskErrorDetail{Message: "booking failed", OccurredAt: time.Now().UTC()})
	assert.NoError(t, err)

	mock.ExpectCommit()
	assert.NoError(t, tx.Commit())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSagaCompensation_EdgeCase(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)

	failureCtx := failureContextRow{
		TaskID:             "task-edge",
		CaseID:             "case-edge",
		TaskDefinitionCode: "FORWARD_TASK",
		ActivityCode:       "FORWARD_ACTIVITY",
		StageCode:          "FORWARD_STAGE",
	}
	failedTask := taskLocation{
		TaskDef:      model.TaskDefinitionV2{Code: "FORWARD_TASK", CompensatingTaskCode: "ROLLBACK_UNKNOWN"},
		ActivityCode: "FORWARD_ACTIVITY",
		StageCode:    "FORWARD_STAGE",
	}

	mock.ExpectQuery(regexp.QuoteMeta("WITH inserted AS (")).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("comp-task-edge"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO task_compensations")).WithArgs("case-edge", "task-edge", "FORWARD_TASK", "ROLLBACK_UNKNOWN", "comp-task-edge", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO events_outbox`).
		WithArgs(sqlmock.AnyArg(), "case-edge", "comp-task-edge", "COMPENSATION_STARTED", sqlmock.AnyArg(), "PENDING", "case-orchestrator", 5, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`(?s)INSERT INTO webhook_delivery_queue`).
		WithArgs(sqlmock.AnyArg(), "COMPENSATION_STARTED", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = startCompensation(context.Background(), tx, failureCtx, &model.CaseTypeConfig{}, failedTask, model.TaskErrorDetail{Message: "failed"})
	assert.NoError(t, err)

	mock.ExpectCommit()
	assert.NoError(t, tx.Commit())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSagaCompensation_FailureMode(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	assert.NoError(t, err)
	db := sqlx.NewDb(sqlDB, "sqlmock")
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.BeginTxx(context.Background(), nil)
	assert.NoError(t, err)

	failureCtx := failureContextRow{
		TaskID:             "task-fail",
		CaseID:             "case-fail",
		TaskDefinitionCode: "FORWARD_TASK",
		ActivityCode:       "FORWARD_ACTIVITY",
		StageCode:          "FORWARD_STAGE",
	}
	failedTask := taskLocation{
		TaskDef:      model.TaskDefinitionV2{Code: "FORWARD_TASK", CompensatingTaskCode: "ROLLBACK_FAIL"},
		ActivityCode: "FORWARD_ACTIVITY",
		StageCode:    "FORWARD_STAGE",
	}

	mock.ExpectQuery(regexp.QuoteMeta("WITH inserted AS (")).WillReturnError(assert.AnError)

	err = startCompensation(context.Background(), tx, failureCtx, &model.CaseTypeConfig{}, failedTask, model.TaskErrorDetail{Message: "failed"})
	assert.Error(t, err)

	mock.ExpectRollback()
	_ = tx.Rollback()
	assert.NoError(t, mock.ExpectationsWereMet())
}
