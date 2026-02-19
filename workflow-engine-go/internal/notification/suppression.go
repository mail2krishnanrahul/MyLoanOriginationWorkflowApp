package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"workflow-engine/pkg/model"

	"github.com/jmoiron/sqlx"
)

// CheckDuplicateNotification queries for recent identical notifications within the dedupe window.
func CheckDuplicateNotification(
	ctx context.Context,
	db *sqlx.DB,
	recipient string,
	triggerCode string,
	caseID string,
	dedupeWindowMins int,
) (isDuplicate bool, err error) {
	if db == nil {
		return false, fmt.Errorf("CheckDuplicateNotification: db is nil")
	}
	if recipient == "" || triggerCode == "" || dedupeWindowMins <= 0 {
		return false, nil
	}

	var caseIDArg *string
	if strings.TrimSpace(caseID) != "" {
		trimmed := strings.TrimSpace(caseID)
		caseIDArg = &trimmed
	}

	var exists bool
	if err := db.GetContext(ctx, &exists, `
		SELECT EXISTS (
			SELECT 1
			FROM notification_queue
			WHERE recipient = $1
			  AND trigger_code = $2
			  AND ((case_id = $3::uuid) OR ($3::uuid IS NULL AND case_id IS NULL))
			  AND created_at >= (now() - make_interval(mins => $4))
			  AND status <> 'CANCELLED'
		)
	`, recipient, triggerCode, caseIDArg, dedupeWindowMins); err != nil {
		return false, fmt.Errorf("CheckDuplicateNotification: %w", err)
	}

	return exists, nil
}

// CheckUserPreferences evaluates opt-out, quiet-hours, and enabled notification types.
// It returns suppress=true for OPT_OUT and TYPE_DISABLED.
// For quiet-hours windows it returns suppress=false and reason=QUIET_HOURS so callers can delay schedule.
func CheckUserPreferences(
	ctx context.Context,
	db *sqlx.DB,
	recipient string,
	channel string,
	notifType string,
) (suppress bool, reason string, err error) {
	if db == nil {
		return false, "", fmt.Errorf("CheckUserPreferences: db is nil")
	}
	if strings.TrimSpace(recipient) == "" {
		return false, "", nil
	}

	type prefRow struct {
		OptOut             bool            `db:"opt_out"`
		QuietHoursStart    sql.NullString  `db:"quiet_hours_start"`
		QuietHoursEnd      sql.NullString  `db:"quiet_hours_end"`
		QuietHoursTimezone sql.NullString  `db:"quiet_hours_timezone"`
		EnabledTypes       json.RawMessage `db:"enabled_notification_types"`
	}

	var row prefRow
	err = db.GetContext(ctx, &row, `
		SELECT
			opt_out,
			quiet_hours_start::text AS quiet_hours_start,
			quiet_hours_end::text AS quiet_hours_end,
			quiet_hours_timezone,
			enabled_notification_types
		FROM user_preferences
		WHERE user_id = $1
		  AND (channel = $2 OR channel IS NULL)
		ORDER BY CASE WHEN channel = $2 THEN 0 ELSE 1 END
		LIMIT 1
	`, recipient, strings.ToUpper(strings.TrimSpace(channel)))
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		}
		return false, "", fmt.Errorf("CheckUserPreferences: query preference: %w", err)
	}

	if row.OptOut {
		return true, string(model.NotificationSuppressionOptOut), nil
	}

	enabled, parseErr := parseEnabledTypes(row.EnabledTypes)
	if parseErr != nil {
		return false, "", fmt.Errorf("CheckUserPreferences: parse enabled types: %w", parseErr)
	}
	if len(enabled) > 0 && strings.TrimSpace(notifType) != "" {
		if _, ok := enabled[strings.ToUpper(strings.TrimSpace(notifType))]; !ok {
			return true, string(model.NotificationSuppressionTypeDisabled), nil
		}
	}

	if row.QuietHoursStart.Valid && row.QuietHoursEnd.Valid && row.QuietHoursTimezone.Valid {
		_, withinQuiet, qErr := NextQuietHoursEnd(
			time.Now().UTC(),
			row.QuietHoursStart.String,
			row.QuietHoursEnd.String,
			row.QuietHoursTimezone.String,
		)
		if qErr != nil {
			return false, "", fmt.Errorf("CheckUserPreferences: quiet-hours evaluation: %w", qErr)
		}
		if withinQuiet {
			return false, string(model.NotificationSuppressionQuietHours), nil
		}
	}

	return false, "", nil
}

// NextQuietHoursEnd returns the next quiet-hours end timestamp in UTC when now is in quiet hours.
func NextQuietHoursEnd(nowUTC time.Time, quietStart, quietEnd, timezone string) (time.Time, bool, error) {
	if strings.TrimSpace(quietStart) == "" || strings.TrimSpace(quietEnd) == "" || strings.TrimSpace(timezone) == "" {
		return time.Time{}, false, nil
	}

	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("NextQuietHoursEnd: load timezone %q: %w", timezone, err)
	}
	startClock, err := parseClockValue(quietStart)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("NextQuietHoursEnd: parse quiet_start: %w", err)
	}
	endClock, err := parseClockValue(quietEnd)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("NextQuietHoursEnd: parse quiet_end: %w", err)
	}

	nowLocal := nowUTC.In(loc)
	startToday := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), startClock.Hour(), startClock.Minute(), startClock.Second(), 0, loc)
	endToday := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), endClock.Hour(), endClock.Minute(), endClock.Second(), 0, loc)

	if startToday.Equal(endToday) {
		return time.Time{}, false, nil
	}

	if startToday.Before(endToday) {
		if !nowLocal.Before(startToday) && nowLocal.Before(endToday) {
			return endToday.UTC(), true, nil
		}
		return time.Time{}, false, nil
	}

	if !nowLocal.Before(startToday) {
		endTomorrow := endToday.Add(24 * time.Hour)
		return endTomorrow.UTC(), true, nil
	}
	if nowLocal.Before(endToday) {
		return endToday.UTC(), true, nil
	}
	return time.Time{}, false, nil
}

func parseEnabledTypes(raw json.RawMessage) (map[string]struct{}, error) {
	if len(raw) == 0 {
		return map[string]struct{}{}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		trimmed := strings.ToUpper(strings.TrimSpace(v))
		if trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	return out, nil
}

func parseClockValue(value string) (time.Time, error) {
	layouts := []string{"15:04:05", "15:04"}
	trimmed := strings.TrimSpace(value)
	for _, layout := range layouts {
		tm, err := time.Parse(layout, trimmed)
		if err == nil {
			return tm, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time-of-day %q", value)
}

func logSuppression(
	ctx context.Context,
	tx *sqlx.Tx,
	notificationID *string,
	triggerCode string,
	recipient string,
	caseID *string,
	reason model.NotificationSuppressionReason,
) error {
	if tx == nil {
		return fmt.Errorf("logSuppression: tx is nil")
	}
	var notifArg interface{}
	if notificationID != nil && strings.TrimSpace(*notificationID) != "" {
		notifArg = strings.TrimSpace(*notificationID)
	}
	var caseArg interface{}
	if caseID != nil && strings.TrimSpace(*caseID) != "" {
		caseArg = strings.TrimSpace(*caseID)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO notification_suppression_log (
			notification_id,
			trigger_code,
			recipient,
			case_id,
			suppressed_at,
			reason,
			created_at
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4::uuid,
			now(),
			$5,
			now()
		)
	`, notifArg, triggerCode, recipient, caseArg, string(reason))
	if err != nil {
		return fmt.Errorf("logSuppression: %w", err)
	}
	return nil
}

func getNextQuietHoursScheduleTx(ctx context.Context, tx *sqlx.Tx, recipient, channel string, now time.Time) (time.Time, bool, error) {
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
	return NextQuietHoursEnd(now, row.QuietHoursStart.String, row.QuietHoursEnd.String, row.QuietHoursTimezone.String)
}
