package notification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// GetNotificationHistory returns queue rows and delivery events for a case.
func GetNotificationHistory(
	ctx context.Context,
	db *sqlx.DB,
	caseID string,
) ([]model.NotificationRecord, error) {
	if db == nil {
		return nil, fmt.Errorf("GetNotificationHistory: db is nil")
	}
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("GetNotificationHistory: caseID is required")
	}

	type notificationRow struct {
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
	}

	var rows []notificationRow
	if err := db.SelectContext(ctx, &rows, `
		SELECT
			id::text AS id,
			trigger_code,
			case_id::text AS case_id,
			task_id::text AS task_id,
			template_code,
			channel,
			recipient,
			subject,
			body,
			priority,
			scheduled_at,
			status,
			attempts,
			last_attempt_at,
			sent_at,
			error_detail,
			acknowledged_at,
			created_at,
			updated_at
		FROM notification_queue
		WHERE case_id = $1::uuid
		ORDER BY created_at ASC
	`, strings.TrimSpace(caseID)); err != nil {
		return nil, fmt.Errorf("GetNotificationHistory: query notifications: %w", err)
	}

	records := make([]model.NotificationRecord, 0, len(rows))
	ids := make([]string, 0, len(rows))
	indexByID := make(map[string]int, len(rows))
	for i, row := range rows {
		n := model.Notification{
			ID:           row.ID,
			TriggerCode:  row.TriggerCode,
			TemplateCode: row.TemplateCode,
			Channel:      model.NotificationChannel(row.Channel),
			Recipient:    row.Recipient,
			Priority:     model.NotificationPriority(row.Priority),
			ScheduledAt:  row.ScheduledAt,
			Status:       model.NotificationStatus(row.Status),
			Attempts:     row.Attempts,
			ErrorDetail:  row.ErrorDetail,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
		}
		if row.CaseID.Valid {
			value := strings.TrimSpace(row.CaseID.String)
			n.CaseID = &value
		}
		if row.TaskID.Valid {
			value := strings.TrimSpace(row.TaskID.String)
			n.TaskID = &value
		}
		if row.Subject.Valid {
			value := row.Subject.String
			n.Subject = &value
		}
		if row.Body.Valid {
			value := row.Body.String
			n.Body = &value
		}
		if row.LastAttemptAt.Valid {
			tm := row.LastAttemptAt.Time
			n.LastAttemptAt = &tm
		}
		if row.SentAt.Valid {
			tm := row.SentAt.Time
			n.SentAt = &tm
		}
		if row.AcknowledgedAt.Valid {
			tm := row.AcknowledgedAt.Time
			n.AcknowledgedAt = &tm
		}

		records = append(records, model.NotificationRecord{Notification: n, Events: []model.NotificationDeliveryEvent{}})
		ids = append(ids, row.ID)
		indexByID[row.ID] = i
	}

	if len(ids) == 0 {
		return records, nil
	}

	query, args, err := sqlx.In(`
		SELECT
			id::text AS id,
			notification_id::text AS notification_id,
			event_type,
			event_timestamp,
			channel_response,
			user_agent,
			created_at
		FROM notification_delivery_events
		WHERE notification_id IN (?)
		ORDER BY event_timestamp ASC
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("GetNotificationHistory: build delivery query: %w", err)
	}
	query = db.Rebind(query)

	var deliveryRows []struct {
		ID              string         `db:"id"`
		NotificationID  string         `db:"notification_id"`
		EventType       string         `db:"event_type"`
		EventTimestamp  time.Time      `db:"event_timestamp"`
		ChannelResponse []byte         `db:"channel_response"`
		UserAgent       sql.NullString `db:"user_agent"`
		CreatedAt       time.Time      `db:"created_at"`
	}
	if err := db.SelectContext(ctx, &deliveryRows, query, args...); err != nil {
		return nil, fmt.Errorf("GetNotificationHistory: query delivery events: %w", err)
	}

	for _, row := range deliveryRows {
		idx, ok := indexByID[row.NotificationID]
		if !ok {
			continue
		}
		event := model.NotificationDeliveryEvent{
			ID:              row.ID,
			NotificationID:  row.NotificationID,
			EventType:       model.NotificationDeliveryEventType(row.EventType),
			EventTimestamp:  row.EventTimestamp,
			ChannelResponse: row.ChannelResponse,
			CreatedAt:       row.CreatedAt,
		}
		if row.UserAgent.Valid {
			ua := row.UserAgent.String
			event.UserAgent = &ua
		}
		records[idx].Events = append(records[idx].Events, event)
	}

	return records, nil
}

// GetDeliveryRate returns aggregate delivery metrics for a channel and date range.
func GetDeliveryRate(
	ctx context.Context,
	db *sqlx.DB,
	channel string,
	startDate, endDate time.Time,
) (model.DeliveryStats, error) {
	if db == nil {
		return model.DeliveryStats{}, fmt.Errorf("GetDeliveryRate: db is nil")
	}
	if strings.TrimSpace(channel) == "" {
		return model.DeliveryStats{}, fmt.Errorf("GetDeliveryRate: channel is required")
	}
	if !endDate.After(startDate) {
		return model.DeliveryStats{}, fmt.Errorf("GetDeliveryRate: endDate must be after startDate")
	}

	stats := model.DeliveryStats{Channel: model.NotificationChannel(strings.ToUpper(strings.TrimSpace(channel)))}
	var bounced int64
	err := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE q.status = 'SENT') AS total_sent,
			COUNT(*) FILTER (
				WHERE EXISTS (
					SELECT 1
					FROM notification_delivery_events e
					WHERE e.notification_id = q.id
					  AND e.event_type = 'DELIVERED'
				)
			) AS delivered,
			COUNT(*) FILTER (WHERE q.status = 'FAILED') AS failed,
			COUNT(*) FILTER (
				WHERE EXISTS (
					SELECT 1
					FROM notification_delivery_events e
					WHERE e.notification_id = q.id
					  AND e.event_type = 'BOUNCED'
				)
			) AS bounced
		FROM notification_queue q
		WHERE q.channel = $1
		  AND q.created_at >= $2
		  AND q.created_at < $3
	`, strings.ToUpper(strings.TrimSpace(channel)), startDate.UTC(), endDate.UTC()).Scan(&stats.TotalSent, &stats.Delivered, &stats.Failed, &bounced)
	if err != nil {
		return model.DeliveryStats{}, fmt.Errorf("GetDeliveryRate: query metrics: %w", err)
	}
	stats.Bounced = bounced
	if stats.TotalSent > 0 {
		stats.BounceRate = float64(stats.Bounced) / float64(stats.TotalSent)
	}
	return stats, nil
}

// RefreshCorrespondenceSummary refreshes the materialized correspondence summary view.
func RefreshCorrespondenceSummary(ctx context.Context, db *sqlx.DB) error {
	if db == nil {
		return fmt.Errorf("RefreshCorrespondenceSummary: db is nil")
	}
	if _, err := db.ExecContext(ctx, `REFRESH MATERIALIZED VIEW CONCURRENTLY correspondence_summary`); err != nil {
		return fmt.Errorf("RefreshCorrespondenceSummary: %w", err)
	}
	return nil
}

// GetCorrespondenceSummary returns the materialized notification summary for a case.
func GetCorrespondenceSummary(ctx context.Context, db *sqlx.DB, caseID string) (*model.CorrespondenceSummary, error) {
	if db == nil {
		return nil, fmt.Errorf("GetCorrespondenceSummary: db is nil")
	}
	if strings.TrimSpace(caseID) == "" {
		return nil, fmt.Errorf("GetCorrespondenceSummary: caseID is required")
	}

	var row model.CorrespondenceSummary
	err := db.GetContext(ctx, &row, `
		SELECT
			case_id::text,
			total_sent,
			sent_by_channel,
			unacknowledged_borrower_count,
			failed_count,
			failed_reasons,
			avg_delivery_seconds,
			refreshed_at
		FROM correspondence_summary
		WHERE case_id = $1::uuid
	`, strings.TrimSpace(caseID))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetCorrespondenceSummary: %w", err)
	}

	return &row, nil
}
