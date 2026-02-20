package notification

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// AcknowledgeNotification sets acknowledged_at for borrower notifications.
func AcknowledgeNotification(ctx context.Context, db *sqlx.DB, notificationID string) (time.Time, bool, error) {
	if db == nil {
		return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: db is nil")
	}
	notificationID = strings.TrimSpace(notificationID)
	if notificationID == "" {
		return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: notificationID is required")
	}

	tx, err := db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: begin tx: %w", err)
	}
	defer tx.Rollback()

	var existing sql.NullTime
	err = tx.GetContext(ctx, &existing, `
		SELECT q.acknowledged_at
		FROM notification_queue q
		JOIN notification_triggers t
		  ON t.trigger_code = q.trigger_code
		WHERE q.id = $1::uuid
		  AND t.recipient_type = 'BORROWER'
		FOR UPDATE OF q
	`, notificationID)
	if err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: notification not found for borrower")
		}
		return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: select notification: %w", err)
	}

	if existing.Valid {
		if err := tx.Commit(); err != nil {
			return time.Time{}, true, fmt.Errorf("AcknowledgeNotification: commit existing acknowledgement: %w", err)
		}
		return existing.Time.UTC(), true, nil
	}

	var acknowledgedAt time.Time
	err = tx.GetContext(ctx, &acknowledgedAt, `
		UPDATE notification_queue
		SET acknowledged_at = now(),
			updated_at = now()
		WHERE id = $1::uuid
		RETURNING acknowledged_at
	`, notificationID)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: update notification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, false, fmt.Errorf("AcknowledgeNotification: commit: %w", err)
	}

	return acknowledgedAt.UTC(), false, nil
}
