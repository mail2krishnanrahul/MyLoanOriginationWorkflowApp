-- notifications_seed.sql
-- Seeds default templates and triggers for correspondence & notifications.

INSERT INTO notification_templates (
    template_code,
    case_type_code,
    channel,
    subject_template,
    body_template,
    language_code,
    status,
    version,
    metadata
)
VALUES
(
    'CASE_CREATED',
    NULL,
    'EMAIL',
    'Your {{case_type}} Application ({{reference_number}})',
    'Dear {{borrower_name}}, your application is now {{stage_code}}.',
    'en',
    'ACTIVE',
    1,
    '{"from_address":"noreply@workflow.local"}'::jsonb
),
(
    'TASK_ASSIGNED',
    NULL,
    'IN_APP',
    'Task assigned: {{task.activity_code}}',
    'Task {{task.task_definition_code}} is assigned and ready.',
    'en',
    'ACTIVE',
    1,
    '{}'::jsonb
),
(
    'APPROVAL_REJECTED',
    NULL,
    'EMAIL',
    'Application Update: Approval Rejected',
    'Dear {{borrower_name}}, your application was rejected. Reason: {{event.reason}}',
    'en',
    'ACTIVE',
    1,
    '{"from_address":"noreply@workflow.local"}'::jsonb
),
(
    'SLA_WARNING',
    NULL,
    'IN_APP',
    'SLA Warning for {{reference_number}}',
    'Attention: SLA warning on case {{reference_number}} (stage {{stage_code}}).',
    'en',
    'ACTIVE',
    1,
    '{}'::jsonb
),
(
    'STAGE_CHANGED',
    NULL,
    'EMAIL',
    'Case Stage Changed: {{reference_number}}',
    'Your case moved to stage {{event.to_stage}}.',
    'en',
    'ACTIVE',
    1,
    '{"from_address":"noreply@workflow.local"}'::jsonb
)
ON CONFLICT (template_code) DO UPDATE
SET
    case_type_code = EXCLUDED.case_type_code,
    channel = EXCLUDED.channel,
    subject_template = EXCLUDED.subject_template,
    body_template = EXCLUDED.body_template,
    language_code = EXCLUDED.language_code,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    metadata = EXCLUDED.metadata,
    updated_at = now();

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
    is_enabled
)
VALUES
(
    'TRG_CASE_CREATED_BORROWER_EMAIL',
    NULL,
    'CASE_CREATED',
    NULL,
    'CASE_CREATED',
    'BORROWER',
    NULL,
    0,
    30,
    'NORMAL',
    TRUE
),
(
    'TRG_TASK_ASSIGNED_IN_APP',
    NULL,
    'TASK_ASSIGNED',
    NULL,
    'TASK_ASSIGNED',
    'TASK_ASSIGNEE',
    NULL,
    0,
    5,
    'HIGH',
    TRUE
),
(
    'TRG_APPROVAL_REJECTED_BORROWER_EMAIL',
    NULL,
    'APPROVAL_REJECTED',
    NULL,
    'APPROVAL_REJECTED',
    'BORROWER',
    NULL,
    0,
    60,
    'HIGH',
    TRUE
),
(
    'TRG_SLA_WARNING_CASE_OWNER',
    NULL,
    'SLA_WARNING',
    NULL,
    'SLA_WARNING',
    'CASE_OWNER',
    NULL,
    0,
    10,
    'URGENT',
    TRUE
),
(
    'TRG_STAGE_CHANGED_BORROWER_EMAIL',
    NULL,
    'CASE_STAGE_CHANGED',
    NULL,
    'STAGE_CHANGED',
    'BORROWER',
    NULL,
    0,
    15,
    'NORMAL',
    TRUE
)
ON CONFLICT (trigger_code) DO UPDATE
SET
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
    updated_at = now();

INSERT INTO user_preferences (
    user_id,
    channel,
    opt_out,
    quiet_hours_start,
    quiet_hours_end,
    quiet_hours_timezone,
    enabled_notification_types
)
SELECT
    'demo-borrower-1',
    NULL,
    FALSE,
    '22:00:00'::time,
    '07:00:00'::time,
    'UTC',
    '["CASE_CREATED","CASE_STAGE_CHANGED","APPROVAL_REJECTED"]'::jsonb
WHERE NOT EXISTS (
    SELECT 1
    FROM user_preferences
    WHERE user_id = 'demo-borrower-1'
      AND channel IS NULL
);

INSERT INTO user_preferences (
    user_id,
    channel,
    opt_out,
    quiet_hours_start,
    quiet_hours_end,
    quiet_hours_timezone,
    enabled_notification_types
)
SELECT
    'demo-ops-user',
    'IN_APP',
    FALSE,
    NULL,
    NULL,
    NULL,
    '["TASK_ASSIGNED","SLA_WARNING"]'::jsonb
WHERE NOT EXISTS (
    SELECT 1
    FROM user_preferences
    WHERE user_id = 'demo-ops-user'
      AND channel = 'IN_APP'
);
