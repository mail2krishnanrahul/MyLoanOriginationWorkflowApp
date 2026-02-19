package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// CreateNotificationTemplate validates and persists a reusable notification template.
func CreateNotificationTemplate(
	ctx context.Context,
	db *sqlx.DB,
	renderer *TemplateRenderer,
	template model.NotificationTemplate,
) (string, error) {
	if db == nil {
		return "", fmt.Errorf("CreateNotificationTemplate: db is nil")
	}

	template.TemplateCode = strings.TrimSpace(template.TemplateCode)
	if template.TemplateCode == "" {
		return "", fmt.Errorf("CreateNotificationTemplate: template_code is required")
	}
	template.BodyTemplate = strings.TrimSpace(template.BodyTemplate)
	if template.BodyTemplate == "" {
		return "", fmt.Errorf("CreateNotificationTemplate: body_template is required")
	}
	template.LanguageCode = strings.TrimSpace(template.LanguageCode)
	if template.LanguageCode == "" {
		return "", fmt.Errorf("CreateNotificationTemplate: language_code is required")
	}
	if template.Channel == "" {
		return "", fmt.Errorf("CreateNotificationTemplate: channel is required")
	}
	if !isValidNotificationChannel(template.Channel) {
		return "", fmt.Errorf("CreateNotificationTemplate: unsupported channel %s", template.Channel)
	}
	if template.Status == "" {
		template.Status = model.NotificationTemplateStatusDraft
	}
	if template.Version <= 0 {
		template.Version = 1
	}

	r := renderer
	if r == nil {
		r = NewTemplateRenderer()
	}
	if template.SubjectTemplate != nil && strings.TrimSpace(*template.SubjectTemplate) != "" {
		if err := r.ValidateTemplate(*template.SubjectTemplate); err != nil {
			return "", fmt.Errorf("CreateNotificationTemplate: validate subject_template: %w", err)
		}
	}
	if err := r.ValidateTemplate(template.BodyTemplate); err != nil {
		return "", fmt.Errorf("CreateNotificationTemplate: validate body_template: %w", err)
	}

	metadata := template.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}

	var templateID string
	err := db.GetContext(ctx, &templateID, `
		INSERT INTO notification_templates (
			template_code,
			case_type_code,
			channel,
			subject_template,
			body_template,
			language_code,
			status,
			version,
			metadata,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9::jsonb,
			now(),
			now()
		)
		RETURNING id::text
	`,
		template.TemplateCode,
		template.CaseTypeCode,
		string(template.Channel),
		template.SubjectTemplate,
		template.BodyTemplate,
		template.LanguageCode,
		string(template.Status),
		template.Version,
		metadata,
	)
	if err != nil {
		return "", fmt.Errorf("CreateNotificationTemplate: insert: %w", err)
	}
	return templateID, nil
}

// UpsertNotificationTrigger creates or updates a notification trigger configuration.
func UpsertNotificationTrigger(
	ctx context.Context,
	db *sqlx.DB,
	trigger model.NotificationTrigger,
) (string, error) {
	if db == nil {
		return "", fmt.Errorf("UpsertNotificationTrigger: db is nil")
	}

	trigger.TriggerCode = strings.TrimSpace(trigger.TriggerCode)
	if trigger.TriggerCode == "" {
		return "", fmt.Errorf("UpsertNotificationTrigger: trigger_code is required")
	}
	if trigger.EventType == "" {
		return "", fmt.Errorf("UpsertNotificationTrigger: event_type is required")
	}
	trigger.TemplateCode = strings.TrimSpace(trigger.TemplateCode)
	if trigger.TemplateCode == "" {
		return "", fmt.Errorf("UpsertNotificationTrigger: template_code is required")
	}
	if trigger.RecipientType == "" {
		return "", fmt.Errorf("UpsertNotificationTrigger: recipient_type is required")
	}
	if !isValidRecipientType(trigger.RecipientType) {
		return "", fmt.Errorf("UpsertNotificationTrigger: unsupported recipient_type %s", trigger.RecipientType)
	}
	if trigger.Priority == "" {
		trigger.Priority = model.NotificationPriorityNormal
	}
	if !isValidNotificationPriority(trigger.Priority) {
		return "", fmt.Errorf("UpsertNotificationTrigger: unsupported priority %s", trigger.Priority)
	}
	if trigger.SendAfterMinutes < 0 {
		return "", fmt.Errorf("UpsertNotificationTrigger: send_after_minutes cannot be negative")
	}
	if trigger.DedupeWindowMinutes < 0 {
		return "", fmt.Errorf("UpsertNotificationTrigger: dedupe_window_minutes cannot be negative")
	}

	var triggerID string
	err := db.GetContext(ctx, &triggerID, `
		INSERT INTO notification_triggers (
			trigger_code,
			case_type_code,
			event_type,
			filter_expression,
			template_code,
			recipient_type,
			recipient_value,
			send_after_minutes,
			dedupe_window_minutes,
			priority,
			is_enabled,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			now(),
			now()
		)
		ON CONFLICT (trigger_code)
		DO UPDATE SET
			case_type_code = EXCLUDED.case_type_code,
			event_type = EXCLUDED.event_type,
			filter_expression = EXCLUDED.filter_expression,
			template_code = EXCLUDED.template_code,
			recipient_type = EXCLUDED.recipient_type,
			recipient_value = EXCLUDED.recipient_value,
			send_after_minutes = EXCLUDED.send_after_minutes,
			dedupe_window_minutes = EXCLUDED.dedupe_window_minutes,
			priority = EXCLUDED.priority,
			is_enabled = EXCLUDED.is_enabled,
			updated_at = now()
		RETURNING id::text
	`,
		trigger.TriggerCode,
		trigger.CaseTypeCode,
		string(trigger.EventType),
		trigger.FilterExpression,
		trigger.TemplateCode,
		string(trigger.RecipientType),
		trigger.RecipientValue,
		trigger.SendAfterMinutes,
		trigger.DedupeWindowMinutes,
		string(trigger.Priority),
		trigger.IsEnabled,
	)
	if err != nil {
		return "", fmt.Errorf("UpsertNotificationTrigger: upsert: %w", err)
	}
	return triggerID, nil
}

func isValidNotificationChannel(channel model.NotificationChannel) bool {
	switch channel {
	case model.NotificationChannelEmail,
		model.NotificationChannelSMS,
		model.NotificationChannelPush,
		model.NotificationChannelInApp,
		model.NotificationChannelWebhook:
		return true
	default:
		return false
	}
}

func isValidRecipientType(value model.NotificationRecipientType) bool {
	switch value {
	case model.NotificationRecipientCaseOwner,
		model.NotificationRecipientTaskAssignee,
		model.NotificationRecipientApprover,
		model.NotificationRecipientSupervisor,
		model.NotificationRecipientBorrower,
		model.NotificationRecipientFixedAddress,
		model.NotificationRecipientDynamicRule:
		return true
	default:
		return false
	}
}

func isValidNotificationPriority(priority model.NotificationPriority) bool {
	switch priority {
	case model.NotificationPriorityLow,
		model.NotificationPriorityNormal,
		model.NotificationPriorityHigh,
		model.NotificationPriorityUrgent:
		return true
	default:
		return false
	}
}
