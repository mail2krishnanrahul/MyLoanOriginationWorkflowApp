═══════════════════════════════════════════════════════════════
SYSTEM ROLE
═══════════════════════════════════════════════════════════════
You are a principal architect with 15+ years delivering enterprise
case management platforms on Pega, Appian, IBM BPM, and Camunda.
You write production Go code, not pseudocode. You never produce
placeholders, TODOs, or "implement this later" stubs. Every
function you write compiles, handles errors explicitly, and is
consistent with the existing codebase conventions below.

═══════════════════════════════════════════════════════════════
PROJECT CONTEXT — DO NOT REDEFINE THESE
═══════════════════════════════════════════════════════════════
Language      : Go (primary workflow engine)
Database      : Postgres (choreography hub / queue)
Architecture  : Choreography pattern — no central orchestrator
                commanding services; services react to events
Scale target  : 100,000 cases / 1,000,000 events per day
Style         : sqlx for DB access, structured logging (zerolog
                or slog), context propagation on all functions,
                typed enums (not raw strings), transactional
                outbox for all cross-service events

CANONICAL DOMAIN MODEL (frozen — do not alter these definitions):

  CaseType   Versioned blueprint. Defines stages (ordered),
             activities (config-defined groupings within a stage),
             and task definitions (what work exists per activity).
             A case_type has a code, version, and a JSONB config
             blob. Status: DRAFT | ACTIVE | DEPRECATED.

  Case       Runtime instance of a CaseType. Top-level parent
             entity — one loan application = one Case. May have
             child sub-Cases (e.g. CREDIT_CHECK is a child of
             HOME_LOAN with parent_case_id set). Tracks
             current_stage_code and current_stage_ordinal.

  Stage      Ordered progress marker on a Case. Stages do NOT
             perform work — they record where a Case is. Moving
             to a lower ordinal = regression (case went back in
             time). Every transition is recorded in
             case_stage_transitions with is_regression flag.

  Activity   Config-defined logical grouping of Tasks within a
             Stage. NOT a runtime entity. Not created by the
             workflow engine at runtime. Exists in case_type
             config and activity_definitions table only. Tasks
             reference their activity_code as a denormalised
             string. Grouping is derived, not stored.

  Task       Atomic unit of work. Stores everything: input_payload,
             output_payload, metadata, error_detail (all JSONB),
             status, priority, assigned_service, retry_count,
             idempotency_key, version (optimistic lock). The
             primary object that workers consume from the queue.

═══════════════════════════════════════════════════════════════
CODEBASE CONVENTIONS — MATCH THESE EXACTLY
═══════════════════════════════════════════════════════════════
- All DB functions:
    func Name(ctx context.Context, db *sqlx.DB, ...) (T, error)
- All transactional functions:
    func Name(ctx context.Context, tx *sqlx.Tx, ...) error
- Status/type fields: typed Go string enums with const blocks
- Migrations: sequential numbered files using golang-migrate
  conventions — UP and DOWN in separate files
  (e.g. 010_add_notifications.up.sql / .down.sql)
- Event publishing: always via PublishEvent(ctx, tx, event)
  inside the same transaction as the state change (outbox)
- Worker polling: SELECT FOR UPDATE SKIP LOCKED
- No N+1 queries — batch or join at the SQL layer
- Tests: table-driven, testify/assert, DB mocked with sqlmock
- Error wrapping: fmt.Errorf("functionName: %w", err)
- Time handling: all timestamps stored as timestamptz in UTC

═══════════════════════════════════════════════════════════════
CURRENT IMPLEMENTATION
═══════════════════════════════════════════════════════════════
All my DDLs are here : C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

ALL MY CURRENT GO STRUCTS AND CURRENT KEY FUNCTIONS HERE IN THE CURRENT PROJECT FOLDER YOU ARE IN.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
CORRESPONDENCE & NOTIFICATIONS
═══════════════════════════════════════════════════════════════
Sub-capabilities to implement — ALL of them, in this order:

  1. NOTIFICATION TEMPLATE MANAGEMENT
     Templates define reusable notification content with
     variable interpolation. Store in a notification_templates
     table:
       - template_code (unique, e.g. LOAN_APPROVED, DOC_REQUIRED)
       - case_type_code (nullable — global templates if null)
       - channel: EMAIL | SMS | PUSH | IN_APP | WEBHOOK
       - subject_template (nullable, used by EMAIL)
       - body_template (text with {{variable}} placeholders)
       - language_code (e.g. 'en', 'es', 'zh' — multi-language)
       - status: DRAFT | ACTIVE | DEPRECATED
       - version (integer, allows template evolution)
       - metadata JSONB (channel-specific config, e.g. email
         from_address, sms_sender_id, webhook_url)
     
     Variables are interpolated from case.metadata, task data,
     or passed-in context at render time. Provide a template
     rendering engine that uses text/template or a similar library.
     Example template:
       Subject: "Your {{case_type}} Application ({{reference_number}})"
       Body: "Dear {{borrower_name}}, your application is now
              {{stage_name}}. Next steps: {{next_steps_description}}."
     
     Templates support conditional blocks and loops using Go
     template syntax. Invalid template syntax is detected at
     template creation time, not render time.

  2. NOTIFICATION TRIGGER CONFIGURATION
     Define when notifications are sent using a
     notification_triggers table:
       - trigger_code (unique)
       - case_type_code (nullable for global triggers)
       - event_type (e.g. CASE_CREATED, TASK_ASSIGNED,
         APPROVAL_REJECTED, SLA_WARNING, STAGE_CHANGED)
       - filter_expression (nullable — only fire if condition true,
         e.g. "stage_code == 'UNDERWRITING' && amount > 500000")
       - template_code FK to notification_templates
       - recipient_type: CASE_OWNER | TASK_ASSIGNEE | APPROVER |
         SUPERVISOR | BORROWER | FIXED_ADDRESS | DYNAMIC_RULE
       - recipient_value (nullable — used by FIXED_ADDRESS or
         as fallback; for DYNAMIC_RULE, stores the rule expression)
       - send_after_minutes (delay, e.g. 0 for immediate, 60 for 1hr)
       - dedupe_window_minutes (suppress duplicate notifications
         within this window to the same recipient)
       - priority: LOW | NORMAL | HIGH | URGENT
       - is_enabled boolean
     
     When an event is published, the system checks all active
     triggers matching that event_type and case_type. If the
     filter_expression evaluates true (using the same expression
     evaluator from Approval Gates), a notification_queue entry
     is created.

  3. NOTIFICATION QUEUE & DISPATCH
     All outbound notifications go through a notification_queue
     table (another outbox pattern):
       - notification_id (PK, UUID)
       - trigger_code (which trigger created this notification)
       - case_id FK (nullable)
       - task_id FK (nullable)
       - template_code
       - channel
       - recipient (email address, phone number, user_id, etc.)
       - subject (rendered from template)
       - body (rendered from template)
       - priority
       - scheduled_at (NOW() + send_after_minutes from trigger)
       - status: PENDING | SENT | FAILED | SUPPRESSED | CANCELLED
       - attempts (retry counter)
       - last_attempt_at
       - sent_at (nullable)
       - error_detail JSONB
       - created_at, updated_at
     
     A notification dispatcher service polls this queue using
     SELECT FOR UPDATE SKIP LOCKED where:
       status = PENDING
       AND scheduled_at <= NOW()
       AND attempts < max_attempts
     
     For each claimed notification, the dispatcher:
       - Renders the template with case/task data (if not pre-rendered)
       - Calls the appropriate channel adapter (email, SMS, push, webhook)
       - Records sent_at and status = SENT on success
       - Records error_detail and increments attempts on failure
       - Applies exponential backoff: next attempt =
         NOW() + (2^attempts * base_retry_interval)
     
     The dispatcher must be idempotent — use notification_id as
     the idempotency key when calling external services.

  4. MULTI-CHANNEL ADAPTERS
     Each notification channel has an adapter interface:
       
       type NotificationChannel interface {
           Send(ctx context.Context, notif Notification) error
       }
     
     Implement adapters for:
       
       EMAIL:
         Use SMTP or an email service API (e.g. SendGrid, AWS SES).
         Support HTML body with fallback plain text.
         Track open/click via webhook callbacks (optional, store in
         notification_delivery_events table).
       
       SMS:
         Use an SMS gateway API (e.g. Twilio, AWS SNS).
         Enforce message length limits (160 chars for GSM-7).
         Truncate long messages or split into multiple parts.
       
       PUSH:
         Use FCM (Firebase Cloud Messaging) or APNs for mobile push.
         Requires device token stored in user profile or case metadata.
       
       IN_APP:
         Insert into a user_notifications table that the frontend
         polls or subscribes to via WebSocket.
         Mark as unread initially, provide mark-as-read endpoint.
       
       WEBHOOK:
         HTTP POST to a URL defined in template.metadata.webhook_url.
         Send case/task data as JSON payload.
         Record response status and body in notification_queue.
       
     Each adapter must:
       - Return a typed error on failure (transient vs permanent)
       - Support timeout (ctx with deadline)
       - Log the full request/response at debug level
       - Never panic — catch and return errors

  5. DEDUPLICATION & SUPPRESSION
     Prevent notification spam using two mechanisms:
       
       DEDUPLICATION (same recipient, same trigger, short window):
         Before inserting into notification_queue, check:
           SELECT id FROM notification_queue
           WHERE recipient = $1
             AND trigger_code = $2
             AND case_id = $3
             AND created_at > NOW() - INTERVAL '{{dedupe_window_minutes}} minutes'
             AND status != 'CANCELLED'
         If a row exists, do not insert a new notification. Mark
         the would-be notification as SUPPRESSED in a
         notification_suppression_log table for audit purposes:
           suppressed_at, trigger_code, recipient, reason ('DUPLICATE')
       
       USER PREFERENCES (opt-out, do-not-disturb):
         Store user notification preferences in user_preferences table:
           - user_id
           - channel (nullable — null = all channels)
           - opt_out boolean (user has unsubscribed)
           - quiet_hours_start, quiet_hours_end (e.g. 22:00 - 07:00)
           - quiet_hours_timezone
           - enabled_notification_types JSONB array (explicit allow-list)
         
         Before sending, check:
           - If opt_out = true for this user + channel → suppress
           - If current time is within quiet_hours → delay until
             quiet_hours_end (update scheduled_at)
           - If notification_type not in enabled_notification_types
             → suppress
         
         Record suppressions in notification_suppression_log with
         reason ('OPT_OUT', 'QUIET_HOURS', 'TYPE_DISABLED').

  6. DELIVERY TRACKING & ACKNOWLEDGEMENT
     Track the full lifecycle of each notification in a
     notification_delivery_events table:
       - notification_id FK
       - event_type: QUEUED | CLAIMED | RENDERED | DISPATCHED |
         DELIVERED | OPENED | CLICKED | BOUNCED | FAILED
       - event_timestamp
       - channel_response JSONB (e.g. email service message ID,
         SMS delivery receipt, webhook HTTP status)
       - user_agent (for OPENED/CLICKED events from tracking pixels)
     
     For customer-facing notifications (to BORROWER), support
     acknowledgement tracking:
       - Add acknowledged_at timestamp to notification_queue
       - Provide an endpoint POST /notifications/{id}/acknowledge
         that sets acknowledged_at = NOW()
       - Use this for compliance ("customer was notified and acknowledged")
     
     Provide query functions:
       func GetNotificationHistory(
           ctx context.Context,
           db  *sqlx.DB,
           caseID string,
       ) ([]NotificationRecord, error)
       
       func GetDeliveryRate(
           ctx context.Context,
           db  *sqlx.DB,
           channel string,
           startDate, endDate time.Time,
       ) (DeliveryStats, error)
       // returns: total sent, delivered, failed, bounce rate

  7. RETRY & CIRCUIT BREAKER
     Handle transient failures in external notification services:
       
       RETRY LOGIC:
         - Max attempts: 5
         - Backoff: exponential with jitter (2^attempt * base + rand)
         - Base retry interval: 30 seconds
         - Permanent failure (4xx error from API): mark as FAILED
           immediately, do not retry
         - Transient failure (5xx, timeout, network error): retry
       
       CIRCUIT BREAKER (per channel):
         Track failure rate in a circuit_breaker_state table:
           - channel
           - state: CLOSED | OPEN | HALF_OPEN
           - failure_count, success_count
           - last_failure_at, opened_at, half_open_at
           - threshold_failures (e.g. 10 failures in 1 minute → open)
         
         When state = OPEN:
           - All send attempts immediately return ErrCircuitOpen
           - Mark notifications as status = FAILED with
             error_detail: "circuit breaker open"
           - After a cooldown period (e.g. 5 minutes), transition
             to HALF_OPEN
         
         When state = HALF_OPEN:
           - Allow a small number of test requests through
           - If success → transition to CLOSED
           - If failure → transition back to OPEN
         
         This prevents cascading failures and API rate limit exhaustion
         when an external service degrades.

  8. CORRESPONDENCE AUDIT LOG
     Every notification sent, suppressed, or failed is recorded
     immutably in the notification_delivery_events table (see #6).
     This serves as the audit trail.
     
     Additionally, provide a correspondence_summary view that
     aggregates:
       - Total notifications sent per case (by channel)
       - Unacknowledged borrower notifications (compliance risk)
       - Failed notification count and reasons
       - Average delivery time (queued_at to sent_at)
     
     This view is materialized and refreshed periodically for
     dashboards and compliance reports.

═══════════════════════════════════════════════════════════════
REQUIRED DELIVERABLES — PRODUCE ALL IN THIS ORDER
═══════════════════════════════════════════════════════════════

## 1. GAP ANALYSIS (this capability only)

A table with columns:
  Sub-capability | Already Have | What Is Missing | Risk If Skipped

Be specific — reference actual column and function names
from the pasted implementation above.

## 2. DATA MODEL & SCHEMA MIGRATIONS

For each sub-capability requiring schema changes, produce:

  -- FILE: 010_notifications.up.sql
  [DDL — CREATE TABLE, ALTER TABLE, CREATE INDEX]
  [Include detailed column comments explaining purpose]

  -- FILE: 010_notifications.down.sql
  [Full rollback DDL]

New tables to define:
  - notification_templates
    (id, template_code UNIQUE, case_type_code, channel, subject_template,
     body_template, language_code, status, version, metadata JSONB,
     created_at, updated_at)
  
  - notification_triggers
    (id, trigger_code UNIQUE, case_type_code, event_type,
     filter_expression, template_code FK, recipient_type,
     recipient_value, send_after_minutes, dedupe_window_minutes,
     priority, is_enabled, created_at, updated_at)
  
  - notification_queue
    (id UUID PK, trigger_code, case_id FK, task_id FK, template_code,
     channel, recipient, subject, body, priority, scheduled_at,
     status, attempts, last_attempt_at, sent_at, error_detail JSONB,
     acknowledged_at, created_at, updated_at)
  
  - notification_delivery_events
    (id, notification_id FK, event_type, event_timestamp,
     channel_response JSONB, user_agent)
  
  - notification_suppression_log
    (id, notification_id, trigger_code, recipient, case_id,
     suppressed_at, reason, created_at)
  
  - user_preferences
    (id, user_id UNIQUE, channel, opt_out, quiet_hours_start,
     quiet_hours_end, quiet_hours_timezone,
     enabled_notification_types JSONB, created_at, updated_at)
  
  - circuit_breaker_state
    (channel PRIMARY KEY, state, failure_count, success_count,
     last_failure_at, opened_at, half_open_at, threshold_failures,
     cooldown_seconds, updated_at)

Indexes to create:
  - notification_queue: (status, scheduled_at, priority DESC)
    for dispatcher polling
  - notification_queue: (recipient, trigger_code, case_id, created_at)
    for deduplication check
  - notification_delivery_events: (notification_id, event_type)
  - notification_suppression_log: (case_id, suppressed_at)

Rules:
  - Every timestamp is timestamptz in UTC
  - Text enums have CHECK constraints
  - Templates with invalid syntax are rejected at insert time
    (add a trigger or check constraint that validates template)
  - notification_queue.id is UUID for distributed idempotency

After the DDL, produce the corresponding Go structs for
every new table with db and json tags. Use typed enums.

## 3. TEMPLATE RENDERING ENGINE

Define the template engine interface and implementation:
```go
  // TemplateRenderer renders notification templates with variable
  // interpolation using Go's text/template engine.
  type TemplateRenderer struct {
      funcMap template.FuncMap
  }

  // Render parses and executes a template with the given context.
  func (r *TemplateRenderer) Render(
      ctx          context.Context,
      templateText string,
      context      map[string]interface{},
  ) (string, error)

  // ValidateTemplate checks if a template is syntactically valid
  // without executing it. Used at template creation time.
  func (r *TemplateRenderer) ValidateTemplate(
      templateText string,
  ) error
```

Produce full implementations with:
  - Custom template functions: formatDate, formatCurrency,
    toUpper, truncate
  - Safe HTML escaping for email body templates
  - Error handling for missing variables (return error, do not panic)

Include unit tests with table-driven cases:
  - Simple variable substitution
  - Conditional blocks ({{if .approved}}Approved{{else}}Denied{{end}})
  - Loops ({{range .documents}}{{.name}}{{end}})
  - Custom functions ({{formatCurrency .amount}})
  - Invalid syntax (should return validation error)
  - Missing variable (should return render error)

## 4. GO IMPLEMENTATION

For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums**
```go
  // Notification-related enums, config structs, request/response types
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling
```

  **Event published**
  Notifications are triggered by existing events (CASE_CREATED,
  TASK_ASSIGNED, etc.), not new event types. However, define
  internal events for the notification system:
    NOTIFICATION_QUEUED, NOTIFICATION_SENT, NOTIFICATION_FAILED,
    NOTIFICATION_SUPPRESSED, CIRCUIT_BREAKER_OPENED

  **Integration point**
  Show where this hooks into:
    - HandleEvent: after processing any event, check triggers
      and queue notifications
    - Dispatcher service: separate background service that polls
      notification_queue

## 5. NOTIFICATION CHANNEL ADAPTERS

Define the interface:
```go
  type NotificationChannel interface {
      // Name returns the channel identifier (EMAIL, SMS, etc.)
      Name() string

      // Send dispatches the notification via this channel.
      // Returns error on failure (transient or permanent).
      Send(ctx context.Context, notif Notification) error

      // IsTransientError determines if an error is retryable.
      IsTransientError(err error) bool
  }
```

Produce complete implementations for at least three channels:

  ### EmailChannel (using SMTP or mock API)
```go
  type EmailChannel struct {
      smtpHost     string
      smtpPort     int
      fromAddress  string
      username     string
      password     string
      logger       *slog.Logger
  }

  func (c *EmailChannel) Send(ctx context.Context, notif Notification) error {
      // Full SMTP send implementation with TLS
      // Parse HTML body, generate plain text fallback
      // Set Message-ID header for tracking
      // Return typed error
  }
```

  ### SMSChannel (mock or Twilio)
```go
  type SMSChannel struct {
      accountSID  string
      authToken   string
      fromNumber  string
      apiClient   *http.Client
      logger      *slog.Logger
  }

  func (c *SMSChannel) Send(ctx context.Context, notif Notification) error {
      // POST to SMS gateway API
      // Truncate body to 160 chars
      // Parse API response for delivery receipt
      // Return typed error
  }
```

  ### InAppChannel
```go
  type InAppChannel struct {
      db     *sqlx.DB
      logger *slog.Logger
  }

  func (c *InAppChannel) Send(ctx context.Context, notif Notification) error {
      // INSERT into user_notifications table
      // Set is_read = false
      // This is always transactional, no external API
  }
```

Each adapter includes:
  - Full error handling (distinguish transient vs permanent)
  - Timeout enforcement via ctx
  - Structured logging of request/response
  - Unit tests with mocked HTTP client (for SMS/Email)

## 6. NOTIFICATION DISPATCHER SERVICE

Produce the complete background service:
```go
  type NotificationDispatcher struct {
      db               *sqlx.DB
      templateRenderer *TemplateRenderer
      channels         map[string]NotificationChannel
      circuitBreaker   *CircuitBreaker
      pollInterval     time.Duration
      batchSize        int
      maxRetries       int
      logger           *slog.Logger
  }

  func (d *NotificationDispatcher) Run(ctx context.Context) error {
      // Full implementation:
      // 1. Poll notification_queue (SELECT FOR UPDATE SKIP LOCKED)
      // 2. For each claimed notification:
      //    - Check circuit breaker state for channel
      //    - If circuit open, mark FAILED immediately
      //    - Render template if body not pre-rendered
      //    - Call appropriate channel.Send()
      //    - On success: status = SENT, sent_at = NOW()
      //    - On failure: increment attempts, compute next retry,
      //      update error_detail
      //    - Record delivery event
      // 3. Update circuit breaker state based on results
      // 4. Handle errors gracefully (log, continue batch)
  }

  func (d *NotificationDispatcher) Start(ctx context.Context) {
      // Runs in a loop with ticker, graceful shutdown on ctx.Done()
  }
```

Recommended poll interval: 10 seconds (lower latency than tasks).
Show arithmetic: at 100k cases/day with 3 notifications/case =
300k notifications/day = 208 notifications/minute = 2080 between
10-second polls. Batch size 500 is reasonable.

## 7. CIRCUIT BREAKER IMPLEMENTATION

Produce a full circuit breaker:
```go
  type CircuitBreaker struct {
      db                *sqlx.DB
      thresholdFailures int
      cooldownDuration  time.Duration
      halfOpenAttempts  int
      logger            *slog.Logger
  }

  // CheckState returns the current state for a channel and
  // decides whether to allow a send attempt.
  func (cb *CircuitBreaker) CheckState(
      ctx     context.Context,
      channel string,
  ) (allow bool, err error)

  // RecordSuccess updates the circuit breaker on successful send.
  func (cb *CircuitBreaker) RecordSuccess(
      ctx     context.Context,
      tx      *sqlx.Tx,
      channel string,
  ) error

  // RecordFailure updates the circuit breaker on failed send.
  func (cb *CircuitBreaker) RecordFailure(
      ctx     context.Context,
      tx      *sqlx.Tx,
      channel string,
  ) error
```

State transitions:
  CLOSED → OPEN: when failure_count >= threshold within 1 minute
  OPEN → HALF_OPEN: after cooldown_duration elapsed
  HALF_OPEN → CLOSED: after half_open_attempts consecutive successes
  HALF_OPEN → OPEN: on any failure during half-open

Include unit tests covering all state transitions with time mocking.

## 8. DEDUPLICATION & SUPPRESSION LOGIC

Produce two functions:
```go
  // CheckDuplicateNotification queries for recent identical
  // notifications within the dedupe window.
  func CheckDuplicateNotification(
      ctx               context.Context,
      db                *sqlx.DB,
      recipient         string,
      triggerCode       string,
      caseID            string,
      dedupeWindowMins  int,
  ) (isDuplicate bool, err error)

  // CheckUserPreferences evaluates opt-out, quiet hours, and
  // enabled notification types for a recipient.
  func CheckUserPreferences(
      ctx        context.Context,
      db         *sqlx.DB,
      recipient  string,
      channel    string,
      notifType  string,
  ) (suppress bool, reason string, err error)
```

Both are called before inserting into notification_queue.
If suppression occurs, log to notification_suppression_log.

Include unit tests:
  - Duplicate within window (suppress)
  - Duplicate outside window (allow)
  - User opt-out (suppress)
  - Quiet hours (delay scheduled_at)
  - Notification type not enabled (suppress)

## 9. TEST CASES

For each sub-capability, three table-driven tests:
  - Happy path
  - Edge case (render template with missing variable, send
    notification during quiet hours, duplicate notification
    exactly at dedupe window boundary, circuit breaker opens
    during batch dispatch, retry with exponential backoff,
    acknowledgement of already-acknowledged notification)
  - Failure mode (template syntax error, SMTP connection timeout,
    SMS API 4xx error, DB constraint violation on queue insert)
```go
  func Test[SubCapabilityName](t *testing.T) {
      tests := []struct {
          name    string
          setup   func(*sqlmock.Sqlmock)
          input   [InputType]
          want    [OutputType]
          wantErr bool
      }{
          // full test cases
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              // full assertions
          })
      }
  }
```

Special test for dispatcher end-to-end:
```go
  func TestNotificationDispatcherEndToEnd(t *testing.T) {
      // Insert 10 PENDING notifications in queue
      // Mock channel.Send() to succeed for 8, fail for 2
      // Run dispatcher.Run() once
      // Verify: 8 SENT, 2 status unchanged with attempts++
      // Verify delivery events created
      // Verify circuit breaker state updated
  }
```

## 10. NOTIFICATION LIFECYCLE GUARD

Produce a validation function:
```go
  func ValidateNotificationTransition(
      ctx       context.Context,
      current   NotificationStatus,
      requested NotificationStatus,
  ) error
```

Valid transitions:
  PENDING → SENT       (dispatcher on successful send)
  PENDING → FAILED     (dispatcher after max retries)
  PENDING → SUPPRESSED (before dispatch, due to deduplication/preferences)
  PENDING → CANCELLED  (user cancels case, all notifications cancelled)
  FAILED → PENDING     (manual retry by operator)

Reject:
  SENT → anything      (terminal state)
  SUPPRESSED → anything (terminal state, logged only)
  CANCELLED → anything (terminal state)
  Any backward transition

## 11. INTEGRATION CHECKLIST

  - [ ] Migrations applied and verified (run up + down + up)
  - [ ] notification_templates seeded with standard templates:
        CASE_CREATED, TASK_ASSIGNED, APPROVAL_REJECTED,
        SLA_WARNING, STAGE_CHANGED
  - [ ] notification_triggers seeded for key events
  - [ ] TemplateRenderer.ValidateTemplate tested with 100% coverage
  - [ ] HandleEvent updated to check triggers after every event
  - [ ] NotificationDispatcher registered in main.go with 10s interval
  - [ ] At least 3 channel adapters implemented and unit tested
  - [ ] Circuit breaker state transitions tested with time mocking
  - [ ] Deduplication logic tested with window edge cases
  - [ ] User preferences table seeded with test data
  - [ ] GetNotificationHistory query tested with 10k+ notifications
  - [ ] Prometheus metrics registered:
        notifications_queued_total{channel, trigger}
        notifications_sent_total{channel}
        notifications_failed_total{channel, reason}
        notifications_suppressed_total{reason}
        circuit_breaker_state{channel}
        notification_dispatch_latency_seconds{channel}
  - [ ] Alert rules defined:
        notifications_failed_total{channel="EMAIL"} > 100/hour
        circuit_breaker_state{state="OPEN"} for 10 minutes
        notifications_suppressed_total{reason="OPT_OUT"} spike
  - [ ] Load test: 10k notifications queued and dispatched in 5 minutes
  - [ ] Email delivery tracking tested with real email service
  - [ ] Correspondence audit log verified complete for sample case

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables
- All notifications go through notification_queue (outbox pattern)
- Template rendering must never panic on invalid input (return error)
- Circuit breaker state is per-channel, not global
- Deduplication window is configurable per trigger, not hardcoded
- User preferences must be checked before every send
- Delivery events are append-only, never updated
- Channel adapters must distinguish transient from permanent errors
- Dispatcher must be idempotent (safe to run multiple instances)
- Do not analyse any other capability dimension
═══════════════════════════════════════════════════════════════