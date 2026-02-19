package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

type NotificationDispatcher struct {
	db               *sqlx.DB
	templateRenderer *TemplateRenderer
	channels         map[string]NotificationChannel
	circuitBreaker   *CircuitBreaker
	pollInterval     time.Duration
	batchSize        int
	maxRetries       int
	logger           *slog.Logger

	baseRetryInterval time.Duration
	nowFunc           func() time.Time
	jitterFunc        func(base time.Duration) time.Duration
	publisher         EventPublisher

	checkCircuitFn         func(ctx context.Context, channel string) (bool, error)
	recordCircuitSuccessFn func(ctx context.Context, tx *sqlx.Tx, channel string) error
	recordCircuitFailureFn func(ctx context.Context, tx *sqlx.Tx, channel string) error
}

func NewNotificationDispatcher(
	db *sqlx.DB,
	renderer *TemplateRenderer,
	channels map[string]NotificationChannel,
	breaker *CircuitBreaker,
	pollInterval time.Duration,
	batchSize int,
	maxRetries int,
	logger *slog.Logger,
	publisher EventPublisher,
) *NotificationDispatcher {
	if renderer == nil {
		renderer = NewTemplateRenderer()
	}
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 500
	}
	if maxRetries <= 0 {
		maxRetries = 5
	}
	if logger == nil {
		logger = slog.Default()
	}
	if publisher == nil {
		publisher = &SQLXEventPublisher{}
	}
	if channels == nil {
		channels = map[string]NotificationChannel{}
	}
	normalizedChannels := make(map[string]NotificationChannel, len(channels))
	for k, v := range channels {
		normalizedChannels[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	if breaker == nil {
		breaker = NewCircuitBreaker(db, 10, 5*time.Minute, 3, logger, publisher)
	}
	return &NotificationDispatcher{
		db:                db,
		templateRenderer:  renderer,
		channels:          normalizedChannels,
		circuitBreaker:    breaker,
		pollInterval:      pollInterval,
		batchSize:         batchSize,
		maxRetries:        maxRetries,
		logger:            logger,
		baseRetryInterval: 30 * time.Second,
		nowFunc:           func() time.Time { return time.Now().UTC() },
		jitterFunc: func(base time.Duration) time.Duration {
			if base <= 0 {
				return 0
			}
			return time.Duration(rand.Int63n(int64(base)))
		},
		publisher: publisher,
		checkCircuitFn: func(ctx context.Context, channel string) (bool, error) {
			return breaker.CheckState(ctx, channel)
		},
		recordCircuitSuccessFn: func(ctx context.Context, tx *sqlx.Tx, channel string) error {
			return breaker.RecordSuccess(ctx, tx, channel)
		},
		recordCircuitFailureFn: func(ctx context.Context, tx *sqlx.Tx, channel string) error {
			return breaker.RecordFailure(ctx, tx, channel)
		},
	}
}

type dispatchRow struct {
	ID             string         `db:"id"`
	TriggerCode    string         `db:"trigger_code"`
	CaseID         sql.NullString `db:"case_id"`
	TaskID         sql.NullString `db:"task_id"`
	TemplateCode   string         `db:"template_code"`
	Channel        string         `db:"channel"`
	Recipient      string         `db:"recipient"`
	Subject        sql.NullString `db:"subject"`
	Body           sql.NullString `db:"body"`
	Priority       string         `db:"priority"`
	ScheduledAt    time.Time      `db:"scheduled_at"`
	Status         string         `db:"status"`
	Attempts       int            `db:"attempts"`
	LastAttemptAt  sql.NullTime   `db:"last_attempt_at"`
	SentAt         sql.NullTime   `db:"sent_at"`
	ErrorDetail    []byte         `db:"error_detail"`
	AcknowledgedAt sql.NullTime   `db:"acknowledged_at"`
	CreatedAt      time.Time      `db:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"`

	TriggerEventType     string         `db:"trigger_event_type"`
	TemplateSubject      sql.NullString `db:"template_subject_template"`
	TemplateBody         sql.NullString `db:"template_body_template"`
	CaseReferenceNumber  sql.NullString `db:"case_reference_number"`
	CaseTypeCode         sql.NullString `db:"case_type_code"`
	CaseMetadata         []byte         `db:"case_metadata"`
	CaseCurrentStageCode sql.NullString `db:"case_current_stage_code"`
	TaskInputPayload     []byte         `db:"task_input_payload"`
	TaskOutputPayload    []byte         `db:"task_output_payload"`
	TaskMetadata         []byte         `db:"task_metadata"`
	TaskStageCode        sql.NullString `db:"task_stage_code"`
	TaskActivityCode     sql.NullString `db:"task_activity_code"`
	TaskDefinitionCode   sql.NullString `db:"task_definition_code"`
}

// Run polls due notifications, dispatches them, and updates queue state.
func (d *NotificationDispatcher) Run(ctx context.Context) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("Run: db is nil")
	}

	tx, err := d.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("Run: begin tx: %w", err)
	}
	defer tx.Rollback()

	notifications, err := d.pollPendingNotifications(ctx, tx)
	if err != nil {
		return fmt.Errorf("Run: poll queue: %w", err)
	}

	for _, row := range notifications {
		if err := d.processNotification(ctx, tx, row); err != nil {
			d.logger.Error("notification dispatch failed", "notification_id", row.ID, "error", err)
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("Run: commit: %w", err)
	}
	return nil
}

// Start runs dispatcher in a loop until context cancellation.
func (d *NotificationDispatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.Run(ctx); err != nil {
				d.logger.Error("notification dispatcher run failed", "error", err)
			}
		}
	}
}

func (d *NotificationDispatcher) pollPendingNotifications(ctx context.Context, tx *sqlx.Tx) ([]dispatchRow, error) {
	var rows []dispatchRow
	err := tx.SelectContext(ctx, &rows, `
		SELECT
			q.id::text AS id,
			q.trigger_code,
			q.case_id::text AS case_id,
			q.task_id::text AS task_id,
			q.template_code,
			q.channel,
			q.recipient,
			q.subject,
			q.body,
			q.priority,
			q.scheduled_at,
			q.status,
			q.attempts,
			q.last_attempt_at,
			q.sent_at,
			q.error_detail,
			q.acknowledged_at,
			q.created_at,
			q.updated_at,
			nt.event_type AS trigger_event_type,
			tpl.subject_template AS template_subject_template,
			tpl.body_template AS template_body_template,
			c.reference_number AS case_reference_number,
			ct.code AS case_type_code,
			c.metadata AS case_metadata,
			c.current_stage_code AS case_current_stage_code,
			t.input_payload AS task_input_payload,
			t.output_payload AS task_output_payload,
			t.metadata AS task_metadata,
			t.stage_code AS task_stage_code,
			t.activity_code AS task_activity_code,
			t.task_definition_code AS task_definition_code
		FROM notification_queue q
		JOIN notification_triggers nt
		  ON nt.trigger_code = q.trigger_code
		LEFT JOIN notification_templates tpl
		  ON tpl.template_code = q.template_code
		LEFT JOIN cases c
		  ON c.id = q.case_id
		LEFT JOIN case_types ct
		  ON ct.id = c.case_type_id
		LEFT JOIN tasks t
		  ON t.id = q.task_id
		WHERE q.status = 'PENDING'
		  AND q.scheduled_at <= now()
		  AND q.attempts < $1
		ORDER BY
			CASE q.priority
				WHEN 'URGENT' THEN 4
				WHEN 'HIGH' THEN 3
				WHEN 'NORMAL' THEN 2
				ELSE 1
			END DESC,
			q.scheduled_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, d.maxRetries, d.batchSize)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (d *NotificationDispatcher) processNotification(ctx context.Context, tx *sqlx.Tx, row dispatchRow) error {
	notif := row.toNotification()
	now := d.now()

	if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventClaimed, json.RawMessage(`{"state":"claimed"}`), nil); err != nil {
		return fmt.Errorf("processNotification: append CLAIMED: %w", err)
	}

	allow, err := d.checkCircuitFn(ctx, row.Channel)
	if err != nil {
		return fmt.Errorf("processNotification: circuit check: %w", err)
	}
	if !allow {
		errDetail := buildErrorDetail("CIRCUIT_OPEN", ErrCircuitOpen)
		if err := d.markFailed(ctx, tx, row, now, errDetail, "circuit breaker open"); err != nil {
			return fmt.Errorf("processNotification: mark failed for open circuit: %w", err)
		}
		return nil
	}

	suppress, reason, err := CheckUserPreferences(ctx, d.db, row.Recipient, row.Channel, row.TriggerEventType)
	if err != nil {
		return fmt.Errorf("processNotification: check preferences: %w", err)
	}
	if suppress {
		if err := d.markSuppressed(ctx, tx, row, model.NotificationSuppressionReason(reason), "suppressed by preference"); err != nil {
			return fmt.Errorf("processNotification: mark suppressed: %w", err)
		}
		return nil
	}
	if reason == string(model.NotificationSuppressionQuietHours) {
		nextAllowed, withinQuiet, qErr := getNextQuietHoursScheduleTx(ctx, tx, row.Recipient, row.Channel, now)
		if qErr != nil {
			return fmt.Errorf("processNotification: quiet-hours delay: %w", qErr)
		}
		if withinQuiet && nextAllowed.After(now) {
			if err := d.delayForQuietHours(ctx, tx, row, nextAllowed); err != nil {
				return fmt.Errorf("processNotification: apply quiet-hours delay: %w", err)
			}
			return nil
		}
	}

	channel, exists := d.channels[strings.ToUpper(strings.TrimSpace(row.Channel))]
	if !exists {
		errDetail := buildErrorDetail("CHANNEL_NOT_CONFIGURED", fmt.Errorf("channel %s not configured", row.Channel))
		if err := d.markFailed(ctx, tx, row, now, errDetail, "channel adapter missing"); err != nil {
			return fmt.Errorf("processNotification: missing channel mark failed: %w", err)
		}
		return nil
	}

	if !row.Subject.Valid || !row.Body.Valid || strings.TrimSpace(row.Body.String) == "" {
		subject, body, renderErr := d.renderIfNeeded(ctx, row)
		if renderErr != nil {
			errDetail := buildErrorDetail("TEMPLATE_RENDER_ERROR", renderErr)
			if err := d.markFailed(ctx, tx, row, now, errDetail, "template render failed"); err != nil {
				return fmt.Errorf("processNotification: render failure mark failed: %w", err)
			}
			return nil
		}
		notif.Subject = subject
		notif.Body = body
		if err := d.updateRenderedContent(ctx, tx, row.ID, subject, body); err != nil {
			return fmt.Errorf("processNotification: persist rendered content: %w", err)
		}
		if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventRendered, json.RawMessage(`{"state":"rendered"}`), nil); err != nil {
			return fmt.Errorf("processNotification: append RENDERED: %w", err)
		}
	}

	if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventDispatched, json.RawMessage(fmt.Sprintf(`{"idempotency_key":"%s"}`, row.ID)), nil); err != nil {
		return fmt.Errorf("processNotification: append DISPATCHED: %w", err)
	}

	if txAware, ok := channel.(interface {
		SendInTx(context.Context, *sqlx.Tx, Notification) error
	}); ok {
		err = txAware.SendInTx(ctx, tx, notif)
	} else {
		err = channel.Send(ctx, notif)
	}
	if err != nil {
		attempts := row.Attempts + 1
		errDetail := buildErrorDetail("CHANNEL_SEND_ERROR", err)

		if channel.IsTransientError(err) {
			if cbErr := d.recordCircuitFailureFn(ctx, tx, row.Channel); cbErr != nil {
				d.logger.Error("failed to record circuit failure", "channel", row.Channel, "error", cbErr)
			}
			if attempts >= d.maxRetries {
				if err := d.markFailedWithAttempts(ctx, tx, row, now, attempts, errDetail, "max retries reached"); err != nil {
					return fmt.Errorf("processNotification: mark failed after retries: %w", err)
				}
			} else {
				nextRetry := d.computeNextRetry(now, attempts)
				if err := d.scheduleRetry(ctx, tx, row.ID, attempts, now, nextRetry, errDetail); err != nil {
					return fmt.Errorf("processNotification: schedule retry: %w", err)
				}
				if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventFailed, json.RawMessage(`{"reason":"transient"}`), nil); err != nil {
					return fmt.Errorf("processNotification: append FAILED transient: %w", err)
				}
				if err := publishNotificationInternalEvent(ctx, tx, d.publisher, model.EventNotificationFailed, NotificationEventPayload{
					NotificationID: row.ID,
					TriggerCode:    row.TriggerCode,
					CaseID:         notif.CaseID,
					TaskID:         notif.TaskID,
					Channel:        model.NotificationChannel(row.Channel),
					Recipient:      row.Recipient,
					Status:         model.NotificationStatusPending,
					Reason:         "transient retry scheduled",
				}); err != nil {
					return fmt.Errorf("processNotification: publish transient failed event: %w", err)
				}
				return nil
			}
		} else {
			if cbErr := d.recordCircuitFailureFn(ctx, tx, row.Channel); cbErr != nil {
				d.logger.Error("failed to record permanent failure to breaker", "channel", row.Channel, "error", cbErr)
			}
			if err := d.markFailedWithAttempts(ctx, tx, row, now, attempts, errDetail, "permanent channel error"); err != nil {
				return fmt.Errorf("processNotification: mark permanent failed: %w", err)
			}
		}
		return nil
	}

	if cbErr := d.recordCircuitSuccessFn(ctx, tx, row.Channel); cbErr != nil {
		d.logger.Error("failed to record breaker success", "channel", row.Channel, "error", cbErr)
	}
	if err := d.markSent(ctx, tx, row, now); err != nil {
		return fmt.Errorf("processNotification: mark sent: %w", err)
	}
	return nil
}

func (d *NotificationDispatcher) renderIfNeeded(ctx context.Context, row dispatchRow) (*string, *string, error) {
	if !row.TemplateBody.Valid || strings.TrimSpace(row.TemplateBody.String) == "" {
		return row.nullableSubjectPtr(), row.nullableBodyPtr(), nil
	}
	contextData, err := buildDispatchContext(row)
	if err != nil {
		return nil, nil, fmt.Errorf("renderIfNeeded: build context: %w", err)
	}

	var subject *string
	if row.TemplateSubject.Valid && strings.TrimSpace(row.TemplateSubject.String) != "" {
		renderedSubject, err := d.templateRenderer.Render(ctx, row.TemplateSubject.String, contextData)
		if err != nil {
			return nil, nil, fmt.Errorf("renderIfNeeded: render subject: %w", err)
		}
		renderedSubject = strings.TrimSpace(renderedSubject)
		subject = &renderedSubject
	} else {
		subject = row.nullableSubjectPtr()
	}

	var renderedBody string
	if strings.EqualFold(row.Channel, string(model.NotificationChannelEmail)) {
		renderedBody, err = d.templateRenderer.renderHTML(ctx, row.TemplateBody.String, contextData)
	} else {
		renderedBody, err = d.templateRenderer.Render(ctx, row.TemplateBody.String, contextData)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("renderIfNeeded: render body: %w", err)
	}
	body := renderedBody
	return subject, &body, nil
}

func buildDispatchContext(row dispatchRow) (map[string]interface{}, error) {
	ctxData := map[string]interface{}{
		"case_id":    nullableString(row.CaseID),
		"task_id":    nullableString(row.TaskID),
		"case_type":  nullableString(row.CaseTypeCode),
		"stage_code": nullableString(row.CaseCurrentStageCode),
	}

	caseMetadata, err := unmarshalToMap(row.CaseMetadata)
	if err != nil {
		return nil, err
	}
	taskInput, err := unmarshalToMap(row.TaskInputPayload)
	if err != nil {
		return nil, err
	}
	taskOutput, err := unmarshalToMap(row.TaskOutputPayload)
	if err != nil {
		return nil, err
	}
	taskMetadata, err := unmarshalToMap(row.TaskMetadata)
	if err != nil {
		return nil, err
	}

	caseMap := map[string]interface{}{
		"id":               nullableString(row.CaseID),
		"reference_number": nullableString(row.CaseReferenceNumber),
		"case_type":        nullableString(row.CaseTypeCode),
		"stage_code":       nullableString(row.CaseCurrentStageCode),
		"metadata":         caseMetadata,
	}
	taskMap := map[string]interface{}{
		"id":                   nullableString(row.TaskID),
		"stage_code":           nullableString(row.TaskStageCode),
		"activity_code":        nullableString(row.TaskActivityCode),
		"task_definition_code": nullableString(row.TaskDefinitionCode),
		"input_payload":        taskInput,
		"output_payload":       taskOutput,
		"metadata":             taskMetadata,
	}

	ctxData["case"] = caseMap
	ctxData["task"] = taskMap
	ctxData["reference_number"] = nullableString(row.CaseReferenceNumber)
	for k, v := range caseMetadata {
		if _, exists := ctxData[k]; !exists {
			ctxData[k] = v
		}
	}
	return ctxData, nil
}

func (d *NotificationDispatcher) markSent(ctx context.Context, tx *sqlx.Tx, row dispatchRow, now time.Time) error {
	if err := ValidateNotificationTransition(ctx, model.NotificationStatus(row.Status), model.NotificationStatusSent); err != nil {
		return fmt.Errorf("markSent: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_queue
		SET status = 'SENT',
			sent_at = $2,
			last_attempt_at = $2,
			attempts = attempts + 1,
			error_detail = NULL,
			updated_at = now()
		WHERE id = $1::uuid
	`, row.ID, now)
	if err != nil {
		return fmt.Errorf("markSent: update queue: %w", err)
	}
	if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventDelivered, json.RawMessage(`{"state":"sent"}`), nil); err != nil {
		return fmt.Errorf("markSent: append DELIVERED: %w", err)
	}
	if err := publishNotificationInternalEvent(ctx, tx, d.publisher, model.EventNotificationSent, NotificationEventPayload{
		NotificationID: row.ID,
		TriggerCode:    row.TriggerCode,
		CaseID:         row.toNotification().CaseID,
		TaskID:         row.toNotification().TaskID,
		Channel:        model.NotificationChannel(row.Channel),
		Recipient:      row.Recipient,
		Status:         model.NotificationStatusSent,
		Reason:         "sent",
	}); err != nil {
		return fmt.Errorf("markSent: publish event: %w", err)
	}
	return nil
}

func (d *NotificationDispatcher) markFailed(ctx context.Context, tx *sqlx.Tx, row dispatchRow, now time.Time, errorDetail json.RawMessage, reason string) error {
	return d.markFailedWithAttempts(ctx, tx, row, now, row.Attempts+1, errorDetail, reason)
}

func (d *NotificationDispatcher) markFailedWithAttempts(ctx context.Context, tx *sqlx.Tx, row dispatchRow, now time.Time, attempts int, errorDetail json.RawMessage, reason string) error {
	if err := ValidateNotificationTransition(ctx, model.NotificationStatus(row.Status), model.NotificationStatusFailed); err != nil {
		return fmt.Errorf("markFailedWithAttempts: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_queue
		SET status = 'FAILED',
			attempts = $2,
			last_attempt_at = $3,
			error_detail = $4::jsonb,
			updated_at = now()
		WHERE id = $1::uuid
	`, row.ID, attempts, now, errorDetail)
	if err != nil {
		return fmt.Errorf("markFailedWithAttempts: update queue: %w", err)
	}
	if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventFailed, json.RawMessage(fmt.Sprintf(`{"reason":%q}`, reason)), nil); err != nil {
		return fmt.Errorf("markFailedWithAttempts: append FAILED: %w", err)
	}
	if err := publishNotificationInternalEvent(ctx, tx, d.publisher, model.EventNotificationFailed, NotificationEventPayload{
		NotificationID: row.ID,
		TriggerCode:    row.TriggerCode,
		CaseID:         row.toNotification().CaseID,
		TaskID:         row.toNotification().TaskID,
		Channel:        model.NotificationChannel(row.Channel),
		Recipient:      row.Recipient,
		Status:         model.NotificationStatusFailed,
		Reason:         reason,
	}); err != nil {
		return fmt.Errorf("markFailedWithAttempts: publish event: %w", err)
	}
	return nil
}

func (d *NotificationDispatcher) markSuppressed(ctx context.Context, tx *sqlx.Tx, row dispatchRow, suppression model.NotificationSuppressionReason, reason string) error {
	if err := ValidateNotificationTransition(ctx, model.NotificationStatus(row.Status), model.NotificationStatusSuppressed); err != nil {
		return fmt.Errorf("markSuppressed: %w", err)
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_queue
		SET status = 'SUPPRESSED',
			error_detail = $2::jsonb,
			updated_at = now()
		WHERE id = $1::uuid
	`, row.ID, buildErrorDetail(string(suppression), nil))
	if err != nil {
		return fmt.Errorf("markSuppressed: update queue: %w", err)
	}
	caseID := row.toNotification().CaseID
	if err := logSuppression(ctx, tx, &row.ID, row.TriggerCode, row.Recipient, caseID, suppression); err != nil {
		return fmt.Errorf("markSuppressed: log suppression: %w", err)
	}
	if err := appendDeliveryEvent(ctx, tx, row.ID, model.NotificationDeliveryEventFailed, json.RawMessage(fmt.Sprintf(`{"reason":%q}`, reason)), nil); err != nil {
		return fmt.Errorf("markSuppressed: append event: %w", err)
	}
	if err := publishNotificationInternalEvent(ctx, tx, d.publisher, model.EventNotificationSuppressed, NotificationEventPayload{
		NotificationID: row.ID,
		TriggerCode:    row.TriggerCode,
		CaseID:         row.toNotification().CaseID,
		TaskID:         row.toNotification().TaskID,
		Channel:        model.NotificationChannel(row.Channel),
		Recipient:      row.Recipient,
		Status:         model.NotificationStatusSuppressed,
		Suppression:    &suppression,
		Reason:         reason,
	}); err != nil {
		return fmt.Errorf("markSuppressed: publish event: %w", err)
	}
	return nil
}

func (d *NotificationDispatcher) delayForQuietHours(ctx context.Context, tx *sqlx.Tx, row dispatchRow, nextAllowed time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_queue
		SET scheduled_at = $2,
			updated_at = now()
		WHERE id = $1::uuid
	`, row.ID, nextAllowed.UTC())
	if err != nil {
		return fmt.Errorf("delayForQuietHours: %w", err)
	}
	caseID := row.toNotification().CaseID
	if err := logSuppression(ctx, tx, &row.ID, row.TriggerCode, row.Recipient, caseID, model.NotificationSuppressionQuietHours); err != nil {
		return fmt.Errorf("delayForQuietHours: log suppression: %w", err)
	}
	return nil
}

func (d *NotificationDispatcher) updateRenderedContent(ctx context.Context, tx *sqlx.Tx, notificationID string, subject, body *string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_queue
		SET subject = $2,
			body = $3,
			updated_at = now()
		WHERE id = $1::uuid
	`, notificationID, subject, body)
	if err != nil {
		return err
	}
	return nil
}

func (d *NotificationDispatcher) scheduleRetry(
	ctx context.Context,
	tx *sqlx.Tx,
	notificationID string,
	attempts int,
	now time.Time,
	nextRetry time.Time,
	errorDetail json.RawMessage,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE notification_queue
		SET attempts = $2,
			last_attempt_at = $3,
			scheduled_at = $4,
			error_detail = $5::jsonb,
			updated_at = now()
		WHERE id = $1::uuid
	`, notificationID, attempts, now, nextRetry, errorDetail)
	if err != nil {
		return err
	}
	return nil
}

func (d *NotificationDispatcher) computeNextRetry(now time.Time, attempt int) time.Time {
	if attempt < 0 {
		attempt = 0
	}
	base := d.baseRetryInterval
	if base <= 0 {
		base = 30 * time.Second
	}
	delay := time.Duration(1<<uint(attempt)) * base
	jitter := time.Duration(0)
	if d.jitterFunc != nil {
		jitter = d.jitterFunc(base)
	}
	return now.Add(delay + jitter)
}

func (d *NotificationDispatcher) now() time.Time {
	if d != nil && d.nowFunc != nil {
		return d.nowFunc()
	}
	return time.Now().UTC()
}

func (r dispatchRow) toNotification() Notification {
	n := Notification{
		ID:           r.ID,
		TriggerCode:  r.TriggerCode,
		TemplateCode: r.TemplateCode,
		Channel:      model.NotificationChannel(r.Channel),
		Recipient:    r.Recipient,
		Priority:     model.NotificationPriority(r.Priority),
		ScheduledAt:  r.ScheduledAt,
		Status:       model.NotificationStatus(r.Status),
		Attempts:     r.Attempts,
		ErrorDetail:  r.ErrorDetail,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.CaseID.Valid {
		v := strings.TrimSpace(r.CaseID.String)
		n.CaseID = &v
	}
	if r.TaskID.Valid {
		v := strings.TrimSpace(r.TaskID.String)
		n.TaskID = &v
	}
	if r.Subject.Valid {
		v := r.Subject.String
		n.Subject = &v
	}
	if r.Body.Valid {
		v := r.Body.String
		n.Body = &v
	}
	if r.LastAttemptAt.Valid {
		tm := r.LastAttemptAt.Time
		n.LastAttemptAt = &tm
	}
	if r.SentAt.Valid {
		tm := r.SentAt.Time
		n.SentAt = &tm
	}
	if r.AcknowledgedAt.Valid {
		tm := r.AcknowledgedAt.Time
		n.AcknowledgedAt = &tm
	}
	return n
}

func (r dispatchRow) nullableSubjectPtr() *string {
	if !r.Subject.Valid {
		return nil
	}
	v := r.Subject.String
	return &v
}

func (r dispatchRow) nullableBodyPtr() *string {
	if !r.Body.Valid {
		return nil
	}
	v := r.Body.String
	return &v
}

func nullableString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
}
