-- 000023_notifications.down.sql
-- Rollback correspondence & notifications schema.

-- ---------------------------------------------------------------------------
-- 1) Drop correspondence summary materialized view
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_correspondence_summary_unack;
DROP INDEX IF EXISTS idx_correspondence_summary_case;
DROP MATERIALIZED VIEW IF EXISTS correspondence_summary;

-- ---------------------------------------------------------------------------
-- 2) Drop mutable-table triggers
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS user_notifications_updated_at ON user_notifications;
DROP TRIGGER IF EXISTS circuit_breaker_state_updated_at ON circuit_breaker_state;
DROP TRIGGER IF EXISTS user_preferences_updated_at ON user_preferences;
DROP TRIGGER IF EXISTS notification_queue_updated_at ON notification_queue;
DROP TRIGGER IF EXISTS notification_triggers_updated_at ON notification_triggers;
DROP TRIGGER IF EXISTS notification_templates_updated_at ON notification_templates;
DROP TRIGGER IF EXISTS notification_templates_validate ON notification_templates;
DROP TRIGGER IF EXISTS notification_delivery_events_no_update_delete ON notification_delivery_events;

-- ---------------------------------------------------------------------------
-- 3) Drop indexes
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_user_notifications_unread;
DROP INDEX IF EXISTS idx_user_preferences_channel;
DROP INDEX IF EXISTS uq_user_preferences_user_global;
DROP INDEX IF EXISTS uq_user_preferences_user_channel;
DROP INDEX IF EXISTS idx_notification_suppression_log_trigger_recipient;
DROP INDEX IF EXISTS idx_notification_suppression_log_case_time;
DROP INDEX IF EXISTS idx_notification_delivery_events_time;
DROP INDEX IF EXISTS idx_notification_delivery_events_notification_event;
DROP INDEX IF EXISTS idx_notification_queue_case;
DROP INDEX IF EXISTS idx_notification_queue_dedupe;
DROP INDEX IF EXISTS idx_notification_queue_poll;
DROP INDEX IF EXISTS idx_notification_triggers_match;
DROP INDEX IF EXISTS idx_notification_templates_scope;

-- ---------------------------------------------------------------------------
-- 4) Drop tables (dependency order)
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS user_notifications;
DROP TABLE IF EXISTS circuit_breaker_state;
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS notification_suppression_log;
DROP TABLE IF EXISTS notification_delivery_events;
DROP TABLE IF EXISTS notification_queue;
DROP TABLE IF EXISTS notification_triggers;
DROP TABLE IF EXISTS notification_templates;

-- ---------------------------------------------------------------------------
-- 5) Drop notification-specific functions
-- ---------------------------------------------------------------------------
DROP FUNCTION IF EXISTS trg_reject_notification_delivery_event_mutation();
DROP FUNCTION IF EXISTS trg_validate_notification_template();
DROP FUNCTION IF EXISTS validate_notification_template_syntax(TEXT);
