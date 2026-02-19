package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"workflow-engine/internal/approval"
	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type NotificationService struct {
	db        *sqlx.DB
	renderer  *TemplateRenderer
	evaluator *approval.ExpressionEvaluator
	publisher EventPublisher
	logger    *slog.Logger
	nowFunc   func() time.Time
}

func NewNotificationService(
	db *sqlx.DB,
	renderer *TemplateRenderer,
	evaluator *approval.ExpressionEvaluator,
	publisher EventPublisher,
	logger *slog.Logger,
) *NotificationService {
	if renderer == nil {
		renderer = NewTemplateRenderer()
	}
	if evaluator == nil {
		evaluator = &approval.ExpressionEvaluator{}
	}
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationService{
		db:        db,
		renderer:  renderer,
		evaluator: evaluator,
		publisher: publisher,
		logger:    logger,
		nowFunc:   func() time.Time { return time.Now().UTC() },
	}
}

type triggerTemplateRow struct {
	TriggerCode         string          `db:"trigger_code"`
	EventType           string          `db:"event_type"`
	FilterExpression    sql.NullString  `db:"filter_expression"`
	TemplateCode        string          `db:"template_code"`
	RecipientType       string          `db:"recipient_type"`
	RecipientValue      sql.NullString  `db:"recipient_value"`
	SendAfterMinutes    int             `db:"send_after_minutes"`
	DedupeWindowMinutes int             `db:"dedupe_window_minutes"`
	Priority            string          `db:"priority"`
	Channel             string          `db:"channel"`
	SubjectTemplate     sql.NullString  `db:"subject_template"`
	BodyTemplate        string          `db:"body_template"`
	TemplateMetadata    json.RawMessage `db:"template_metadata"`
}

type caseSnapshot struct {
	ID                  string          `db:"id"`
	ReferenceNumber     string          `db:"reference_number"`
	Metadata            json.RawMessage `db:"metadata"`
	AssignedTo          sql.NullString  `db:"assigned_to"`
	SupervisorID        sql.NullString  `db:"supervisor_id"`
	CurrentStageCode    sql.NullString  `db:"current_stage_code"`
	CurrentStageOrdinal int             `db:"current_stage_ordinal"`
	CaseTypeCode        string          `db:"case_type_code"`
}

type taskSnapshot struct {
	ID                 string          `db:"id"`
	AssigneeID         sql.NullString  `db:"assignee_id"`
	AssignedService    sql.NullString  `db:"assigned_service"`
	InputPayload       json.RawMessage `db:"input_payload"`
	OutputPayload      json.RawMessage `db:"output_payload"`
	Metadata           json.RawMessage `db:"metadata"`
	ActivityCode       string          `db:"activity_code"`
	StageCode          string          `db:"stage_code"`
	TaskDefinitionCode string          `db:"task_definition_code"`
}

// HandleEvent evaluates active triggers for an event and queues notifications.
func (s *NotificationService) HandleEvent(ctx context.Context, event model.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("HandleEvent: db is nil")
	}

	tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("HandleEvent: begin tx: %w", err)
	}
	defer tx.Rollback()

	triggers, err := s.loadMatchingTriggers(ctx, tx, event)
	if err != nil {
		return fmt.Errorf("HandleEvent: load triggers: %w", err)
	}
	if len(triggers) == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("HandleEvent: commit empty tx: %w", err)
		}
		return nil
	}

	payloadMap, err := unmarshalToMap(event.Payload)
	if err != nil {
		return fmt.Errorf("HandleEvent: parse payload: %w", err)
	}
	caseSnap, err := s.loadCaseSnapshot(ctx, tx, event.CaseID)
	if err != nil {
		return fmt.Errorf("HandleEvent: load case snapshot: %w", err)
	}
	taskSnap, err := s.loadTaskSnapshot(ctx, tx, event.TaskID)
	if err != nil {
		return fmt.Errorf("HandleEvent: load task snapshot: %w", err)
	}
	contextData, caseMetadata, err := buildTemplateContext(event, payloadMap, caseSnap, taskSnap)
	if err != nil {
		return fmt.Errorf("HandleEvent: build context: %w", err)
	}

	for _, trigger := range triggers {
		if trigger.FilterExpression.Valid && strings.TrimSpace(trigger.FilterExpression.String) != "" {
			matches, err := s.evaluator.Evaluate(ctx, trigger.FilterExpression.String, contextData)
			if err != nil {
				return fmt.Errorf("HandleEvent: evaluate filter for trigger %s: %w", trigger.TriggerCode, err)
			}
			if !matches {
				continue
			}
		}

		recipient, err := s.resolveRecipient(ctx, trigger, caseSnap, taskSnap, payloadMap, caseMetadata, contextData)
		if err != nil {
			return fmt.Errorf("HandleEvent: resolve recipient for trigger %s: %w", trigger.TriggerCode, err)
		}

		caseIDValue := stringFromPtr(event.CaseID)
		isDuplicate, err := checkDuplicateNotificationTx(ctx, tx, recipient, trigger.TriggerCode, caseIDValue, trigger.DedupeWindowMinutes)
		if err != nil {
			return fmt.Errorf("HandleEvent: dedupe check for trigger %s: %w", trigger.TriggerCode, err)
		}
		if isDuplicate {
			reason := model.NotificationSuppressionDuplicate
			if err := logSuppression(ctx, tx, nil, trigger.TriggerCode, recipient, event.CaseID, reason); err != nil {
				return fmt.Errorf("HandleEvent: log duplicate suppression: %w", err)
			}
			if err := publishNotificationInternalEvent(ctx, tx, s.publisher, model.EventNotificationSuppressed, NotificationEventPayload{
				TriggerCode: trigger.TriggerCode,
				CaseID:      event.CaseID,
				TaskID:      event.TaskID,
				Channel:     model.NotificationChannel(trigger.Channel),
				Recipient:   recipient,
				Status:      model.NotificationStatusSuppressed,
				Suppression: &reason,
				Reason:      "duplicate dedupe window",
			}); err != nil {
				return fmt.Errorf("HandleEvent: publish duplicate suppression event: %w", err)
			}
			continue
		}

		suppress, reason, err := CheckUserPreferences(ctx, s.db, recipient, trigger.Channel, trigger.EventType)
		if err != nil {
			return fmt.Errorf("HandleEvent: user preference check for trigger %s: %w", trigger.TriggerCode, err)
		}
		if suppress {
			suppReason := model.NotificationSuppressionReason(reason)
			if err := logSuppression(ctx, tx, nil, trigger.TriggerCode, recipient, event.CaseID, suppReason); err != nil {
				return fmt.Errorf("HandleEvent: log preference suppression: %w", err)
			}
			if err := publishNotificationInternalEvent(ctx, tx, s.publisher, model.EventNotificationSuppressed, NotificationEventPayload{
				TriggerCode: trigger.TriggerCode,
				CaseID:      event.CaseID,
				TaskID:      event.TaskID,
				Channel:     model.NotificationChannel(trigger.Channel),
				Recipient:   recipient,
				Status:      model.NotificationStatusSuppressed,
				Suppression: &suppReason,
				Reason:      reason,
			}); err != nil {
				return fmt.Errorf("HandleEvent: publish preference suppression event: %w", err)
			}
			continue
		}

		scheduledAt := s.now().Add(time.Duration(trigger.SendAfterMinutes) * time.Minute)
		if reason == string(model.NotificationSuppressionQuietHours) {
			nextAllowed, withinQuiet, qErr := s.nextQuietHoursScheduleTx(ctx, tx, recipient, trigger.Channel)
			if qErr != nil {
				return fmt.Errorf("HandleEvent: quiet-hours schedule for trigger %s: %w", trigger.TriggerCode, qErr)
			}
			if withinQuiet && nextAllowed.After(scheduledAt) {
				scheduledAt = nextAllowed
				suppReason := model.NotificationSuppressionQuietHours
				if err := logSuppression(ctx, tx, nil, trigger.TriggerCode, recipient, event.CaseID, suppReason); err != nil {
					return fmt.Errorf("HandleEvent: log quiet-hours suppression: %w", err)
				}
			}
		}

		subject, body, renderErr := s.renderQueuedContent(ctx, trigger, contextData)
		errorDetail := json.RawMessage(nil)
		if renderErr != nil {
			errorDetail = buildErrorDetail("TEMPLATE_RENDER_ERROR", renderErr)
		}

		notificationID, err := s.insertNotificationQueue(ctx, tx, trigger, event.CaseID, event.TaskID, recipient, subject, body, scheduledAt, errorDetail)
		if err != nil {
			return fmt.Errorf("HandleEvent: queue insert for trigger %s: %w", trigger.TriggerCode, err)
		}

		if err := appendDeliveryEvent(ctx, tx, notificationID, model.NotificationDeliveryEventQueued, json.RawMessage(`{"state":"queued"}`), nil); err != nil {
			return fmt.Errorf("HandleEvent: append queued delivery event: %w", err)
		}

		if err := publishNotificationInternalEvent(ctx, tx, s.publisher, model.EventNotificationQueued, NotificationEventPayload{
			NotificationID: notificationID,
			TriggerCode:    trigger.TriggerCode,
			CaseID:         event.CaseID,
			TaskID:         event.TaskID,
			Channel:        model.NotificationChannel(trigger.Channel),
			Recipient:      recipient,
			Status:         model.NotificationStatusPending,
			Reason:         "queued",
		}); err != nil {
			return fmt.Errorf("HandleEvent: publish queued event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("HandleEvent: commit: %w", err)
	}
	return nil
}

func (s *NotificationService) loadMatchingTriggers(ctx context.Context, tx *sqlx.Tx, event model.Event) ([]triggerTemplateRow, error) {
	caseIDArg := interface{}(nil)
	if event.CaseID != nil && strings.TrimSpace(*event.CaseID) != "" {
		caseIDArg = strings.TrimSpace(*event.CaseID)
	}

	var rows []triggerTemplateRow
	err := tx.SelectContext(ctx, &rows, `
		SELECT
			nt.trigger_code,
			nt.event_type,
			nt.filter_expression,
			nt.template_code,
			nt.recipient_type,
			nt.recipient_value,
			nt.send_after_minutes,
			nt.dedupe_window_minutes,
			nt.priority,
			tpl.channel,
			tpl.subject_template,
			tpl.body_template,
			tpl.metadata AS template_metadata
		FROM notification_triggers nt
		JOIN notification_templates tpl
		  ON tpl.template_code = nt.template_code
		WHERE nt.is_enabled = TRUE
		  AND tpl.status = 'ACTIVE'
		  AND nt.event_type = $1
		  AND (
			nt.case_type_code IS NULL
			OR (
				$2::uuid IS NOT NULL
				AND EXISTS (
					SELECT 1
					FROM cases c
					JOIN case_types ct ON ct.id = c.case_type_id
					WHERE c.id = $2::uuid
					  AND ct.code = nt.case_type_code
				)
			)
		  )
		ORDER BY nt.trigger_code ASC
	`, string(event.EventType), caseIDArg)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *NotificationService) loadCaseSnapshot(ctx context.Context, tx *sqlx.Tx, caseID *string) (*caseSnapshot, error) {
	if caseID == nil || strings.TrimSpace(*caseID) == "" {
		return nil, nil
	}
	var row caseSnapshot
	err := tx.GetContext(ctx, &row, `
		SELECT
			c.id::text AS id,
			c.reference_number,
			c.metadata,
			c.assigned_to,
			c.supervisor_id,
			c.current_stage_code,
			c.current_stage_ordinal,
			ct.code AS case_type_code
		FROM cases c
		JOIN case_types ct ON ct.id = c.case_type_id
		WHERE c.id = $1::uuid
	`, strings.TrimSpace(*caseID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (s *NotificationService) loadTaskSnapshot(ctx context.Context, tx *sqlx.Tx, taskID *string) (*taskSnapshot, error) {
	if taskID == nil || strings.TrimSpace(*taskID) == "" {
		return nil, nil
	}
	var row taskSnapshot
	err := tx.GetContext(ctx, &row, `
		SELECT
			t.id::text AS id,
			t.assignee_id,
			t.assigned_service,
			t.input_payload,
			t.output_payload,
			t.metadata,
			t.activity_code,
			t.stage_code,
			t.task_definition_code
		FROM tasks t
		WHERE t.id = $1::uuid
	`, strings.TrimSpace(*taskID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func buildTemplateContext(
	event model.Event,
	payload map[string]interface{},
	caseSnap *caseSnapshot,
	taskSnap *taskSnapshot,
) (map[string]interface{}, map[string]interface{}, error) {
	ctxData := map[string]interface{}{
		"event_type": string(event.EventType),
		"event":      payload,
	}
	for k, v := range payload {
		ctxData[k] = v
	}
	if event.CaseID != nil {
		ctxData["case_id"] = *event.CaseID
	}
	if event.TaskID != nil {
		ctxData["task_id"] = *event.TaskID
	}

	caseMetadata := map[string]interface{}{}
	if caseSnap != nil {
		decoded, err := unmarshalToMap(caseSnap.Metadata)
		if err != nil {
			return nil, nil, err
		}
		caseMetadata = decoded
		caseMap := map[string]interface{}{
			"id":                    caseSnap.ID,
			"reference_number":      caseSnap.ReferenceNumber,
			"current_stage_code":    nullableSQLString(caseSnap.CurrentStageCode),
			"current_stage_ordinal": caseSnap.CurrentStageOrdinal,
			"case_type":             caseSnap.CaseTypeCode,
			"metadata":              decoded,
		}
		ctxData["case"] = caseMap
		ctxData["case_type"] = caseSnap.CaseTypeCode
		ctxData["reference_number"] = caseSnap.ReferenceNumber
		if caseSnap.CurrentStageCode.Valid {
			ctxData["stage_code"] = caseSnap.CurrentStageCode.String
		}
		for k, v := range decoded {
			if _, exists := ctxData[k]; !exists {
				ctxData[k] = v
			}
		}
	}

	if taskSnap != nil {
		inputPayload, err := unmarshalToMap(taskSnap.InputPayload)
		if err != nil {
			return nil, nil, err
		}
		outputPayload, err := unmarshalToMap(taskSnap.OutputPayload)
		if err != nil {
			return nil, nil, err
		}
		taskMetadata, err := unmarshalToMap(taskSnap.Metadata)
		if err != nil {
			return nil, nil, err
		}
		taskMap := map[string]interface{}{
			"id":                   taskSnap.ID,
			"assignee_id":          nullableSQLString(taskSnap.AssigneeID),
			"assigned_service":     nullableSQLString(taskSnap.AssignedService),
			"activity_code":        taskSnap.ActivityCode,
			"stage_code":           taskSnap.StageCode,
			"task_definition_code": taskSnap.TaskDefinitionCode,
			"input_payload":        inputPayload,
			"output_payload":       outputPayload,
			"metadata":             taskMetadata,
		}
		ctxData["task"] = taskMap
		if _, exists := ctxData["stage_code"]; !exists && taskSnap.StageCode != "" {
			ctxData["stage_code"] = taskSnap.StageCode
		}
	}

	return ctxData, caseMetadata, nil
}

func (s *NotificationService) resolveRecipient(
	ctx context.Context,
	trigger triggerTemplateRow,
	caseSnap *caseSnapshot,
	taskSnap *taskSnapshot,
	payload map[string]interface{},
	caseMetadata map[string]interface{},
	contextData map[string]interface{},
) (string, error) {
	recipientType := model.NotificationRecipientType(trigger.RecipientType)
	channel := model.NotificationChannel(trigger.Channel)
	value := strings.TrimSpace(trigger.RecipientValue.String)

	switch recipientType {
	case model.NotificationRecipientCaseOwner:
		if caseSnap != nil && caseSnap.AssignedTo.Valid {
			return strings.TrimSpace(caseSnap.AssignedTo.String), nil
		}
		return "", fmt.Errorf("CASE_OWNER recipient unavailable")

	case model.NotificationRecipientTaskAssignee:
		if taskSnap != nil {
			if taskSnap.AssigneeID.Valid && strings.TrimSpace(taskSnap.AssigneeID.String) != "" {
				return strings.TrimSpace(taskSnap.AssigneeID.String), nil
			}
			if taskSnap.AssignedService.Valid && strings.TrimSpace(taskSnap.AssignedService.String) != "" {
				return strings.TrimSpace(taskSnap.AssignedService.String), nil
			}
		}
		return "", fmt.Errorf("TASK_ASSIGNEE recipient unavailable")

	case model.NotificationRecipientApprover:
		if approverID, ok := payload["approver_id"].(string); ok && strings.TrimSpace(approverID) != "" {
			return strings.TrimSpace(approverID), nil
		}
		if value != "" {
			return value, nil
		}
		return "", fmt.Errorf("APPROVER recipient unavailable")

	case model.NotificationRecipientSupervisor:
		if caseSnap != nil && caseSnap.SupervisorID.Valid && strings.TrimSpace(caseSnap.SupervisorID.String) != "" {
			return strings.TrimSpace(caseSnap.SupervisorID.String), nil
		}
		return "", fmt.Errorf("SUPERVISOR recipient unavailable")

	case model.NotificationRecipientBorrower:
		recipient := resolveBorrowerRecipient(channel, caseMetadata)
		if recipient == "" {
			return "", fmt.Errorf("BORROWER recipient unavailable")
		}
		return recipient, nil

	case model.NotificationRecipientFixedAddress:
		if value == "" {
			return "", fmt.Errorf("FIXED_ADDRESS recipient value is empty")
		}
		return value, nil

	case model.NotificationRecipientDynamicRule:
		if value == "" {
			return "", fmt.Errorf("DYNAMIC_RULE recipient value is empty")
		}
		if strings.Contains(value, "{{") {
			rendered, err := s.renderer.Render(ctx, value, contextData)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(rendered), nil
		}
		resolved, ok := lookupContextPath(contextData, value)
		if !ok {
			return "", fmt.Errorf("DYNAMIC_RULE path %s not found", value)
		}
		return strings.TrimSpace(fmt.Sprint(resolved)), nil

	default:
		return "", fmt.Errorf("unsupported recipient type %s", recipientType)
	}
}

func resolveBorrowerRecipient(channel model.NotificationChannel, caseMetadata map[string]interface{}) string {
	if caseMetadata == nil {
		return ""
	}
	lookup := func(keys ...string) string {
		for _, k := range keys {
			if val, ok := caseMetadata[k]; ok {
				s := strings.TrimSpace(fmt.Sprint(val))
				if s != "" {
					return s
				}
			}
		}
		return ""
	}

	switch channel {
	case model.NotificationChannelEmail:
		return lookup("borrower_email", "email", "contact_email")
	case model.NotificationChannelSMS:
		return lookup("borrower_phone", "phone_number", "mobile_number")
	case model.NotificationChannelInApp, model.NotificationChannelPush:
		return lookup("borrower_id", "user_id", "customer_id")
	default:
		return lookup("borrower_contact", "borrower_email", "borrower_phone", "borrower_id")
	}
}

func (s *NotificationService) renderQueuedContent(
	ctx context.Context,
	trigger triggerTemplateRow,
	contextData map[string]interface{},
) (*string, *string, error) {
	var subject *string
	var body *string

	if trigger.SubjectTemplate.Valid && strings.TrimSpace(trigger.SubjectTemplate.String) != "" {
		renderedSubject, err := s.renderer.Render(ctx, trigger.SubjectTemplate.String, contextData)
		if err != nil {
			return nil, nil, fmt.Errorf("render subject: %w", err)
		}
		renderedSubject = strings.TrimSpace(renderedSubject)
		subject = &renderedSubject
	}

	if strings.TrimSpace(trigger.BodyTemplate) != "" {
		var (
			renderedBody string
			err         error
		)
		if model.NotificationChannel(trigger.Channel) == model.NotificationChannelEmail {
			renderedBody, err = s.renderer.renderHTML(ctx, trigger.BodyTemplate, contextData)
		} else {
			renderedBody, err = s.renderer.Render(ctx, trigger.BodyTemplate, contextData)
		}
		if err != nil {
			return subject, nil, fmt.Errorf("render body: %w", err)
		}
		body = &renderedBody
	}

	return subject, body, nil
}

func (s *NotificationService) insertNotificationQueue(
	ctx context.Context,
	tx *sqlx.Tx,
	trigger triggerTemplateRow,
	caseID *string,
	taskID *string,
	recipient string,
	subject *string,
	body *string,
	scheduledAt time.Time,
	errorDetail json.RawMessage,
) (string, error) {
	var caseIDArg interface{}
	if caseID != nil && strings.TrimSpace(*caseID) != "" {
		caseIDArg = strings.TrimSpace(*caseID)
	}
	var taskIDArg interface{}
	if taskID != nil && strings.TrimSpace(*taskID) != "" {
		taskIDArg = strings.TrimSpace(*taskID)
	}
	var errArg interface{}
	if len(errorDetail) > 0 {
		errArg = errorDetail
	}

	var notificationID string
	err := tx.GetContext(ctx, &notificationID, `
		INSERT INTO notification_queue (
			trigger_code,
			case_id,
			task_id,
			template_code,
			channel,
			recipient,
			subject,
			body,
			priority,
			scheduled_at,
			status,
			attempts,
			error_detail,
			created_at,
			updated_at
		)
		VALUES (
			$1,
			$2::uuid,
			$3::uuid,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			'PENDING',
			0,
			$11::jsonb,
			now(),
			now()
		)
		RETURNING id::text
	`, trigger.TriggerCode, caseIDArg, taskIDArg, trigger.TemplateCode, trigger.Channel, recipient, subject, body, trigger.Priority, scheduledAt.UTC(), errArg)
	if err != nil {
		return "", err
	}
	return notificationID, nil
}

func (s *NotificationService) nextQuietHoursScheduleTx(ctx context.Context, tx *sqlx.Tx, recipient, channel string) (time.Time, bool, error) {
	type prefRow struct {
		QuietHoursStart    sql.NullString `db:"quiet_hours_start"`
		QuietHoursEnd      sql.NullString `db:"quiet_hours_end"`
		QuietHoursTimezone sql.NullString `db:"quiet_hours_timezone"`
	}
	var row prefRow
	err := tx.GetContext(ctx, &row, `
		SELECT
			quiet_hours_start::text AS quiet_hours_start,
			quiet_hours_end::text AS quiet_hours_end,
			quiet_hours_timezone
		FROM user_preferences
		WHERE user_id = $1
		  AND (channel = $2 OR channel IS NULL)
		ORDER BY CASE WHEN channel = $2 THEN 0 ELSE 1 END
		LIMIT 1
	`, recipient, strings.ToUpper(strings.TrimSpace(channel)))
	if err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	if !row.QuietHoursStart.Valid || !row.QuietHoursEnd.Valid || !row.QuietHoursTimezone.Valid {
		return time.Time{}, false, nil
	}
	return NextQuietHoursEnd(s.now(), row.QuietHoursStart.String, row.QuietHoursEnd.String, row.QuietHoursTimezone.String)
}

func checkDuplicateNotificationTx(
	ctx context.Context,
	tx *sqlx.Tx,
	recipient string,
	triggerCode string,
	caseID string,
	dedupeWindowMins int,
) (bool, error) {
	if dedupeWindowMins <= 0 {
		return false, nil
	}
	var caseIDArg interface{}
	if strings.TrimSpace(caseID) != "" {
		caseIDArg = strings.TrimSpace(caseID)
	}
	var exists bool
	err := tx.GetContext(ctx, &exists, `
		SELECT EXISTS (
			SELECT 1
			FROM notification_queue
			WHERE recipient = $1
			  AND trigger_code = $2
			  AND ((case_id = $3::uuid) OR ($3::uuid IS NULL AND case_id IS NULL))
			  AND created_at >= (now() - make_interval(mins => $4))
			  AND status <> 'CANCELLED'
		)
	`, recipient, triggerCode, caseIDArg, dedupeWindowMins)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func unmarshalToMap(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return map[string]interface{}{}, nil
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func nullableSQLString(v sql.NullString) interface{} {
	if !v.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(v.String)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func lookupContextPath(ctx map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = ctx
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, exists := m[part]
		if !exists {
			return nil, false
		}
		current = v
	}
	return current, true
}

func (s *NotificationService) now() time.Time {
	if s != nil && s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now().UTC()
}

func stringFromPtr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
