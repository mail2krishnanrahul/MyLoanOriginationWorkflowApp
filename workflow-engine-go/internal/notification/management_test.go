package notification

import (
	"context"
	"errors"
	"testing"

	"workflow-engine/pkg/model"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

func TestCreateNotificationTemplate(t *testing.T) {
	subject := "Case {{.reference_number}} update"

	tests := []struct {
		name    string
		input   model.NotificationTemplate
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path",
			input: model.NotificationTemplate{
				TemplateCode:    "CASE_CREATED",
				Channel:         model.NotificationChannelEmail,
				SubjectTemplate: &subject,
				BodyTemplate:    "Hello {{.borrower_name}}",
				LanguageCode:    "en",
				Status:          model.NotificationTemplateStatusActive,
				Version:         1,
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)INSERT INTO notification_templates`).
					WithArgs(
						"CASE_CREATED",
						nil,
						"EMAIL",
						"Case {{.reference_number}} update",
						"Hello {{.borrower_name}}",
						"en",
						"ACTIVE",
						1,
						sqlmock.AnyArg(),
					).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("tpl-1"))
			},
		},
		{
			name: "edge invalid template syntax",
			input: model.NotificationTemplate{
				TemplateCode: "CASE_CREATED",
				Channel:      model.NotificationChannelEmail,
				BodyTemplate: "{{if .borrower_name}}",
				LanguageCode: "en",
			},
			wantErr: true,
		},
		{
			name: "failure mode insert error",
			input: model.NotificationTemplate{
				TemplateCode: "CASE_CREATED",
				Channel:      model.NotificationChannelEmail,
				BodyTemplate: "Hello {{.borrower_name}}",
				LanguageCode: "en",
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)INSERT INTO notification_templates`).
					WillReturnError(errors.New("insert failed"))
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

			if tt.setup != nil {
				tt.setup(mock)
			}

			_, err = CreateNotificationTemplate(context.Background(), db, NewTemplateRenderer(), tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpsertNotificationTrigger(t *testing.T) {
	filter := "amount > 500000"
	recipient := "borrower_email"

	tests := []struct {
		name    string
		input   model.NotificationTrigger
		setup   func(sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			name: "happy path",
			input: model.NotificationTrigger{
				TriggerCode:         "TRG_CASE_CREATED_EMAIL",
				EventType:           model.EventCaseCreated,
				FilterExpression:    &filter,
				TemplateCode:        "CASE_CREATED",
				RecipientType:       model.NotificationRecipientBorrower,
				RecipientValue:      &recipient,
				SendAfterMinutes:    0,
				DedupeWindowMinutes: 30,
				Priority:            model.NotificationPriorityNormal,
				IsEnabled:           true,
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)INSERT INTO notification_triggers`).
					WithArgs(
						"TRG_CASE_CREATED_EMAIL",
						nil,
						"CASE_CREATED",
						"amount > 500000",
						"CASE_CREATED",
						"BORROWER",
						"borrower_email",
						0,
						30,
						"NORMAL",
						true,
					).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("trg-1"))
			},
		},
		{
			name: "edge defaults priority",
			input: model.NotificationTrigger{
				TriggerCode:   "TRG_TASK_ASSIGNED_INAPP",
				EventType:     model.EventTaskAssigned,
				TemplateCode:  "TASK_ASSIGNED",
				RecipientType: model.NotificationRecipientTaskAssignee,
				IsEnabled:     true,
			},
			setup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery(`(?s)INSERT INTO notification_triggers`).
					WithArgs(
						"TRG_TASK_ASSIGNED_INAPP",
						nil,
						"TASK_ASSIGNED",
						nil,
						"TASK_ASSIGNED",
						"TASK_ASSIGNEE",
						nil,
						0,
						0,
						"NORMAL",
						true,
					).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("trg-2"))
			},
		},
		{
			name: "failure mode negative dedupe",
			input: model.NotificationTrigger{
				TriggerCode:         "TRG_BAD",
				EventType:           model.EventCaseCreated,
				TemplateCode:        "CASE_CREATED",
				RecipientType:       model.NotificationRecipientBorrower,
				DedupeWindowMinutes: -1,
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

			if tt.setup != nil {
				tt.setup(mock)
			}

			_, err = UpsertNotificationTrigger(context.Background(), db, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
