-- 000023_notifications.up.sql
-- Correspondence & notifications capability: template management, trigger config,
-- queue/dispatch state, suppression, preferences, delivery tracking, and
-- per-channel circuit breaker state.

-- ---------------------------------------------------------------------------
-- 0) Template syntax validator (lightweight Go-template style guard)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION validate_notification_template_syntax(template_text TEXT)
RETURNS BOOLEAN
LANGUAGE plpgsql
AS $$
DECLARE
    idx          INT := 1;
    open_pos     INT;
    close_pos    INT;
    action_text  TEXT;
    token        TEXT;
    remaining    TEXT;
    block_depth  INT := 0;
BEGIN
    IF template_text IS NULL OR btrim(template_text) = '' THEN
        RETURN TRUE;
    END IF;

    LOOP
        remaining := substring(template_text FROM idx);
        open_pos := strpos(remaining, '{{');
        IF open_pos = 0 THEN
            EXIT;
        END IF;
        open_pos := open_pos + idx - 1;

        close_pos := strpos(substring(template_text FROM open_pos + 2), '}}');
        IF close_pos = 0 THEN
            RETURN FALSE;
        END IF;
        close_pos := close_pos + open_pos + 1;

        action_text := btrim(substring(template_text FROM open_pos + 2 FOR close_pos - open_pos - 2));
        action_text := btrim(action_text, '- ');
        IF action_text = '' THEN
            RETURN FALSE;
        END IF;

        token := split_part(action_text, ' ', 1);
        IF token IN ('if', 'range', 'with', 'define', 'block') THEN
            block_depth := block_depth + 1;
        ELSIF token = 'end' THEN
            block_depth := block_depth - 1;
            IF block_depth < 0 THEN
                RETURN FALSE;
            END IF;
        END IF;

        idx := close_pos + 2;
    END LOOP;

    IF strpos(substring(template_text FROM idx), '}}') > 0 THEN
        RETURN FALSE;
    END IF;

    RETURN block_depth = 0;
END;
$$;

CREATE OR REPLACE FUNCTION trg_validate_notification_template()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.subject_template IS NOT NULL
       AND NOT validate_notification_template_syntax(NEW.subject_template) THEN
        RAISE EXCEPTION 'Invalid subject_template syntax for template_code=%', NEW.template_code;
    END IF;

    IF NEW.body_template IS NOT NULL
       AND NOT validate_notification_template_syntax(NEW.body_template) THEN
        RAISE EXCEPTION 'Invalid body_template syntax for template_code=%', NEW.template_code;
    END IF;

    RETURN NEW;
END;
$$;

-- ---------------------------------------------------------------------------
-- 1) Notification templates
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_templates (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    template_code       VARCHAR(100)    NOT NULL,
    case_type_code      VARCHAR(100),
    channel             VARCHAR(20)     NOT NULL
                            CHECK (channel IN ('EMAIL', 'SMS', 'PUSH', 'IN_APP', 'WEBHOOK')),
    subject_template    TEXT,
    body_template       TEXT            NOT NULL,
    language_code       VARCHAR(10)     NOT NULL,
    status              VARCHAR(20)     NOT NULL DEFAULT 'DRAFT'
                            CHECK (status IN ('DRAFT', 'ACTIVE', 'DEPRECATED')),
    version             INT             NOT NULL DEFAULT 1 CHECK (version > 0),
    metadata            JSONB           NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT uq_notification_templates_code UNIQUE (template_code),
    CONSTRAINT chk_notification_templates_language_code CHECK (language_code ~ '^[a-z]{2}(-[A-Za-z0-9]+)?$')
);

COMMENT ON TABLE notification_templates IS
'Reusable channel templates. Template text supports Go-style interpolation syntax ({{ }}) and control blocks.';
COMMENT ON COLUMN notification_templates.template_code IS
'Business-stable identifier (for example LOAN_APPROVED, DOC_REQUIRED).';
COMMENT ON COLUMN notification_templates.case_type_code IS
'Optional case type scope. NULL means globally reusable across case types.';
COMMENT ON COLUMN notification_templates.subject_template IS
'Optional subject template, used primarily by EMAIL channels.';
COMMENT ON COLUMN notification_templates.body_template IS
'Primary template body containing interpolation placeholders and optional loops/conditionals.';
COMMENT ON COLUMN notification_templates.language_code IS
'Language code used for localization (for example en, es, zh).';
COMMENT ON COLUMN notification_templates.metadata IS
'Channel-specific configuration (for example from_address, sender_id, webhook_url, custom headers).';

CREATE INDEX IF NOT EXISTS idx_notification_templates_scope
    ON notification_templates (case_type_code, status, channel);

DROP TRIGGER IF EXISTS notification_templates_updated_at ON notification_templates;
CREATE TRIGGER notification_templates_updated_at
    BEFORE UPDATE ON notification_templates
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

DROP TRIGGER IF EXISTS notification_templates_validate ON notification_templates;
CREATE TRIGGER notification_templates_validate
    BEFORE INSERT OR UPDATE OF subject_template, body_template
    ON notification_templates
    FOR EACH ROW
    EXECUTE FUNCTION trg_validate_notification_template();

-- ---------------------------------------------------------------------------
-- 2) Notification triggers
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_triggers (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_code            VARCHAR(100)    NOT NULL,
    case_type_code          VARCHAR(100),
    event_type              VARCHAR(100)    NOT NULL,
    filter_expression       TEXT,
    template_code           VARCHAR(100)    NOT NULL REFERENCES notification_templates(template_code),
    recipient_type          VARCHAR(30)     NOT NULL
                                CHECK (recipient_type IN (
                                    'CASE_OWNER',
                                    'TASK_ASSIGNEE',
                                    'APPROVER',
                                    'SUPERVISOR',
                                    'BORROWER',
                                    'FIXED_ADDRESS',
                                    'DYNAMIC_RULE'
                                )),
    recipient_value         TEXT,
    send_after_minutes      INT             NOT NULL DEFAULT 0 CHECK (send_after_minutes >= 0),
    dedupe_window_minutes   INT             NOT NULL DEFAULT 0 CHECK (dedupe_window_minutes >= 0),
    priority                VARCHAR(10)     NOT NULL DEFAULT 'NORMAL'
                                CHECK (priority IN ('LOW', 'NORMAL', 'HIGH', 'URGENT')),
    is_enabled              BOOLEAN         NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT uq_notification_triggers_code UNIQUE (trigger_code),
    CONSTRAINT chk_notification_triggers_event_type CHECK (event_type ~ '^[A-Z0-9_]+$')
);

COMMENT ON TABLE notification_triggers IS
'Rule configuration that maps domain events to outbound notifications.';
COMMENT ON COLUMN notification_triggers.filter_expression IS
'Optional boolean expression evaluated with event/case/task context before queueing.';
COMMENT ON COLUMN notification_triggers.recipient_value IS
'Used by FIXED_ADDRESS and DYNAMIC_RULE recipient types (address literal or rule expression).';
COMMENT ON COLUMN notification_triggers.send_after_minutes IS
'Scheduled delay from trigger execution to initial dispatch eligibility.';
COMMENT ON COLUMN notification_triggers.dedupe_window_minutes IS
'Window for suppressing duplicates for same recipient + trigger + case.';

CREATE INDEX IF NOT EXISTS idx_notification_triggers_match
    ON notification_triggers (event_type, is_enabled, case_type_code);

DROP TRIGGER IF EXISTS notification_triggers_updated_at ON notification_triggers;
CREATE TRIGGER notification_triggers_updated_at
    BEFORE UPDATE ON notification_triggers
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 3) Notification queue (dispatch outbox)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_queue (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger_code        VARCHAR(100)    NOT NULL REFERENCES notification_triggers(trigger_code),
    case_id             UUID            REFERENCES cases(id) ON DELETE SET NULL,
    task_id             UUID            REFERENCES tasks(id) ON DELETE SET NULL,
    template_code       VARCHAR(100)    NOT NULL REFERENCES notification_templates(template_code),
    channel             VARCHAR(20)     NOT NULL
                            CHECK (channel IN ('EMAIL', 'SMS', 'PUSH', 'IN_APP', 'WEBHOOK')),
    recipient           TEXT            NOT NULL,
    subject             TEXT,
    body                TEXT,
    priority            VARCHAR(10)     NOT NULL DEFAULT 'NORMAL'
                            CHECK (priority IN ('LOW', 'NORMAL', 'HIGH', 'URGENT')),
    scheduled_at        TIMESTAMPTZ     NOT NULL DEFAULT now(),
    status              VARCHAR(20)     NOT NULL DEFAULT 'PENDING'
                            CHECK (status IN ('PENDING', 'SENT', 'FAILED', 'SUPPRESSED', 'CANCELLED')),
    attempts            INT             NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_attempt_at     TIMESTAMPTZ,
    sent_at             TIMESTAMPTZ,
    error_detail        JSONB,
    acknowledged_at     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_notification_queue_ack_after_create
        CHECK (acknowledged_at IS NULL OR acknowledged_at >= created_at)
);

COMMENT ON TABLE notification_queue IS
'Dispatch queue for all outbound notifications. Dispatcher workers claim PENDING rows using FOR UPDATE SKIP LOCKED.';
COMMENT ON COLUMN notification_queue.id IS
'Stable distributed idempotency key used by channel adapters for external send deduplication.';
COMMENT ON COLUMN notification_queue.scheduled_at IS
'Eligibility timestamp (UTC) for dispatch. Includes trigger delay and quiet-hours adjustments.';
COMMENT ON COLUMN notification_queue.error_detail IS
'Last error payload from rendering or channel dispatch failure.';
COMMENT ON COLUMN notification_queue.acknowledged_at IS
'Borrower acknowledgement timestamp for compliance-sensitive correspondence.';

CREATE INDEX IF NOT EXISTS idx_notification_queue_poll
    ON notification_queue (status, scheduled_at, priority DESC);

CREATE INDEX IF NOT EXISTS idx_notification_queue_dedupe
    ON notification_queue (recipient, trigger_code, case_id, created_at);

CREATE INDEX IF NOT EXISTS idx_notification_queue_case
    ON notification_queue (case_id, created_at DESC)
    WHERE case_id IS NOT NULL;

DROP TRIGGER IF EXISTS notification_queue_updated_at ON notification_queue;
CREATE TRIGGER notification_queue_updated_at
    BEFORE UPDATE ON notification_queue
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) Delivery tracking events (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_delivery_events (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id     UUID            NOT NULL REFERENCES notification_queue(id) ON DELETE CASCADE,
    event_type          VARCHAR(20)     NOT NULL
                            CHECK (event_type IN (
                                'QUEUED',
                                'CLAIMED',
                                'RENDERED',
                                'DISPATCHED',
                                'DELIVERED',
                                'OPENED',
                                'CLICKED',
                                'BOUNCED',
                                'FAILED'
                            )),
    event_timestamp     TIMESTAMPTZ     NOT NULL DEFAULT now(),
    channel_response    JSONB           NOT NULL DEFAULT '{}',
    user_agent          TEXT,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now()
);

COMMENT ON TABLE notification_delivery_events IS
'Append-only lifecycle events for each notification delivery attempt and downstream callbacks.';
COMMENT ON COLUMN notification_delivery_events.channel_response IS
'Raw/normalized provider response payload (message id, HTTP status, receipts, etc.).';

CREATE INDEX IF NOT EXISTS idx_notification_delivery_events_notification_event
    ON notification_delivery_events (notification_id, event_type);

CREATE INDEX IF NOT EXISTS idx_notification_delivery_events_time
    ON notification_delivery_events (event_timestamp DESC);

CREATE OR REPLACE FUNCTION trg_reject_notification_delivery_event_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'notification_delivery_events is append-only';
END;
$$;

DROP TRIGGER IF EXISTS notification_delivery_events_no_update_delete ON notification_delivery_events;
CREATE TRIGGER notification_delivery_events_no_update_delete
    BEFORE UPDATE OR DELETE ON notification_delivery_events
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_notification_delivery_event_mutation();

-- ---------------------------------------------------------------------------
-- 5) Suppression audit log
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notification_suppression_log (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id     UUID            REFERENCES notification_queue(id) ON DELETE SET NULL,
    trigger_code        VARCHAR(100)    NOT NULL,
    recipient           TEXT            NOT NULL,
    case_id             UUID            REFERENCES cases(id) ON DELETE SET NULL,
    suppressed_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    reason              VARCHAR(30)     NOT NULL
                            CHECK (reason IN ('DUPLICATE', 'OPT_OUT', 'QUIET_HOURS', 'TYPE_DISABLED')),
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now()
);

COMMENT ON TABLE notification_suppression_log IS
'Audit log for notification suppressions and delays to prevent spam and enforce user preferences.';
COMMENT ON COLUMN notification_suppression_log.reason IS
'Suppression reason code (dedupe, opt-out, quiet-hours, or disabled type).';

CREATE INDEX IF NOT EXISTS idx_notification_suppression_log_case_time
    ON notification_suppression_log (case_id, suppressed_at);

CREATE INDEX IF NOT EXISTS idx_notification_suppression_log_trigger_recipient
    ON notification_suppression_log (trigger_code, recipient, suppressed_at DESC);

-- ---------------------------------------------------------------------------
-- 6) User preferences
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_preferences (
    id                              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                         TEXT            NOT NULL,
    channel                         VARCHAR(20)
                                        CHECK (channel IN ('EMAIL', 'SMS', 'PUSH', 'IN_APP', 'WEBHOOK')),
    opt_out                         BOOLEAN         NOT NULL DEFAULT FALSE,
    quiet_hours_start               TIME,
    quiet_hours_end                 TIME,
    quiet_hours_timezone            TEXT,
    enabled_notification_types      JSONB           NOT NULL DEFAULT '[]',
    created_at                      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_user_preferences_enabled_types_array
        CHECK (jsonb_typeof(enabled_notification_types) = 'array'),
    CONSTRAINT chk_user_preferences_quiet_hours_pair
        CHECK (
            (quiet_hours_start IS NULL AND quiet_hours_end IS NULL)
            OR (quiet_hours_start IS NOT NULL AND quiet_hours_end IS NOT NULL)
        ),
    CONSTRAINT chk_user_preferences_quiet_tz_required
        CHECK (
            (quiet_hours_start IS NULL AND quiet_hours_end IS NULL)
            OR (quiet_hours_timezone IS NOT NULL AND btrim(quiet_hours_timezone) <> '')
        )
);

COMMENT ON TABLE user_preferences IS
'Per-user notification preferences: opt-out, quiet-hours, and explicit notification type allow-list.';
COMMENT ON COLUMN user_preferences.channel IS
'Optional channel scope. NULL means preference applies to all channels.';

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_preferences_user_channel
    ON user_preferences (user_id, channel)
    WHERE channel IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_preferences_user_global
    ON user_preferences (user_id)
    WHERE channel IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_preferences_channel
    ON user_preferences (channel, user_id);

DROP TRIGGER IF EXISTS user_preferences_updated_at ON user_preferences;
CREATE TRIGGER user_preferences_updated_at
    BEFORE UPDATE ON user_preferences
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 7) Circuit breaker state (per channel)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS circuit_breaker_state (
    channel                 VARCHAR(20)     PRIMARY KEY
                                CHECK (channel IN ('EMAIL', 'SMS', 'PUSH', 'IN_APP', 'WEBHOOK')),
    state                   VARCHAR(20)     NOT NULL DEFAULT 'CLOSED'
                                CHECK (state IN ('CLOSED', 'OPEN', 'HALF_OPEN')),
    failure_count           INT             NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    success_count           INT             NOT NULL DEFAULT 0 CHECK (success_count >= 0),
    last_failure_at         TIMESTAMPTZ,
    opened_at               TIMESTAMPTZ,
    half_open_at            TIMESTAMPTZ,
    threshold_failures      INT             NOT NULL DEFAULT 10 CHECK (threshold_failures > 0),
    cooldown_seconds        INT             NOT NULL DEFAULT 300 CHECK (cooldown_seconds > 0),
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now()
);

COMMENT ON TABLE circuit_breaker_state IS
'Per-channel circuit breaker state to guard external provider degradation and limit cascading failures.';

DROP TRIGGER IF EXISTS circuit_breaker_state_updated_at ON circuit_breaker_state;
CREATE TRIGGER circuit_breaker_state_updated_at
    BEFORE UPDATE ON circuit_breaker_state
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 8) In-app user notification inbox
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_notifications (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id     UUID            NOT NULL UNIQUE REFERENCES notification_queue(id) ON DELETE CASCADE,
    user_id             TEXT            NOT NULL,
    channel             VARCHAR(20)     NOT NULL DEFAULT 'IN_APP'
                            CHECK (channel = 'IN_APP'),
    title               TEXT,
    message             TEXT            NOT NULL,
    payload             JSONB           NOT NULL DEFAULT '{}',
    is_read             BOOLEAN         NOT NULL DEFAULT FALSE,
    read_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_user_notifications_read_at
        CHECK (read_at IS NULL OR read_at >= created_at)
);

COMMENT ON TABLE user_notifications IS
'Inbox table for IN_APP channel notifications consumed by UI polling or WebSocket subscriptions.';

CREATE INDEX IF NOT EXISTS idx_user_notifications_unread
    ON user_notifications (user_id, is_read, created_at DESC);

DROP TRIGGER IF EXISTS user_notifications_updated_at ON user_notifications;
CREATE TRIGGER user_notifications_updated_at
    BEFORE UPDATE ON user_notifications
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 9) Correspondence summary materialized view
-- ---------------------------------------------------------------------------
DROP MATERIALIZED VIEW IF EXISTS correspondence_summary;

CREATE MATERIALIZED VIEW correspondence_summary AS
WITH sent_by_channel AS (
    SELECT
        q.case_id,
        q.channel,
        COUNT(*)::BIGINT AS sent_count
    FROM notification_queue q
    WHERE q.case_id IS NOT NULL
      AND q.status = 'SENT'
    GROUP BY q.case_id, q.channel
),
sent_agg AS (
    SELECT
        s.case_id,
        COALESCE(jsonb_object_agg(s.channel, s.sent_count), '{}'::jsonb) AS sent_by_channel,
        COALESCE(SUM(s.sent_count), 0)::BIGINT AS total_sent
    FROM sent_by_channel s
    GROUP BY s.case_id
),
failed_reason AS (
    SELECT
        q.case_id,
        COALESCE(q.error_detail ->> 'reason', 'UNKNOWN') AS reason,
        COUNT(*)::BIGINT AS cnt
    FROM notification_queue q
    WHERE q.case_id IS NOT NULL
      AND q.status = 'FAILED'
    GROUP BY q.case_id, COALESCE(q.error_detail ->> 'reason', 'UNKNOWN')
),
failed_agg AS (
    SELECT
        f.case_id,
        COALESCE(SUM(f.cnt), 0)::BIGINT AS failed_count,
        COALESCE(jsonb_object_agg(f.reason, f.cnt), '{}'::jsonb) AS failed_reasons
    FROM failed_reason f
    GROUP BY f.case_id
),
unack_borrower AS (
    SELECT
        q.case_id,
        COUNT(*)::BIGINT AS unacknowledged_borrower_count
    FROM notification_queue q
    JOIN notification_triggers t
      ON t.trigger_code = q.trigger_code
    WHERE q.case_id IS NOT NULL
      AND t.recipient_type = 'BORROWER'
      AND q.status = 'SENT'
      AND q.acknowledged_at IS NULL
    GROUP BY q.case_id
),
delivery_latency AS (
    SELECT
        q.case_id,
        AVG(EXTRACT(EPOCH FROM (q.sent_at - q.created_at)))::NUMERIC(12,2) AS avg_delivery_seconds
    FROM notification_queue q
    WHERE q.case_id IS NOT NULL
      AND q.status = 'SENT'
      AND q.sent_at IS NOT NULL
    GROUP BY q.case_id
)
SELECT
    c.id::UUID AS case_id,
    COALESCE(sa.total_sent, 0)::BIGINT AS total_sent,
    COALESCE(sa.sent_by_channel, '{}'::jsonb) AS sent_by_channel,
    COALESCE(ub.unacknowledged_borrower_count, 0)::BIGINT AS unacknowledged_borrower_count,
    COALESCE(fa.failed_count, 0)::BIGINT AS failed_count,
    COALESCE(fa.failed_reasons, '{}'::jsonb) AS failed_reasons,
    dl.avg_delivery_seconds,
    now()::TIMESTAMPTZ AS refreshed_at
FROM cases c
LEFT JOIN sent_agg sa ON sa.case_id = c.id
LEFT JOIN unack_borrower ub ON ub.case_id = c.id
LEFT JOIN failed_agg fa ON fa.case_id = c.id
LEFT JOIN delivery_latency dl ON dl.case_id = c.id;

COMMENT ON MATERIALIZED VIEW correspondence_summary IS
'Case-level correspondence dashboard aggregate: sent volume by channel, unacknowledged borrower notifications, failures, and average delivery latency.';

CREATE UNIQUE INDEX IF NOT EXISTS idx_correspondence_summary_case
    ON correspondence_summary (case_id);

CREATE INDEX IF NOT EXISTS idx_correspondence_summary_unack
    ON correspondence_summary (unacknowledged_borrower_count DESC);
