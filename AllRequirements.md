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
- All DB functions: func Name(ctx context.Context, db *sqlx.DB, ...) (T, error)
- All transactional functions: func Name(ctx context.Context, tx *sqlx.Tx, ...) error
- Status/type fields: typed Go string enums with const blocks
- Migrations: sequential numbered files (e.g. 004_add_case_suspension.sql)
  using golang-migrate conventions — UP and DOWN in separate files
- Event publishing: always via PublishEvent(ctx, tx, event) inside the
  same transaction as the state change (outbox guarantee)
- Worker polling: SELECT FOR UPDATE SKIP LOCKED
- No N+1 queries — batch or join at the SQL layer
- Tests: table-driven, use testify/assert, mock the DB with sqlmock
- Error wrapping: fmt.Errorf("functionName: %w", err)

═══════════════════════════════════════════════════════════════
CURRENT IMPLEMENTATION
═══════════════════════════════════════════════════════════════
All my DDLs are here : C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

ALL MY CURRENT GO STRUCTS AND CURRENT KEY FUNCTIONS HERE IN THE CURRENT PROJECT FOLDER YOU ARE IN.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW  ◄── CHANGE THIS BLOCK EACH ITERATION
═══════════════════════════════════════════════════════════════
CASE LIFECYCLE MANAGEMENT

Sub-capabilities to implement (all of them, in this order):
  1. Case cloning          — duplicate a case and all its tasks
                             into a new case with CLONED status
                             and a reference to the source case
  2. Case suspension       — freeze all activity on a case with
                             a reason and an optional resume_at
                             timestamp; tasks pause, clock stops
  3. Case resumption       — lift suspension, restart task clock,
                             publish CASE_RESUMED event
  4. Case withdrawal       — applicant-initiated cancel; requires
                             a reason; terminal state; notify all
                             open task holders
  5. Case archival         — move completed/withdrawn cases older
                             than a configurable TTL to an
                             cases_archive table without data loss
  6. Case expiry           — auto-close cases that breach a TTL
                             defined on the case_type config;
                             runs as a background sweep job
  7. Emergency manual close — operator-initiated force-close with
                              mandatory reason, supervisor_id,
                              and 4-eyes confirmation token

═══════════════════════════════════════════════════════════════
REQUIRED DELIVERABLES — PRODUCE ALL OF THESE, IN THIS ORDER
═══════════════════════════════════════════════════════════════

## 1. GAP ANALYSIS (this capability only)
A table with columns:
  Sub-capability | Already Have | What Is Missing | Risk If Not Built
One row per sub-capability listed above.
Be specific — reference actual function or column names from my code.

## 2. SCHEMA MIGRATIONS
One migration file per sub-capability that requires schema changes.
Format each as:

  -- FILE: 006_case_suspension.up.sql
  [DDL here — ALTER TABLE, CREATE TABLE, CREATE INDEX, etc.]

  -- FILE: 006_case_suspension.down.sql
  [Rollback DDL]

Rules:
  - Backward-compatible only — no column drops, no renames
  - Every new status value must be added to the existing enum
    OR converted to a text column with a CHECK constraint
  - Indexes on any column used in WHERE or ORDER BY clauses
    on tables expected to exceed 1M rows

## 3. GO IMPLEMENTATION
For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums** (if any)
```go
  // all new types here
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling
  // consistent with codebase conventions above
```

  **Event published** — state the event_type string constant
  and the payload shape as a Go struct

  **Integration point** — exactly where in the existing
  HandleEvent / orchestrator / worker loop this hooks in;
  show the diff or the updated case statement

## 4. TEST CASES
For each sub-capability, three table-driven test cases:
  - Happy path
  - Edge case (the non-obvious one — e.g. suspend an already
    suspended case, clone a case with failed tasks)
  - Failure / error mode (DB error, constraint violation,
    invalid state transition)

Format:
```go
  func TestCaseSuspension(t *testing.T) {
      tests := []struct{ ... }{ ... }
      // full test bodies, not stubs
  }
```

## 5. STATE TRANSITION GUARD
Produce a single Go function:

  func ValidateLifecycleTransition(
      ctx context.Context,
      current CaseStatus,
      requested CaseStatus,
      initiatedBy string,
  ) error

that encodes ALL valid and invalid state transitions for
Case across this entire capability dimension as an explicit
allow-list. Reject anything not on the list with a typed
error that includes the from-state, to-state, and reason.

## 6. INTEGRATION CHECKLIST
A markdown checklist of everything that must be wired up
before this capability is safe to deploy to production:
  - [ ] Migration applied and verified
  - [ ] Feature flag added (if applicable)
  - [ ] Existing HandleEvent updated
  - [ ] Background jobs registered (for sweep/expiry)
  - [ ] Monitoring alert defined (metric name + threshold)
  - [ ] Load test scenario written

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to the tasks, cases, or events tables
- No new query may do a sequential scan on tables > 10M rows
- All writes use the transactional outbox — no direct publishes
- Every new function accepts ctx as first parameter
- No raw string statuses — typed enums only
- Migrations must have a valid DOWN file
- Do not analyse any other capability dimension in this response
═══════════════════════════════════════════════════════════════


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
  (e.g. 007_add_workbaskets.up.sql / 007_add_workbaskets.down.sql)
- Event publishing: always via PublishEvent(ctx, tx, event)
  inside the same transaction as the state change (outbox guarantee)
- Worker polling: SELECT FOR UPDATE SKIP LOCKED
- No N+1 queries — batch or join at the SQL layer
- Tests: table-driven, testify/assert, DB mocked with sqlmock
- Error wrapping: fmt.Errorf("functionName: %w", err)

═══════════════════════════════════════════════════════════════
CURRENT IMPLEMENTATION
═══════════════════════════════════════════════════════════════
All my DDLs are here : C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

ALL MY CURRENT GO STRUCTS AND CURRENT KEY FUNCTIONS HERE IN THE CURRENT PROJECT FOLDER YOU ARE IN.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
WORK ASSIGNMENT & ROUTING
═══════════════════════════════════════════════════════════════
Sub-capabilities to implement — ALL of them, in this order:

  1. SKILL-BASED ROUTING
     Tasks declare required skills in their task_definition
     config (e.g. CREDIT_ANALYSIS, KYC_REVIEW, FRAUD_CHECK).
     Workers/users declare possessed skills with proficiency
     levels (BEGINNER | COMPETENT | EXPERT). Assignment engine
     matches task required skills against worker skill profiles.
     Minimum proficiency thresholds are configurable per task
     definition. A task with multiple required skills must find
     a worker who satisfies ALL of them at or above threshold.

  2. WORKBASKETS
     A workbasket is a named team-level queue. Tasks can be
     assigned to a workbasket rather than a specific individual.
     Workers pull from workbaskets they are members of.
     A workbasket has a type: GENERAL | SPECIALIST | ESCALATION.
     Tasks in an ESCALATION workbasket skip normal priority
     ordering and are always served first. Workbasket membership
     is time-bounded (e.g. on-call rotations). A task may be in
     at most one workbasket at a time. Pulling from a workbasket
     claims the task and removes it from the basket.

  3. DELEGATION & REASSIGNMENT
     Any assigned task can be delegated to another worker or
     workbasket by the current assignee or a supervisor.
     Delegation records: from_assignee, to_assignee, reason,
     delegated_at, delegated_by, delegation_type (MANUAL |
     AUTO_ESCALATION | OUT_OF_OFFICE). A full delegation chain
     must be preserved — not just the current holder. Reassignment
     by a supervisor is distinct from delegation by the assignee
     and must be recorded separately. Neither operation resets
     the task SLA clock — time already elapsed is preserved.

  4. OUT-OF-OFFICE & CAPACITY MANAGEMENT
     Workers declare availability windows (available_from,
     available_until, timezone). When a worker is unavailable,
     tasks assigned to them are automatically redistributed to
     their configured delegate or back to the originating
     workbasket. Worker capacity is expressed as max_concurrent_tasks.
     The assignment engine must never assign a task to a worker
     who is at capacity. A capacity sweep job runs periodically
     to detect overloaded workers and rebalance.

  5. SLA-AWARE URGENCY ESCALATION
     Each task has a due_at derived from the SLA defined on its
     task_definition in the case_type config. As due_at approaches,
     the task priority must be automatically promoted:
       > 80% of SLA elapsed  → promote to HIGH
       > 95% of SLA elapsed  → promote to CRITICAL and move to
                               ESCALATION workbasket
       Breached (past due_at) → publish TASK_SLA_BREACHED event,
                               assign to supervisor workbasket,
                               record breach in sla_breach_log
     The promotion sweep runs as a background job. Priority
     promotion is one-way — a promoted task never demotes back.
     Breach log must capture: task_id, case_id, original_due_at,
     breach_detected_at, assignee_at_breach, elapsed_percentage.

  6. LOAD-BALANCED ASSIGNMENT
     When the assignment engine selects a worker from a pool of
     eligible candidates (skill-matched, available, under capacity),
     it must apply a load-balancing strategy configurable per
     workbasket: ROUND_ROBIN | LEAST_LOADED | SKILL_SCORE.
     LEAST_LOADED picks the eligible worker with fewest
     IN_PROGRESS tasks. SKILL_SCORE picks the worker whose
     skill proficiency best matches the task's requirements
     (highest aggregate proficiency score across required skills).
     ROUND_ROBIN distributes evenly using a persistent cursor
     stored on the workbasket row.

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

  -- FILE: 007_workbaskets.up.sql
  [DDL — CREATE TABLE, ALTER TABLE, CREATE INDEX]

  -- FILE: 007_workbaskets.down.sql
  [Full rollback DDL]

Rules:
  - Backward-compatible only — no column drops, no renames
  - Text columns for status/type with CHECK constraints;
    document the allowed values in a comment on the column
  - Every column used in WHERE or ORDER BY on tables expected
    to exceed 1M rows must have an index
  - Foreign keys must have indexes on the referencing column
  - Include a brief comment on each table explaining its role

After the DDL, produce the corresponding Go structs for
every new table. Match field names to column names using
db struct tags. Include json tags. Use typed enums.

## 3. ASSIGNMENT ENGINE INTERFACE

Before writing any implementation, define the core interface:
```go
  // AssignmentEngine decides who receives a task.
  // Implementations are swappable per workbasket strategy.
  type AssignmentEngine interface {
      // FindCandidate returns the best worker ID for the task,
      // or ErrNoEligibleWorker if none qualify.
      FindCandidate(
          ctx context.Context,
          tx  *sqlx.Tx,
          task Task,
      ) (workerID string, err error)
  }
```

Then produce three concrete implementations:
  - RoundRobinAssignmentEngine
  - LeastLoadedAssignmentEngine
  - SkillScoreAssignmentEngine

Each must satisfy the interface. Show the constructor for each.

## 4. GO IMPLEMENTATION

For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums**
```go
  // typed enums, const blocks, new request/response structs
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling,
  // matches codebase conventions
```

  **Event published**
  State the event_type string constant and define the
  event payload as a named Go struct with json tags.
  Example events to define:
    TASK_ASSIGNED, TASK_DELEGATED, TASK_REASSIGNED,
    TASK_SLA_WARNING, TASK_SLA_BREACHED,
    WORKBASKET_TASK_CLAIMED, WORKER_CAPACITY_EXCEEDED

  **Integration point**
  Show exactly where this hooks into the existing
  HandleEvent switch / worker loop. Provide the specific
  case branch or function call to add — not a description,
  actual code.

## 5. BACKGROUND SWEEP JOBS

For each job that runs on a schedule, produce:

  ### [Job Name] (e.g. SLA Urgency Promotion Sweep)
```go
  // Full job implementation as a Go struct with:
  //   - Run(ctx context.Context) error
  //   - a configurable interval
  //   - structured logging on every action taken
  //   - idempotency — safe to run overlapping if a run stalls
  //   - metrics counter incremented on each promotion/breach
```

  State the recommended polling interval and justify it
  against the 100k cases / 1M events per day scale target.

## 6. TEST CASES

For each sub-capability, three table-driven tests:
  - Happy path
  - Edge case (the non-obvious scenario — e.g. claim from
    workbasket when worker is at max capacity, delegate a
    task that is already delegated, SLA promotion when
    due_at has already passed before the sweep runs)
  - Failure mode (DB error, constraint violation, no
    eligible worker found, worker unavailable mid-assignment)
```go
  func Test[SubCapabilityName](t *testing.T) {
      tests := []struct {
          name    string
          setup   func(*sqlmock.Sqlmock)
          input   [InputType]
          want    [OutputType]
          wantErr bool
      }{
          // full test cases — no stubs
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              // full assertion bodies
          })
      }
  }
```

## 7. ASSIGNMENT STATE TRANSITION GUARD

Produce a single function that encodes ALL valid and invalid
assignment state transitions as an explicit allow-list:
```go
  func ValidateAssignmentTransition(
      ctx         context.Context,
      current     AssignmentState,
      requested   AssignmentState,
      initiatedBy string,
      role        ActorRole,
  ) error
```

Valid transitions to encode:
  UNASSIGNED      → WORKBASKET_QUEUED  (system)
  UNASSIGNED      → ASSIGNED           (system, supervisor)
  WORKBASKET_QUEUED → ASSIGNED         (worker self-claim, system)
  ASSIGNED        → DELEGATED          (assignee, supervisor)
  ASSIGNED        → REASSIGNED         (supervisor only)
  DELEGATED       → ASSIGNED           (new assignee accepts)
  ASSIGNED        → WORKBASKET_QUEUED  (supervisor return to basket)
  Any → UNASSIGNED                     (supervisor only, with reason)

Reject anything not on the list with a typed sentinel error
that includes from-state, to-state, initiator, and reason.

## 8. INTEGRATION CHECKLIST

  - [ ] Migrations applied and verified against a test DB
  - [ ] AssignmentEngine wired into ClaimTasks / worker loop
  - [ ] SLA sweep job registered in main.go with graceful shutdown
  - [ ] Capacity sweep job registered with configurable interval
  - [ ] OOO redistribution job registered
  - [ ] New event types added to EventType const block
  - [ ] HandleEvent updated with new event case branches
  - [ ] Workbasket membership seeded for at least one test basket
  - [ ] Worker skill profiles seeded for integration tests
  - [ ] Prometheus metrics counters registered:
        tasks_assigned_total, tasks_delegated_total,
        tasks_sla_breached_total, workbasket_depth_gauge
  - [ ] Alert rule defined: workbasket_depth_gauge > [threshold]
  - [ ] Load test scenario covers 10k concurrent task claims

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables
- No new query may do a sequential scan on tables > 10M rows
- All state-changing writes use the transactional outbox
- Every new function accepts ctx as first parameter
- No raw string statuses — typed enums only
- Every migration must have a valid DOWN file
- Do not analyse any other capability dimension
- Priority promotion is strictly one-way — never demote
- The assignment engine must be stateless and swappable
═══════════════════════════════════════════════════════════════

═══════════════════════════════════════════════════════════════
SYSTEM ROLE
═══════════════════════════════════════════════════════════════
You are a senior engineering reviewer with deep expertise in:
  - Production Go systems at high throughput (1M+ events/day)
  - Postgres query optimisation and schema design
  - Enterprise case management (Pega, Appian, IBM BPM, Camunda)
  - Distributed systems failure modes and choreography patterns

You have been asked to perform a cold review of an implementation
produced by a different AI model (Gemini 2.0 Pro). Your job is
not to be diplomatic — your job is to find every gap, assumption,
anti-pattern, and production risk before this code ships.

You are reviewing TWO capability dimensions that were implemented
together:
  - CASE LIFECYCLE MANAGEMENT
  - WORK ASSIGNMENT & ROUTING

═══════════════════════════════════════════════════════════════
PROJECT CONTEXT — DO NOT REDEFINE THESE
═══════════════════════════════════════════════════════════════
Language      : Go (primary workflow engine)
Database      : Postgres (choreography hub / queue)
Architecture  : Choreography pattern — services react to events,
                no central orchestrator commanding services
Scale target  : 100,000 cases / 1,000,000 events per day
Style         : sqlx, zerolog/slog, context on all functions,
                typed enums, transactional outbox pattern

CANONICAL DOMAIN MODEL (frozen):

  CaseType   Versioned blueprint. Stages, activities, task
             definitions in JSONB config. Status: DRAFT |
             ACTIVE | DEPRECATED.

  Case       Runtime instance of CaseType. Parent entity.
             May have child sub-Cases via parent_case_id.
             Tracks current_stage_code, current_stage_ordinal.

  Stage      Ordered progress marker. Lower-ordinal transition
             = regression. All transitions recorded with
             is_regression flag.

  Activity   Config-defined grouping of Tasks within a Stage.
             NOT a runtime entity. Derived from task.activity_code.

  Task       Atomic unit of work. Stores input_payload,
             output_payload, metadata, error_detail (JSONB),
             status, priority, assigned_service, retry_count,
             idempotency_key, version (optimistic lock).

═══════════════════════════════════════════════════════════════
IMPLEMENTATION UNDER REVIEW
═══════════════════════════════════════════════════════════════
The entire code repository can be located here C:\MyProjects\MyLoanOriginationWorkflowApp

═══════════════════════════════════════════════════════════════
REVIEW DIMENSIONS
═══════════════════════════════════════════════════════════════
Review the implementation against ALL of the following
dimensions. For each finding, state:

  SEVERITY   : BLOCKER | HIGH | MEDIUM | LOW | SUGGESTION
  LOCATION   : exact file, function, or line reference
  FINDING    : what is wrong or missing
  EVIDENCE   : quote the specific code that demonstrates
               the issue — do not describe it abstractly
  IMPACT     : what fails, degrades, or breaks at scale
  FIX        : the corrected code — not a description,
               actual production-ready Go or SQL

────────────────────────────────────────────────────────────────
DIMENSION 1 — CORRECTNESS & COMPLETENESS
────────────────────────────────────────────────────────────────
Verify that every sub-capability listed in the original
requirements was actually implemented:

Case Lifecycle Management:
  □ Case cloning (with source reference, task duplication)
  □ Case suspension (reason, resume_at, clock pause)
  □ Case resumption (clock restart, CASE_RESUMED event)
  □ Case withdrawal (terminal, reason, task holder notification)
  □ Case archival (TTL-based, lossless move to archive table)
  □ Case expiry (background sweep, auto-close on TTL breach)
  □ Emergency manual close (reason, supervisor_id, 4-eyes token)

Work Assignment & Routing:
  □ Skill-based routing (required skills, proficiency threshold)
  □ Workbaskets (team queues, ESCALATION type, membership TTL)
  □ Delegation (full chain preserved, not just current holder)
  □ Reassignment (supervisor-distinct from delegation, audited)
  □ Out-of-office / capacity management (redistribution sweep)
  □ SLA-aware urgency escalation (80% → HIGH, 95% → CRITICAL,
    breach → supervisor workbasket + sla_breach_log)
  □ Load-balanced assignment (ROUND_ROBIN, LEAST_LOADED,
    SKILL_SCORE strategies, all satisfying AssignmentEngine
    interface)

For each unchecked item, mark it BLOCKER and provide the
missing implementation.

────────────────────────────────────────────────────────────────
DIMENSION 2 — SCHEMA & DATA MODEL
────────────────────────────────────────────────────────────────
Inspect every DDL migration for:

  □ Missing indexes on columns used in WHERE, ORDER BY,
    or JOIN clauses on tables expected to exceed 1M rows
  □ Foreign keys without indexes on the referencing column
  □ Missing CHECK constraints on status/type text columns
  □ JSONB columns that should be typed columns (performance)
  □ Nullable columns that should be NOT NULL with a default
  □ Columns added without a DEFAULT that will lock the table
    during migration on a live dataset
  □ Missing updated_at triggers
  □ Tables missing a version column for optimistic locking
    where concurrent writes are expected
  □ Sequences or counters that will become hot spots at scale
  □ DOWN migrations that are destructive or incomplete
  □ Any schema that breaks backward compatibility with the
    existing cases, tasks, or events table contracts

────────────────────────────────────────────────────────────────
DIMENSION 3 — CONCURRENCY & RACE CONDITIONS
────────────────────────────────────────────────────────────────
Inspect all DB writes and task claiming logic for:

  □ Optimistic lock version checks missing on UPDATE statements
    where concurrent modification is possible
  □ SELECT FOR UPDATE SKIP LOCKED used correctly — flag any
    place where it is missing or used incorrectly
  □ Task claiming that could allow double-assignment under
    concurrent workers
  □ Workbasket pull operations that are not atomic
  □ SLA sweep and capacity sweep jobs that could run
    overlapping instances — verify idempotency
  □ Delegation chain updates that are not atomic (partial
    update leaves chain in inconsistent state)
  □ Case suspension that does not atomically pause all
    in-flight tasks in the same transaction
  □ Round-robin cursor update that is not atomic (lost update
    under concurrent assignment)

────────────────────────────────────────────────────────────────
DIMENSION 4 — TRANSACTIONAL OUTBOX COMPLIANCE
────────────────────────────────────────────────────────────────
Every state change that produces an event must publish
that event inside the same transaction as the state change.
Verify:

  □ Every state-changing function accepts a *sqlx.Tx, not
    a *sqlx.DB — flag any function that opens its own
    transaction internally when it should accept one
  □ PublishEvent is called inside the transaction before
    Commit() — not after
  □ No direct message broker / channel writes anywhere
    (Redis, Kafka, HTTP — all must go through the outbox)
  □ Event payload structs are defined as named Go types,
    not anonymous map[string]interface{}
  □ All required event types are present:
    TASK_ASSIGNED, TASK_DELEGATED, TASK_REASSIGNED,
    TASK_SLA_WARNING, TASK_SLA_BREACHED,
    WORKBASKET_TASK_CLAIMED, WORKER_CAPACITY_EXCEEDED,
    CASE_SUSPENDED, CASE_RESUMED, CASE_WITHDRAWN,
    CASE_ARCHIVED, CASE_EXPIRED, CASE_CLONED,
    CASE_EMERGENCY_CLOSED

────────────────────────────────────────────────────────────────
DIMENSION 5 — PERFORMANCE AT SCALE
────────────────────────────────────────────────────────────────
Evaluate whether the implementation will hold at
100k cases / 1M events per day:

  □ Any query doing a sequential scan on a large table
    — quote the query and provide the corrected version
    with the index DDL
  □ N+1 query patterns — identify and rewrite as a single
    batched query or JOIN
  □ Background sweep jobs with polling intervals too
    aggressive for the event volume (justify recommended
    interval with arithmetic against the scale target)
  □ Assignment engine queries loading full worker lists
    into memory rather than filtering at the SQL layer
  □ SLA escalation sweep that re-scans all open tasks
    rather than using a targeted index on due_at + status
  □ Workbasket depth queries that COUNT(*) without an
    index-only scan path
  □ Any lock contention hotspot (e.g. single row updated
    by many concurrent workers)

────────────────────────────────────────────────────────────────
DIMENSION 6 — ERROR HANDLING & RESILIENCE
────────────────────────────────────────────────────────────────
  □ All errors wrapped with context:
    fmt.Errorf("functionName: %w", err)
  □ No errors silently swallowed (log and continue without
    returning the error)
  □ Typed sentinel errors defined for domain failures:
    ErrNoEligibleWorker, ErrWorkerAtCapacity,
    ErrInvalidStateTransition, ErrCaseAlreadySuspended,
    ErrDelegationChainBroken — flag any that use errors.New
    with a plain string instead
  □ Retry logic on transient DB errors (connection loss,
    serialisation failure) — flag any missing retry wrapper
  □ Background jobs that crash on a single bad record
    rather than logging the error and continuing
  □ Missing context cancellation checks in long-running
    sweep loops (ctx.Err() checked between batches)

────────────────────────────────────────────────────────────────
DIMENSION 7 — STATE MACHINE INTEGRITY
────────────────────────────────────────────────────────────────
  □ ValidateLifecycleTransition covers ALL required
    Case status transitions — list any missing edges
  □ ValidateAssignmentTransition covers ALL required
    assignment state transitions with role enforcement —
    list any missing edges or missing role checks
  □ Both guards are called before every state change,
    not just in some paths — trace the call graph
  □ Priority promotion is strictly one-way — verify
    no code path demotes a CRITICAL task back to HIGH
  □ Suspension correctly blocks all downstream transitions
    until resumed — verify no other function bypasses
    the suspension check
  □ The delegation chain is append-only — verify no
    code path overwrites previous delegation entries

────────────────────────────────────────────────────────────────
DIMENSION 8 — TEST COVERAGE QUALITY
────────────────────────────────────────────────────────────────
  □ Tests are table-driven — flag any test that is not
  □ Every sub-capability has a test for its edge case
    and failure mode, not just the happy path
  □ Concurrent access tests exist for: task claiming,
    workbasket pull, round-robin cursor, optimistic lock
  □ SLA escalation tests use time injection
    (clock is a parameter, not time.Now() directly)
    — flag any hardcoded time.Now() in testable functions
  □ Background sweep tests verify idempotency
    (running the sweep twice produces the same result)
  □ State transition guard tests cover every reject path

═══════════════════════════════════════════════════════════════
REQUIRED OUTPUT FORMAT
═══════════════════════════════════════════════════════════════

## Executive Summary
Three sentences: overall quality verdict, most critical risk,
and one thing that was done well.

## Findings by Severity

### BLOCKER (must fix before any deployment)
For each finding:
  **[B-01] [Short title]**
  Location  : [function / file / migration]
  Finding   : [what is wrong]
  Evidence  : ```go or ```sql [quoted code]
  Impact    : [what breaks]
  Fix       : ```go or ```sql [corrected code]

### HIGH (must fix before production load)
[same format]

### MEDIUM (fix in next sprint)
[same format]

### LOW / SUGGESTION
[same format — brief, no full code fix required]

## Missing Implementation
For every sub-capability checkbox that was NOT implemented,
provide the complete production implementation using the
same format as the original capability prompts:
  - Schema migration (up + down)
  - Go structs and enums
  - Core function(s)
  - Event published
  - Integration point in HandleEvent / worker loop
  - Three test cases

## Corrected Files
For every file that has BLOCKER or HIGH findings, produce
the complete corrected file — not a diff, the full file —
so it can be dropped in directly.

## Verification Checklist
A checklist the developer can run through after applying
all fixes to confirm the implementation is production-ready:
  - [ ] All BLOCKER findings resolved
  - [ ] All migrations have been tested against a clone of
        the production schema
  - [ ] Concurrent task claim test passes under 100 goroutines
  - [ ] SLA sweep tested with time injection at 80%, 95%,
        and 101% elapsed
  - [ ] All state transition guard reject paths have tests
  - [ ] EXPLAIN ANALYZE run on every new query against
        a dataset of 10M rows
  - [ ] Background jobs verified idempotent under overlap

═══════════════════════════════════════════════════════════════
CONSTRAINTS
═══════════════════════════════════════════════════════════════
- Do not re-explain the requirements — go straight to findings
- Do not praise the implementation unless it is genuinely
  exceptional — the goal is finding problems, not validation
- Quote actual code for every finding — no abstract descriptions
- Every fix must be production-ready Go or SQL — no pseudocode
- Do not analyse any capability outside these two dimensions
═══════════════════════════════════════════════════════════════

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
  (e.g. 008_add_sla_management.up.sql / .down.sql)
- Event publishing: always via PublishEvent(ctx, tx, event)
  inside the same transaction as the state change (outbox)
- Worker polling: SELECT FOR UPDATE SKIP LOCKED
- No N+1 queries — batch or join at the SQL layer
- Tests: table-driven, testify/assert, DB mocked with sqlmock
- Error wrapping: fmt.Errorf("functionName: %w", err)
- Time handling: all timestamps stored as timestamptz in UTC;
  all duration arithmetic uses Go time.Duration, not raw integers

═══════════════════════════════════════════════════════════════
CURRENT IMPLEMENTATION
═══════════════════════════════════════════════════════════════
All my DDLs are here : C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

ALL MY CURRENT GO STRUCTS AND CURRENT KEY FUNCTIONS HERE IN THE CURRENT PROJECT FOLDER YOU ARE IN.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
SLA & DEADLINE MANAGEMENT
═══════════════════════════════════════════════════════════════
Sub-capabilities to implement — ALL of them, in this order:

  1. HIERARCHICAL SLA DEFINITION
     SLAs are defined at multiple levels in the case_type config:
       - Case level:     entire case must complete within N hours
       - Stage level:    stage must complete within N hours
       - Activity level: activity must complete within N hours
       - Task level:     task must complete within N hours
     Lower-level SLAs inherit from parent if not explicitly set.
     SLA definition includes:
       - duration_hours (decimal, e.g. 2.5 for 2h 30m)
       - warning_threshold_pct (e.g. 80 = warn at 80% elapsed)
       - critical_threshold_pct (e.g. 95 = escalate at 95%)
       - breach_action: ESCALATE_TO_SUPERVISOR | AUTO_REASSIGN |
         CREATE_EXCEPTION_CASE | NOTIFY_ONLY
     Each case/stage/activity/task stores its computed due_at
     timestamp at creation time. SLAs are immutable once set —
     changing the case_type SLA does not retroactively update
     live cases.

  2. BUSINESS CALENDAR AWARENESS
     SLA clock runs on business time, not wall time. A business
     calendar defines:
       - Working hours per day (start_time, end_time, timezone)
       - Working days (bitfield: Mon=1, Tue=2, Wed=4... Sun=64;
         store as integer, sum of active days)
       - Public holidays (table: holiday_calendar_id, date, name)
       - Tenant-specific calendars (each tenant can override)
     When computing due_at from created_at + duration_hours:
       - Skip non-working hours (clock pauses overnight)
       - Skip weekends and holidays
       - Respect timezone of the calendar, not UTC
     Provide a Go function:
       func AddBusinessHours(
           ctx context.Context,
           db  *sqlx.DB,
           start time.Time,
           duration time.Duration,
           calendarID string,
       ) (time.Time, error)
     that performs the calendar-aware calculation. This function
     must be used everywhere a due_at is computed.

  3. SLA PAUSE & RESUME
     The SLA clock can be paused and resumed without cancelling
     the SLA entirely. Use cases:
       - Task status changes to AWAITING_EXTERNAL — clock pauses
       - Task status changes back to IN_PROGRESS — clock resumes
       - Case is suspended — ALL task clocks pause
       - Case is resumed — ALL task clocks resume
     Track pause/resume in an sla_pause_log table:
       - entity_type: CASE | STAGE | ACTIVITY | TASK
       - entity_id (polymorphic reference)
       - paused_at, resumed_at (nullable)
       - pause_reason (e.g. AWAITING_EXTERNAL, CASE_SUSPENDED)
       - elapsed_before_pause (duration that had already elapsed)
     When resuming, recompute due_at as:
       due_at = NOW() + (original_duration - elapsed_before_pause)
     applying business calendar rules to the remaining duration.
     The pause log must be append-only — no updates, only inserts.

  4. WARNING & CRITICAL THRESHOLD DETECTION
     A background sweep job monitors all active cases, stages,
     activities, and tasks. For each entity with an SLA:
       elapsed_pct = (NOW() - effective_start_time) / total_duration * 100
       where effective_start_time accounts for all pauses.
     When thresholds are crossed:
       >= warning_threshold_pct (e.g. 80%) and < critical_threshold_pct
         → publish SLA_WARNING event once (idempotent, do not repeat)
         → set sla_warning_issued_at on the entity
       >= critical_threshold_pct (e.g. 95%) and not yet breached
         → publish SLA_CRITICAL event once
         → set sla_critical_issued_at on the entity
         → execute critical_action if defined (e.g. escalate priority)
     The sweep must be idempotent — use the issued_at timestamps
     to ensure each threshold triggers exactly once per entity.
     Sweep interval: recommend 5 minutes for 100k cases/day.

  5. BREACH DETECTION & BREACH LOG
     When NOW() > due_at and status is not COMPLETED/CANCELLED/SKIPPED:
       → record a breach in sla_breach_log table:
          - entity_type, entity_id
          - breach_detected_at
          - original_due_at
          - assignee_at_breach (who was responsible)
          - elapsed_time_minutes (how long it took vs SLA)
          - breach_severity: MINOR (<10% over) | MODERATE (10-30%)
            | MAJOR (30-50%) | CRITICAL (>50% over)
       → publish SLA_BREACHED event
       → execute breach_action from the SLA definition:
          - ESCALATE_TO_SUPERVISOR: reassign to supervisor workbasket
          - AUTO_REASSIGN: find another eligible worker and reassign
          - CREATE_EXCEPTION_CASE: spawn a child case of type
            EXCEPTION_HANDLING with the breach context
          - NOTIFY_ONLY: just log and publish event, no auto-action
     Breaches are recorded exactly once per entity using an
     sla_breach_detected_at timestamp on the entity itself.
     A single entity may breach multiple times if the SLA is
     reset (e.g. task fails and is retried with a new SLA).

  6. SLA REPORTING & METRICS
     Provide query functions for operational dashboards:
       - SLA compliance rate (% non-breached) by case_type, stage,
         activity, task_definition over a time window
       - Average resolution time vs SLA target (actual - target)
       - P50, P95, P99 latency to completion
       - Current breach count and trend (breaches per hour)
       - Top 10 breach-prone task definitions
       - SLA pause time distribution (how much time is spent paused)
     All reporting functions must use pre-aggregated summary tables
     or materialized views — never scan the full tasks table.
     Define an sla_metrics_summary table that is updated by the
     same sweep job that detects breaches. It stores daily rollups:
       - metric_date, case_type_code, stage_code, activity_code,
         task_definition_code
       - total_count, completed_count, breached_count
       - avg_elapsed_minutes, p95_elapsed_minutes
       - total_pause_minutes
     Reporting queries read from this summary table, not live data.

  7. SLA RESET & EXTENSION
     Two operations that modify an existing SLA:
       
       SLA RESET (supervisor only):
         Clears all existing SLA state and recomputes due_at as if
         the entity was just created. Use case: major external delay
         that invalidates the original timeline. Requires:
           - reason (mandatory text)
           - approved_by (supervisor ID)
           - new_duration_hours (if different from original)
         Records the reset in an sla_reset_log table.
       
       SLA EXTENSION (supervisor only):
         Adds additional time to the existing due_at without
         resetting the elapsed time. Use case: grant an extra
         2 hours due to complexity. Requires:
           - extension_duration_hours
           - reason
           - approved_by
         Records the extension in an sla_extension_log table.
     
     Both operations must:
       - Cancel any pending SLA_WARNING or SLA_CRITICAL events
         (set a cancelled_at timestamp on those events)
       - Recompute due_at applying business calendar rules
       - Clear sla_warning_issued_at and sla_critical_issued_at
         so thresholds can trigger again
       - Publish SLA_RESET or SLA_EXTENDED event
       - Preserve the original SLA for audit trail — do not
         overwrite, append to a history

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

  -- FILE: 008_sla_management.up.sql
  [DDL — CREATE TABLE, ALTER TABLE, CREATE INDEX]
  [Include detailed column comments explaining purpose]

  -- FILE: 008_sla_management.down.sql
  [Full rollback DDL]

New tables to define:
  - business_calendars
    (id, name, timezone, start_time, end_time, working_days_bitfield)
  - holiday_calendars
    (calendar_id FK, date, name, is_recurring)
  - sla_pause_log
    (id, entity_type, entity_id, paused_at, resumed_at,
     pause_reason, elapsed_before_pause_ms)
  - sla_breach_log
    (id, entity_type, entity_id, breach_detected_at,
     original_due_at, assignee_at_breach, elapsed_time_minutes,
     breach_severity, breach_action_taken)
  - sla_reset_log
    (id, entity_type, entity_id, reset_at, previous_due_at,
     new_due_at, new_duration_hours, reason, approved_by)
  - sla_extension_log
    (id, entity_type, entity_id, extended_at, previous_due_at,
     new_due_at, extension_duration_hours, reason, approved_by)
  - sla_metrics_summary
    (metric_date, case_type_code, stage_code, activity_code,
     task_definition_code, total_count, completed_count,
     breached_count, avg_elapsed_minutes, p95_elapsed_minutes,
     total_pause_minutes)

Columns to add to existing tables:
  - cases: case_due_at, case_sla_warning_issued_at,
    case_sla_critical_issued_at, case_sla_breach_detected_at
  - tasks: task_due_at, sla_warning_issued_at,
    sla_critical_issued_at, sla_breach_detected_at,
    effective_start_time (accounts for pauses)

Rules:
  - Every date/time column is timestamptz in UTC
  - Every index on tables > 1M rows must be partial where
    appropriate (e.g. WHERE status IN ('PENDING', 'IN_PROGRESS'))
  - Every table has created_at, updated_at with triggers
  - Text columns for enums have CHECK constraints

After the DDL, produce the corresponding Go structs for
every new table with db and json tags. Use typed enums.

## 3. BUSINESS CALENDAR ENGINE

Before any SLA calculation function, define:
```go
  // BusinessCalendar represents working hours and holidays.
  type BusinessCalendar struct {
      ID              string
      Name            string
      Timezone        string
      StartTime       string // "09:00"
      EndTime         string // "17:00"
      WorkingDays     int    // bitfield: Mon=1, Tue=2... Sun=64
      HolidayDates    []time.Time
  }

  // AddBusinessHours adds a duration to a start time,
  // skipping non-working hours, weekends, and holidays.
  func AddBusinessHours(
      ctx context.Context,
      db  *sqlx.DB,
      start time.Time,
      duration time.Duration,
      calendarID string,
  ) (time.Time, error)

  // BusinessHoursElapsed calculates how many business hours
  // have passed between two timestamps.
  func BusinessHoursElapsed(
      ctx context.Context,
      db  *sqlx.DB,
      start time.Time,
      end   time.Time,
      calendarID string,
  ) (time.Duration, error)
```

Produce full implementations. Handle edge cases:
  - Start time is already in non-working hours → skip to next
    working hour before beginning calculation
  - Duration spans multiple days → iterate day-by-day
  - Timezone conversion when calendar is not UTC
  - Holidays that fall on weekends are not double-counted

Include unit tests with table-driven cases covering:
  - Same-day calculation (within working hours)
  - Overnight span (crosses midnight)
  - Weekend skip
  - Holiday skip
  - Multiple weeks
  - Start in non-working hours

## 4. GO IMPLEMENTATION

For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums**
```go
  // SLA-related enums, config structs, request/response types
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling
```

  **Event published**
  Define the event_type constant and payload struct:
    SLA_WARNING, SLA_CRITICAL, SLA_BREACHED,
    SLA_PAUSED, SLA_RESUMED, SLA_RESET, SLA_EXTENDED

  **Integration point**
  Show where this hooks into:
    - HandleEvent case branches for SLA_* events
    - CreateTask / CreateCase to compute initial due_at
    - Task status transition to trigger pause/resume
    - Case suspension to pause all task SLAs

## 5. SLA SWEEP JOB

Produce the complete background job:
```go
  type SLASweepJob struct {
      db              *sqlx.DB
      eventPublisher  EventPublisher
      sweepInterval   time.Duration
      batchSize       int
      logger          *slog.Logger
  }

  func (j *SLASweepJob) Run(ctx context.Context) error {
      // Full implementation:
      // 1. Load all entities with active SLAs (use index)
      // 2. For each, compute elapsed_pct accounting for pauses
      // 3. Check thresholds (warning, critical, breach)
      // 4. Publish events and update issued_at / breach_detected_at
      // 5. Execute breach_action if breached
      // 6. Update sla_metrics_summary (daily rollup)
      // 7. Handle errors gracefully (log, continue batch)
  }

  func (j *SLASweepJob) Start(ctx context.Context) {
      // Runs in a loop with ticker, graceful shutdown on ctx.Done()
  }
```

State the recommended sweep interval (5 minutes for 100k cases/day).
Show arithmetic: at 100k cases/day = 69 cases/minute = 345 cases
between sweeps, multiplied by avg 5 tasks/case = 1,725 tasks to
check per sweep. With proper indexing, a batch query scanning
<5k rows is acceptable.

## 6. SLA REPORTING QUERIES

Provide optimized SQL and Go functions for:
```go
  type SLAComplianceReport struct {
      CaseTypeCode        string
      StageCode           string
      ActivityCode        string
      TaskDefinitionCode  string
      TotalCount          int
      CompletedCount      int
      BreachedCount       int
      ComplianceRate      float64  // (completed - breached) / completed
      AvgElapsedMinutes   float64
      P95ElapsedMinutes   int
  }

  func GetSLAComplianceReport(
      ctx context.Context,
      db  *sqlx.DB,
      startDate time.Time,
      endDate   time.Time,
      filters   SLAReportFilters,
  ) ([]SLAComplianceReport, error)

  // Use sla_metrics_summary — do NOT scan tasks table directly
```

Show the SQL query that reads from sla_metrics_summary and
aggregates across the date range. Ensure it uses an index on
(metric_date, case_type_code).

## 7. TEST CASES

For each sub-capability, three table-driven tests:
  - Happy path
  - Edge case (non-obvious — e.g. pause an already-paused SLA,
    breach an SLA that was extended, calculate due_at when
    start time is on a holiday, resume after multiple pauses)
  - Failure mode (DB error, invalid calendar ID, negative
    duration, breach action fails)
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

Special test for AddBusinessHours:
```go
  func TestAddBusinessHours(t *testing.T) {
      tests := []struct {
          name         string
          start        time.Time
          duration     time.Duration
          calendarID   string
          want         time.Time
      }{
          {
              name: "same day within working hours",
              start: parseTime("2025-02-18 10:00:00 UTC"),
              duration: 3 * time.Hour,
              calendarID: "default",
              want: parseTime("2025-02-18 13:00:00 UTC"),
          },
          {
              name: "overnight span",
              start: parseTime("2025-02-18 16:00:00 UTC"),
              duration: 4 * time.Hour,
              calendarID: "default", // 09:00-17:00, Mon-Fri
              want: parseTime("2025-02-19 11:00:00 UTC"), // 1h on 18th, 3h on 19th
          },
          // add: weekend skip, holiday skip, multiple weeks
      }
      // full test body
  }
```

## 8. SLA STATE TRANSITION GUARD

Produce a validation function that enforces SLA lifecycle rules:
```go
  func ValidateSLAOperation(
      ctx       context.Context,
      operation SLAOperation,  // PAUSE | RESUME | RESET | EXTEND
      entity    SLAEntity,     // current state
      actor     Actor,         // who is performing the operation
  ) error
```

Valid transitions to encode:
  ACTIVE → PAUSED        (task status → AWAITING_EXTERNAL, case suspended)
  PAUSED → ACTIVE        (task status → IN_PROGRESS, case resumed)
  ACTIVE → RESET         (supervisor only, with reason)
  PAUSED → RESET         (supervisor only)
  ACTIVE → EXTENDED      (supervisor only, cannot extend if breached)
  Any → BREACHED         (system only, not manually triggerable)

Reject:
  - PAUSED → PAUSED      (already paused)
  - ACTIVE → RESUME      (not paused)
  - BREACHED → EXTENDED  (too late, must reset)
  - RESET by non-supervisor
  - EXTEND by non-supervisor
  - EXTEND with negative duration

## 9. INTEGRATION CHECKLIST

  - [ ] Migrations applied and verified (run up + down + up)
  - [ ] business_calendars table seeded with default calendar
  - [ ] AddBusinessHours unit tests pass with 100% coverage
  - [ ] CreateTask updated to call AddBusinessHours for due_at
  - [ ] CreateCase updated to compute case_due_at
  - [ ] Task status transition hook added to trigger pause/resume
  - [ ] Case suspension/resumption hooks all task SLA pause/resume
  - [ ] SLASweepJob registered in main.go with 5min interval
  - [ ] HandleEvent updated with SLA_* event case branches
  - [ ] sla_metrics_summary materialized view refresh scheduled
  - [ ] Prometheus metrics registered:
        sla_breaches_total{severity, case_type, stage}
        sla_warnings_total{case_type, stage}
        sla_compliance_rate{case_type, stage}
  - [ ] Alert rules defined:
        sla_breaches_total > [threshold] for 1h
        sla_compliance_rate < 0.95 for 1h
  - [ ] GetSLAComplianceReport tested against 10M row summary table
  - [ ] Load test: 1000 concurrent SLA calculations via AddBusinessHours

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables
- All SLA calculations must use business calendar, not wall time
- All timestamps stored as timestamptz in UTC
- SLA pause/resume log is append-only, never updated
- Breach log is append-only, breaches recorded exactly once
- Sweep job must be idempotent under concurrent execution
- No query may scan tasks table for reporting — use summary table
- Every SLA operation (reset, extend) requires supervisor approval
- Warning/critical thresholds trigger exactly once per entity
- Do not analyse any other capability dimension
═══════════════════════════════════════════════════════════════

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
  (e.g. 009_add_approval_gates.up.sql / .down.sql)
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
APPROVAL & DECISION GATES
═══════════════════════════════════════════════════════════════
Sub-capabilities to implement — ALL of them, in this order:

  1. APPROVAL TASK DEFINITION
     An approval is a special type of Task with additional
     metadata. In the case_type config, a task_definition can
     be marked as requires_approval: true. This creates an
     approval_gate when the task is instantiated. An approval
     gate has:
       - approval_policy: SINGLE_APPROVER | ALL_MUST_APPROVE |
         ANY_CAN_APPROVE | MAJORITY | CONSENSUS (> 66%)
       - required_approver_count: int (used by MAJORITY, ANY)
       - approver_selection: EXPLICIT_LIST | ROLE_BASED |
         REPORTING_CHAIN | DYNAMIC_RULE
       - approvers: list of user IDs or roles (if EXPLICIT_LIST)
       - authority_limit: decimal (if approval involves $$ amounts,
         e.g. only users with authority >= this amount can approve)
       - approval_timeout_hours: decimal (approval expires if not
         acted upon within this time — uses business calendar)
       - on_timeout_action: AUTO_APPROVE | AUTO_REJECT | ESCALATE
       - rejection_behavior: SEND_TO_REWORK | TERMINAL_FAIL
       - rework_target_stage_code: nullable (which stage to regress
         to if rejected and rejection_behavior = SEND_TO_REWORK)
     When a task with requires_approval: true reaches IN_PROGRESS,
     the system creates an approval_requests table entry for each
     required approver. Task cannot complete until approval policy
     is satisfied.

  2. APPROVAL CHAINS & TIERED APPROVAL
     Approval can be structured in tiers/levels. A case_type can
     define an approval_chain in config:
       approval_chain:
         - tier: 1
           approver_role: "CREDIT_ANALYST"
           authority_limit: 100000
           can_skip_if: "amount < 50000"
         - tier: 2
           approver_role: "SENIOR_MANAGER"
           authority_limit: 500000
           required_if: "amount >= 100000"
         - tier: 3
           approver_role: "DIRECTOR"
           authority_limit: null  # unlimited
           required_if: "amount >= 500000"
     The system evaluates the chain in order. Each tier spawns
     an approval_requests row. A tier blocks until its approval
     policy is satisfied, then the next tier starts. If a tier's
     can_skip_if evaluates true, that tier is skipped entirely.
     required_if determines whether a tier must run or is optional.
     Expressions are evaluated against case.metadata JSONB using
     a simple expression evaluator (support: ==, !=, <, >, <=, >=,
     && for AND, || for OR, field access via dot notation).
     Store the approval chain state in an approval_chain_state
     table: current_tier, tier_status (PENDING | APPROVED |
     REJECTED | SKIPPED), tier_started_at, tier_completed_at.

  3. DELEGATED AUTHORITY LIMITS
     Approvers have authority limits stored in a user_authority
     table: user_id, authority_type (e.g. LOAN_APPROVAL,
     CREDIT_ADJUSTMENT), max_amount, granted_by, granted_at,
     expires_at (nullable). When selecting approvers for a task,
     the system must filter by:
       - approver has the required role/skill
       - approver.max_amount >= task.approval_amount (if amount-based)
       - approver.expires_at IS NULL OR expires_at > NOW()
     If no eligible approvers are found, publish
     NO_ELIGIBLE_APPROVER event and escalate to a fallback
     supervisor role defined in the case_type config.
     Authority limits are version-controlled in an
     authority_limit_history table (append-only log of grants
     and revocations).

  4. APPROVAL REQUEST LIFECYCLE
     Each approver receives an approval_request row:
       - approval_gate_id (FK to approval_gates table)
       - approver_id (user ID)
       - tier (nullable, used in approval chains)
       - status: PENDING | APPROVED | REJECTED | EXPIRED | DELEGATED
       - decision: nullable text (approver's reason/justification)
       - evidence_refs: JSONB array of document IDs or URLs
       - decided_at, decided_by (who actually made the decision,
         may differ from approver_id if delegated)
       - expires_at (computed from approval_timeout_hours)
       - delegated_to_id (nullable, if approver delegates)
     
     Approver actions:
       APPROVE:
         - Set status = APPROVED, decision, evidence_refs, decided_at
         - Publish APPROVAL_GRANTED event
         - Check if approval_policy is now satisfied
         - If satisfied, complete the approval gate and allow
           the parent task to proceed
       
       REJECT:
         - Set status = REJECTED, decision, evidence_refs, decided_at
         - Publish APPROVAL_REJECTED event
         - Execute rejection_behavior:
           - SEND_TO_REWORK: RecordStageTransition to
             rework_target_stage_code (regression), create new
             tasks for that stage, publish CASE_SENT_TO_REWORK
           - TERMINAL_FAIL: set case status = REJECTED (terminal),
             cancel all open tasks, publish CASE_REJECTED
       
       DELEGATE:
         - Set status = DELEGATED, delegated_to_id, delegated_at
         - Create a new approval_request for the delegate with
           the same gate, preserving the delegation chain
         - Publish APPROVAL_DELEGATED event
       
       (TIMEOUT):
         - Background sweep job detects expires_at < NOW()
         - Set status = EXPIRED
         - Execute on_timeout_action from the gate definition
         - Publish APPROVAL_EXPIRED event

  5. APPROVAL POLICY EVALUATION ENGINE
     Produce a reusable function that evaluates whether an
     approval_gate is satisfied:
       
       func EvaluateApprovalPolicy(
           ctx context.Context,
           db  *sqlx.DB,
           gateID string,
       ) (satisfied bool, err error)
     
     Logic by policy:
       SINGLE_APPROVER:
         satisfied = COUNT(status=APPROVED) >= 1
       
       ALL_MUST_APPROVE:
         satisfied = COUNT(status=APPROVED) == COUNT(total approvers)
         fail fast: if COUNT(status=REJECTED) > 0 → not satisfied
       
       ANY_CAN_APPROVE:
         satisfied = COUNT(status=APPROVED) >= 1
         fail fast: if COUNT(status=REJECTED) == COUNT(total) → not satisfied
       
       MAJORITY:
         satisfied = COUNT(status=APPROVED) > COUNT(total) / 2
         fail fast: if COUNT(status=REJECTED) > COUNT(total) / 2 → not satisfied
       
       CONSENSUS (> 66%):
         satisfied = COUNT(status=APPROVED) / COUNT(total) > 0.66
         fail fast: if COUNT(status=REJECTED) makes it impossible → not satisfied
     
     This function is called after every approval/rejection
     decision. If satisfied, the gate closes and the parent task
     can proceed. If fail-fast condition is met, trigger rejection
     behavior immediately without waiting for remaining approvers.

  6. APPROVAL EXPIRY SWEEP
     A background job monitors all PENDING approval_requests and
     detects expires_at < NOW(). For each expired request:
       - Set status = EXPIRED
       - Execute on_timeout_action from the parent gate:
         - AUTO_APPROVE: treat as if approver approved
           (status = APPROVED, decided_by = 'SYSTEM_AUTO_APPROVE')
         - AUTO_REJECT: treat as rejection, trigger rejection_behavior
         - ESCALATE: reassign to the approver's supervisor or
           fallback role, reset expires_at with same timeout duration
       - Publish APPROVAL_EXPIRED event with timeout_action taken
     
     Sweep interval: recommend 1 minute (approvals are time-sensitive).
     Expiry detection must be idempotent — use expires_at timestamp
     and status to ensure each expiry is processed exactly once.

  7. APPROVAL HISTORY & AUDIT
     Every decision, delegation, and expiry is recorded immutably
     in an approval_audit_log table:
       - approval_request_id
       - event_type: REQUESTED | APPROVED | REJECTED | DELEGATED |
         EXPIRED | AUTO_APPROVED | AUTO_REJECTED | ESCALATED
       - actor_id (who performed the action, 'SYSTEM' for auto)
       - decision_text (approver's reason)
       - evidence_refs JSONB
       - previous_state, new_state (for audit trail continuity)
       - created_at
     
     This log is append-only. Provide a query function:
       func GetApprovalHistory(
           ctx context.Context,
           db  *sqlx.DB,
           caseID string,
       ) ([]ApprovalAuditEntry, error)
     that returns the full approval history for a case, ordered
     chronologically, with approver names resolved (JOIN users).
     This is used by compliance officers and auditors.

  8. REWORK LOOP IMPLEMENTATION
     When an approval is rejected with rejection_behavior =
     SEND_TO_REWORK:
       - RecordStageTransition to rework_target_stage_code
         (mark is_regression = true)
       - Cancel all tasks in the current stage that are not yet
         completed (set status = CANCELLED, reason = 'APPROVAL_REJECTED')
       - Create new task instances for the rework_target_stage
         (use the same logic as stage transition task creation)
       - Publish CASE_SENT_TO_REWORK event with:
           - case_id, from_stage, to_stage (rework target)
           - rejection_reason, rejected_by, rejected_at
       - Preserve the rejected approval_gate and all its requests
         in the database (do not delete) — mark them with
         gate_status = REJECTED_REWORK_INITIATED for audit trail
       
       On re-entry to the stage that had the approval:
       - Create a NEW approval_gate (do not reuse the old one)
       - The new gate may have different approvers if approver
         selection is DYNAMIC_RULE (re-evaluate eligibility)
     
     Rework loops can occur multiple times. Track the rework count
     on the case: rework_count integer, increment on each rework.
     If rework_count exceeds a configurable max_rework_attempts
     (defined in case_type config), automatically set case status
     to REJECTED (terminal) and publish CASE_MAX_REWORK_EXCEEDED.

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

  -- FILE: 009_approval_gates.up.sql
  [DDL — CREATE TABLE, ALTER TABLE, CREATE INDEX]
  [Include detailed column comments explaining purpose]

  -- FILE: 009_approval_gates.down.sql
  [Full rollback DDL]

New tables to define:
  - approval_gates
    (id, task_id FK, approval_policy, required_approver_count,
     approver_selection, approvers JSONB, authority_limit,
     approval_timeout_hours, on_timeout_action, rejection_behavior,
     rework_target_stage_code, gate_status, created_at, closed_at)
  
  - approval_requests
    (id, approval_gate_id FK, approver_id, tier, status, decision,
     evidence_refs JSONB, decided_at, decided_by, expires_at,
     delegated_to_id, created_at, updated_at)
  
  - approval_chain_state
    (id, case_id FK, approval_chain_definition JSONB,
     current_tier, tier_status, tier_started_at, tier_completed_at,
     chain_status, created_at, updated_at)
  
  - user_authority
    (id, user_id, authority_type, max_amount, granted_by,
     granted_at, expires_at, revoked_at, revoked_by)
  
  - authority_limit_history
    (id, user_id, authority_type, max_amount, change_type
     (GRANTED | REVOKED | MODIFIED), changed_by, changed_at,
     reason)
  
  - approval_audit_log
    (id, approval_request_id FK, event_type, actor_id,
     decision_text, evidence_refs JSONB, previous_state,
     new_state, created_at)

Columns to add to existing tables:
  - cases: rework_count (integer, default 0),
    max_rework_attempts (integer, from case_type config)
  - tasks: requires_approval (boolean, denormalized from
    task_definition), approval_gate_id (nullable FK)

Rules:
  - Every index on WHERE/ORDER BY columns for tables > 1M rows
  - Foreign keys have indexes on referencing columns
  - Text enums have CHECK constraints
  - All timestamps are timestamptz in UTC
  - Append-only tables (audit_log, history) have no updates

After the DDL, produce the corresponding Go structs for
every new table with db and json tags. Use typed enums.

## 3. EXPRESSION EVALUATOR

Before implementing approval chains, define the expression
evaluator for can_skip_if and required_if conditions:
```go
  // ExpressionEvaluator evaluates simple boolean expressions
  // against a JSONB context (case.metadata).
  type ExpressionEvaluator struct{}

  // Evaluate parses and evaluates an expression.
  // Supported: ==, !=, <, >, <=, >=, &&, ||, field access via dot
  // Example: "amount >= 100000 && risk_rating == 'HIGH'"
  func (e *ExpressionEvaluator) Evaluate(
      ctx        context.Context,
      expression string,
      context    map[string]interface{}, // from case.metadata
  ) (bool, error)
```

Produce a full implementation. Use a simple recursive descent
parser or the Expr library (github.com/expr-lang/expr) if
available. Handle type coercion (string to number, etc.).
Return typed errors for invalid expressions.

Include unit tests with table-driven cases covering:
  - Numeric comparisons
  - String equality
  - Boolean AND / OR
  - Nested field access (e.g. "borrower.credit_score > 700")
  - Invalid syntax (should return error, not panic)

## 4. GO IMPLEMENTATION

For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums**
```go
  // Approval-related enums, config structs, request/response types
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling
```

  **Event published**
  Define the event_type constant and payload struct:
    APPROVAL_GATE_CREATED, APPROVAL_REQUESTED,
    APPROVAL_GRANTED, APPROVAL_REJECTED, APPROVAL_DELEGATED,
    APPROVAL_EXPIRED, APPROVAL_GATE_SATISFIED,
    APPROVAL_GATE_FAILED, CASE_SENT_TO_REWORK,
    CASE_REJECTED, CASE_MAX_REWORK_EXCEEDED,
    NO_ELIGIBLE_APPROVER

  **Integration point**
  Show where this hooks into:
    - CreateTask: if task_definition.requires_approval, create gate
    - Task status transition to IN_PROGRESS: activate approval gate
    - HandleEvent: case branches for APPROVAL_* events
    - RecordStageTransition: handle rework regression

## 5. APPROVAL POLICY EVALUATION ENGINE

Produce the complete implementation:
```go
  type ApprovalPolicyEvaluator struct {
      db     *sqlx.DB
      logger *slog.Logger
  }

  // EvaluateApprovalPolicy checks if the gate's approval policy
  // is satisfied based on current approval_requests state.
  func (e *ApprovalPolicyEvaluator) EvaluateApprovalPolicy(
      ctx    context.Context,
      tx     *sqlx.Tx,
      gateID string,
  ) (satisfied bool, err error) {
      // Full implementation:
      // 1. Load gate with approval_policy
      // 2. Load all approval_requests for this gate
      // 3. Count APPROVED, REJECTED, PENDING by policy
      // 4. Check fail-fast conditions (early rejection)
      // 5. Return satisfied = true/false
      // 6. If satisfied or failed, update gate_status and close gate
  }

  // EvaluateApprovalChain checks if the current tier in a chain
  // is complete and advances to the next tier if ready.
  func (e *ApprovalPolicyEvaluator) EvaluateApprovalChain(
      ctx     context.Context,
      tx      *sqlx.Tx,
      chainID string,
  ) error {
      // Full implementation:
      // 1. Load chain state, get current_tier
      // 2. Evaluate expressions (can_skip_if, required_if) for tier
      // 3. If tier satisfied, advance to next tier or complete chain
      // 4. Publish tier completion event
  }
```

Include unit tests for each policy type with edge cases:
  - ALL_MUST_APPROVE with one rejection (should fail fast)
  - MAJORITY with exactly 50/50 split (not satisfied)
  - CONSENSUS with 67% approved (satisfied)
  - ANY_CAN_APPROVE with all rejected (fail fast)

## 6. APPROVAL EXPIRY SWEEP JOB

Produce the complete background job:
```go
  type ApprovalExpirySweepJob struct {
      db              *sqlx.DB
      eventPublisher  EventPublisher
      evaluator       *ApprovalPolicyEvaluator
      sweepInterval   time.Duration
      batchSize       int
      logger          *slog.Logger
  }

  func (j *ApprovalExpirySweepJob) Run(ctx context.Context) error {
      // Full implementation:
      // 1. SELECT approval_requests WHERE status=PENDING AND expires_at < NOW()
      // 2. For each expired request:
      //    - Set status = EXPIRED
      //    - Load gate, get on_timeout_action
      //    - Execute action (AUTO_APPROVE, AUTO_REJECT, ESCALATE)
      //    - Log to approval_audit_log
      //    - Publish APPROVAL_EXPIRED event
      //    - Call EvaluateApprovalPolicy to check if gate satisfied
      // 3. Handle errors gracefully (log, continue batch)
  }

  func (j *ApprovalExpirySweepJob) Start(ctx context.Context) {
      // Runs in a loop with ticker, graceful shutdown on ctx.Done()
  }
```

Recommended sweep interval: 1 minute (approvals are time-sensitive).
Show arithmetic: at 100k cases/day with 2 approvals/case on average
= 200k approval requests/day = 139 approvals/minute. Batch size 500
ensures each sweep processes a manageable set even if many expire
simultaneously.

## 7. APPROVER SELECTION FUNCTION

Produce a function that selects eligible approvers based on
approver_selection strategy:
```go
  func SelectApprovers(
      ctx             context.Context,
      db              *sqlx.DB,
      gate            ApprovalGate,
      caseData        Case,
  ) ([]string, error) // returns list of user IDs

  // Strategies:
  //   EXPLICIT_LIST: return gate.approvers (already specified)
  //   ROLE_BASED: query users WHERE role IN gate.approver_roles
  //               AND authority_limit >= gate.authority_limit
  //   REPORTING_CHAIN: walk up the org chart from case.assigned_to
  //                    until finding someone with authority_limit
  //   DYNAMIC_RULE: evaluate a rule expression from gate config
  //                 (e.g. "if amount > 500k then DIRECTOR else MANAGER")
```

Include authority limit filtering for amount-based approvals.
If no eligible approvers found, return ErrNoEligibleApprover
(typed sentinel error).

## 8. TEST CASES

For each sub-capability, three table-driven tests:
  - Happy path
  - Edge case (non-obvious — e.g. delegate an already-delegated
    approval, reject with rework when already at max_rework_attempts,
    expire an approval with AUTO_REJECT that triggers terminal fail,
    approval chain skips tier 2 based on can_skip_if, MAJORITY
    policy with even number of approvers)
  - Failure mode (DB error, invalid expression syntax, no eligible
    approvers, gate evaluation during concurrent approvals)
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

Special test for concurrent approval decisions:
```go
  func TestConcurrentApprovalDecisions(t *testing.T) {
      // Simulate 5 approvers making decisions concurrently
      // for a gate with ALL_MUST_APPROVE policy.
      // Verify gate_status is updated exactly once and
      // all approvers see consistent state via optimistic lock.
  }
```

## 9. APPROVAL STATE TRANSITION GUARD

Produce a validation function that enforces approval lifecycle:
```go
  func ValidateApprovalTransition(
      ctx       context.Context,
      current   ApprovalRequestStatus,
      requested ApprovalRequestStatus,
      actor     Actor,
  ) error
```

Valid transitions to encode:
  PENDING → APPROVED       (approver only, with decision + evidence)
  PENDING → REJECTED       (approver only, with decision + evidence)
  PENDING → DELEGATED      (approver only, delegated_to_id required)
  PENDING → EXPIRED        (system only via sweep job)
  DELEGATED → APPROVED     (delegate only, not original approver)
  DELEGATED → REJECTED     (delegate only)
  EXPIRED → APPROVED       (system only if AUTO_APPROVE)
  EXPIRED → REJECTED       (system only if AUTO_REJECT)
  EXPIRED → PENDING        (system only if ESCALATE, new expires_at)

Reject:
  - APPROVED → anything    (terminal state)
  - REJECTED → anything    (terminal state)
  - PENDING → APPROVED without decision text (evidence required)
  - DELEGATED → action by original approver (only delegate can act)
  - Any transition by non-authorized actor

## 10. INTEGRATION CHECKLIST

  - [ ] Migrations applied and verified (run up + down + up)
  - [ ] user_authority table seeded with test authority limits
  - [ ] ExpressionEvaluator unit tests pass with 100% coverage
  - [ ] CreateTask updated to create approval_gate if requires_approval
  - [ ] Task IN_PROGRESS hook activates approval gate (spawns requests)
  - [ ] HandleEvent updated with all APPROVAL_* event case branches
  - [ ] ApprovalExpirySweepJob registered in main.go with 1min interval
  - [ ] EvaluateApprovalPolicy called after every approve/reject action
  - [ ] RecordStageTransition handles rework regression correctly
  - [ ] Case rework_count incremented on SEND_TO_REWORK
  - [ ] CASE_MAX_REWORK_EXCEEDED logic tested with max_rework_attempts
  - [ ] GetApprovalHistory query tested with JOIN on users table
  - [ ] Prometheus metrics registered:
        approvals_granted_total{case_type, tier}
        approvals_rejected_total{case_type, tier}
        approvals_expired_total{case_type, timeout_action}
        approval_decision_latency_seconds{case_type, tier}
        rework_loops_total{case_type}
  - [ ] Alert rules defined:
        approvals_expired_total{timeout_action="AUTO_REJECT"} > 10/hour
        rework_loops_total > 100/day
  - [ ] Load test: 1000 concurrent approval decisions on same gate
  - [ ] Audit trail verified: every action logged in approval_audit_log

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables
- All approval decisions must be recorded immutably in audit log
- Approval expiry detection must be idempotent
- Gate evaluation must handle concurrent approvals via optimistic lock
- Rework loops must preserve full history (do not delete old gates)
- Expression evaluator must not panic on invalid syntax (return error)
- Every decision requires decision_text and evidence_refs (may be empty array)
- Delegated approvals must preserve full delegation chain
- Authority limits are checked at selection time, not decision time
- Do not analyse any other capability dimension
═══════════════════════════════════════════════════════════════

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
  (e.g. 011_add_document_management.up.sql / .down.sql)
- Event publishing: always via PublishEvent(ctx, tx, event)
  inside the same transaction as the state change (outbox)
- Worker polling: SELECT FOR UPDATE SKIP LOCKED
- No N+1 queries — batch or join at the SQL layer
- Tests: table-driven, testify/assert, DB mocked with sqlmock
- Error wrapping: fmt.Errorf("functionName: %w", err)
- Time handling: all timestamps stored as timestamptz in UTC
- File storage: do NOT store file content in Postgres — store
  metadata only, with references to external object storage
  (S3, GCS, Azure Blob, or local filesystem for dev)

═══════════════════════════════════════════════════════════════
CURRENT IMPLEMENTATION
═══════════════════════════════════════════════════════════════
All my DDLs are here : C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

ALL MY CURRENT GO STRUCTS AND CURRENT KEY FUNCTIONS HERE IN THE CURRENT PROJECT FOLDER YOU ARE IN.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
DOCUMENT & DATA MANAGEMENT
═══════════════════════════════════════════════════════════════
Sub-capabilities to implement — ALL of them, in this order:

  1. DOCUMENT TYPE DEFINITION
     Documents are typed and constrained by the case_type config.
     In the case_type JSONB config, define a document_types array:
       document_types:
         - document_type_code: "INCOME_PROOF"
           display_name: "Proof of Income"
           description: "Pay stubs, tax returns, bank statements"
           allowed_extensions: ["pdf", "jpg", "png"]
           max_size_mb: 10
           required_at_stage: "INITIAL_REVIEW"
           required_count_min: 1
           required_count_max: 3
           is_sensitive: true  # PII / financial data
           retention_days: 2555  # 7 years
           allowed_viewers: ["CASE_OWNER", "UNDERWRITER", "SUPERVISOR"]
         - document_type_code: "ID_DOCUMENT"
           ...
     
     The document_types table stores document type definitions:
       - document_type_code (unique per case_type)
       - case_type_code FK
       - display_name, description
       - allowed_extensions (array or comma-separated)
       - max_size_mb
       - required_at_stage (nullable — if set, case cannot progress
         past this stage without this document)
       - required_count_min, required_count_max
       - is_sensitive boolean
       - retention_days (for auto-deletion/archival policy)
       - allowed_viewers (array of roles or "PUBLIC")
       - created_at, updated_at
     
     When a case is created, the system checks which documents are
     required at the entry stage and may create placeholder
     document_requests (see sub-capability #3).

  2. DOCUMENT STORAGE & METADATA
     Store document metadata in a case_documents table:
       - document_id (UUID PK)
       - case_id FK
       - task_id FK (nullable — if attached to a specific task)
       - stage_code (denormalized — which stage was the doc uploaded in)
       - document_type_code FK to document_types
       - filename (original user-provided name)
       - file_extension
       - file_size_bytes
       - storage_provider (S3 | GCS | AZURE_BLOB | LOCAL)
       - storage_path (bucket + key, or filesystem path)
       - storage_url (pre-signed URL or permanent URL if public)
       - checksum_sha256 (for integrity verification)
       - uploaded_by (user ID)
       - uploaded_at
       - status: PENDING_UPLOAD | UPLOADED | VERIFIED | REJECTED |
         ARCHIVED | DELETED
       - rejection_reason (nullable, if status = REJECTED)
       - verified_by, verified_at (nullable, manual verification)
       - metadata JSONB (OCR results, extracted text, AI tags, etc.)
       - version (integer, for versioned documents)
       - superseded_by_document_id (nullable FK to self — if a
         new version is uploaded, the old version points to the new)
       - created_at, updated_at
     
     CRITICAL: Do NOT store file content (BYTEA) in Postgres at
     scale. Store in object storage (S3, GCS, etc.) and keep only
     metadata + storage_path in Postgres.
     
     Provide functions:
       func UploadDocument(
           ctx     context.Context,
           tx      *sqlx.Tx,
           caseID  string,
           docType string,
           file    io.Reader,
           metadata DocumentMetadata,
       ) (Document, error)
       // Validates file type, size, uploads to storage, inserts row
       
       func GetDocument(
           ctx        context.Context,
           db         *sqlx.DB,
           documentID string,
           requestor  Actor,
       ) (Document, error)
       // Checks allowed_viewers before returning
       
       func DeleteDocument(
           ctx        context.Context,
           tx         *sqlx.Tx,
           documentID string,
           reason     string,
       ) error
       // Soft delete (status = DELETED), keeps metadata for audit

  3. DOCUMENT REQUIREMENT TRACKING
     Track which documents are required vs uploaded in a
     document_requests table:
       - request_id (UUID PK)
       - case_id FK
       - document_type_code FK
       - required_at_stage
       - required_count_min, required_count_max
       - current_count (computed — count of UPLOADED/VERIFIED docs)
       - status: PENDING | PARTIALLY_FULFILLED | FULFILLED | WAIVED
       - requested_at
       - fulfilled_at (nullable)
       - waived_by (nullable user ID), waived_at, waiver_reason
     
     When a document of the required type is uploaded and verified,
     increment current_count. When current_count >= required_count_min,
     set status = FULFILLED.
     
     Provide a function that checks if all document requirements
     for a stage are fulfilled before allowing stage transition:
       func CheckDocumentRequirements(
           ctx       context.Context,
           db        *sqlx.DB,
           caseID    string,
           stageCode string,
       ) (fulfilled bool, missing []DocumentType, err error)
     
     This function is called by RecordStageTransition. If not
     fulfilled, the transition is blocked and a
     STAGE_TRANSITION_BLOCKED event is published with reason:
     "Missing required documents: [doc types]".

  4. DOCUMENT VERSIONING
     When a document is re-uploaded (e.g., updated bank statement),
     create a new document row with version = old_version + 1 and
     set old_document.superseded_by_document_id = new_document.id.
     
     The old version is NOT deleted — it is retained for audit trail.
     Its status is changed to ARCHIVED.
     
     Provide a function:
       func GetDocumentHistory(
           ctx        context.Context,
           db         *sqlx.DB,
           documentID string,
       ) ([]Document, error)
       // Returns the full version chain for a document, ordered
       // by version DESC (newest first)
     
     When viewing a case's documents, by default show only the
     latest version of each document (WHERE superseded_by_document_id
     IS NULL). Provide a toggle to view all versions.

  5. DOCUMENT VERIFICATION WORKFLOW
     Uploaded documents may require manual verification (e.g.,
     a credit analyst reviews an ID document for authenticity).
     
     Document verification is modeled as a special Task type
     with task_definition.is_document_verification = true.
     
     When a document is uploaded with document_type.requires_verification
     = true (new field on document_types), automatically create a
     verification task:
       - task_definition_code: "VERIFY_{document_type_code}"
       - assigned_service: "DOCUMENT_VERIFICATION_SERVICE"
       - input_payload contains: document_id, document_type_code
       - The task is assigned to a role defined in document_type config
         (e.g., "DOCUMENT_REVIEWER")
     
     The verifier can:
       APPROVE: set document.status = VERIFIED, verified_by, verified_at
       REJECT:  set document.status = REJECTED, rejection_reason
       REQUEST_REUPLOAD: notify the uploader to provide a new version
     
     On APPROVE, the document_request.current_count is incremented.
     On REJECT, the document does not count toward fulfillment.
     
     Provide functions:
       func ApproveDocument(
           ctx        context.Context,
           tx         *sqlx.Tx,
           documentID string,
           verifierID string,
       ) error
       
       func RejectDocument(
           ctx        context.Context,
           tx         *sqlx.Tx,
           documentID string,
           verifierID string,
           reason     string,
       ) error

  6. DATA PROPAGATION RULES
     Data flows between tasks. The output of Task A becomes the
     input of Task B. This is defined in the case_type config:
       
       task_definitions:
         - task_definition_code: "CREDIT_CHECK"
           ...
           outputs:
             - field: "credit_score"
               type: "integer"
             - field: "credit_report_url"
               type: "string"
         
         - task_definition_code: "UNDERWRITING_DECISION"
           ...
           inputs:
             - field: "credit_score"
               source_task: "CREDIT_CHECK"
               source_field: "credit_score"
               required: true
             - field: "credit_report_url"
               source_task: "CREDIT_CHECK"
               source_field: "credit_report_url"
               required: false
     
     When creating Task B, the system looks up the completed Task A
     (by task_definition_code within the same case), extracts
     output_payload.credit_score, and injects it into Task B's
     input_payload.credit_score.
     
     If source_task has not completed or the required field is
     missing, Task B creation is blocked. Publish
     TASK_CREATION_BLOCKED event with reason:
     "Dependency not satisfied: waiting for CREDIT_CHECK.credit_score".
     
     Provide a function:
       func ResolveTaskInputs(
           ctx    context.Context,
           db     *sqlx.DB,
           caseID string,
           taskDef TaskDefinition,
       ) (map[string]interface{}, error)
       // Returns the fully resolved input_payload for a task by
       // querying completed tasks and extracting their outputs

  7. SCHEMA VALIDATION & CONTRACTS
     Task input/output payloads must conform to a schema defined
     in the case_type config. Use JSON Schema for validation.
     
     In the task_definition config:
       task_definitions:
         - task_definition_code: "CREDIT_CHECK"
           input_schema:
             type: object
             properties:
               borrower_ssn:
                 type: string
                 pattern: "^\\d{3}-\\d{2}-\\d{4}$"
               credit_bureau:
                 type: string
                 enum: ["EQUIFAX", "EXPERIAN", "TRANSUNION"]
             required: ["borrower_ssn", "credit_bureau"]
           output_schema:
             type: object
             properties:
               credit_score:
                 type: integer
                 minimum: 300
                 maximum: 850
               credit_report_url:
                 type: string
                 format: uri
             required: ["credit_score"]
     
     When a task is created, validate task.input_payload against
     input_schema. When a task completes, validate task.output_payload
     against output_schema. If validation fails, reject the operation
     with a typed error ValidationError that includes the schema
     violation details.
     
     Use a Go JSON Schema library (e.g., github.com/xeipuuv/gojsonschema)
     to perform validation.
     
     Provide functions:
       func ValidateTaskInput(
           ctx       context.Context,
           taskDef   TaskDefinition,
           payload   map[string]interface{},
       ) error
       
       func ValidateTaskOutput(
           ctx       context.Context,
           taskDef   TaskDefinition,
           payload   map[string]interface{},
       ) error

  8. CASE DATA AGGREGATION & ROLLUP
     A case has a master case.metadata JSONB field that aggregates
     key data points from completed tasks. This provides a single
     source of truth for reporting and decision-making without
     scanning all tasks.
     
     In the case_type config, define aggregation rules:
       aggregation_rules:
         - target_field: "metadata.credit_score"
           source_task: "CREDIT_CHECK"
           source_field: "output_payload.credit_score"
           on_task_complete: true
         - target_field: "metadata.loan_amount"
           source_task: "LOAN_APPLICATION"
           source_field: "input_payload.requested_amount"
           on_task_complete: true
         - target_field: "metadata.approval_status"
           source_task: "UNDERWRITING_DECISION"
           source_field: "output_payload.decision"
           on_task_complete: true
     
     When a task completes, the orchestrator evaluates all
     aggregation rules for that task_definition_code. If a rule
     matches, it extracts the source_field value and writes it
     to case.metadata at the target_field path.
     
     Provide a function:
       func ApplyAggregationRules(
           ctx     context.Context,
           tx      *sqlx.Tx,
           caseID  string,
           task    Task,
       ) error
       // Called by HandleEvent on TASK_COMPLETED
       // Updates case.metadata using JSONB set operations
     
     JSONB path updates use Postgres jsonb_set():
       UPDATE cases
       SET metadata = jsonb_set(
           metadata,
           '{credit_score}',
           to_jsonb($1::int),
           true
       )
       WHERE id = $2

  9. SENSITIVE DATA REDACTION & MASKING
     Documents and task payloads may contain PII (SSN, credit card,
     account numbers). Provide redaction for non-authorized viewers.
     
     Define a sensitive_fields table:
       - field_path (e.g. "input_payload.borrower_ssn",
         "metadata.bank_account_number")
       - redaction_rule: MASK | TRUNCATE | HIDE
       - mask_pattern (e.g. "***-**-{last4}" for SSN)
       - allowed_roles (array — only these roles see unredacted)
     
     When serving task data or case metadata via API, apply
     redaction based on the requestor's role:
       
       func RedactSensitiveData(
           ctx       context.Context,
           db        *sqlx.DB,
           data      map[string]interface{},
           requestor Actor,
       ) (map[string]interface{}, error)
       // Walks the data map, finds fields in sensitive_fields,
       // applies redaction if requestor.role not in allowed_roles
     
     Example:
       Original: {"borrower_ssn": "123-45-6789"}
       Redacted: {"borrower_ssn": "***-**-6789"}
     
     Redaction is applied at read time, not write time — original
     data is preserved in the database.

 10. DOCUMENT RETENTION & AUTO-DELETION
     Documents have a retention policy defined by
     document_type.retention_days. After this period, documents
     are automatically archived or deleted.
     
     A background job scans case_documents WHERE:
       status = UPLOADED OR VERIFIED
       AND uploaded_at < NOW() - INTERVAL '{{retention_days}} days'
       AND case.status IN (COMPLETED, CANCELLED)
     
     For each document:
       - If document_type.retention_policy = ARCHIVE:
         Move to cold storage (S3 Glacier, archive tier)
         Set status = ARCHIVED, keep metadata in Postgres
       - If document_type.retention_policy = DELETE:
         Delete from object storage
         Set status = DELETED, keep metadata for audit trail
         (or hard delete if regulatory requirements allow)
     
     Provide a function:
       func EnforceDocumentRetention(
           ctx context.Context,
           db  *sqlx.DB,
       ) (archived int, deleted int, err error)
       // Called by a cron job daily
     
     CRITICAL: Document deletion MUST respect legal hold flags.
     Add a legal_hold boolean to case_documents. If true,
     retention policy is suspended — document is never deleted
     until legal_hold is cleared by compliance officer.

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

  -- FILE: 011_document_management.up.sql
  [DDL — CREATE TABLE, ALTER TABLE, CREATE INDEX]
  [Include detailed column comments explaining purpose]

  -- FILE: 011_document_management.down.sql
  [Full rollback DDL]

New tables to define:
  - document_types
    (id, document_type_code UNIQUE per case_type, case_type_code FK,
     display_name, description, allowed_extensions, max_size_mb,
     required_at_stage, required_count_min, required_count_max,
     is_sensitive, requires_verification, retention_days,
     retention_policy (ARCHIVE | DELETE), allowed_viewers,
     created_at, updated_at)
  
  - case_documents
    (id UUID PK, case_id FK, task_id FK nullable, stage_code,
     document_type_code FK, filename, file_extension,
     file_size_bytes, storage_provider, storage_path, storage_url,
     checksum_sha256, uploaded_by, uploaded_at, status,
     rejection_reason, verified_by, verified_at, metadata JSONB,
     version, superseded_by_document_id FK self, legal_hold,
     created_at, updated_at)
  
  - document_requests
    (id UUID PK, case_id FK, document_type_code FK,
     required_at_stage, required_count_min, required_count_max,
     current_count, status, requested_at, fulfilled_at,
     waived_by, waived_at, waiver_reason)
  
  - sensitive_fields
    (id, field_path, redaction_rule, mask_pattern, allowed_roles,
     created_at)
  
  - document_verification_tasks
    (optional — if verification is modeled as regular tasks,
     this table may not be needed; otherwise, store verification
     audit trail here)

Columns to add to existing tables:
  - task_definitions config (in case_type JSONB):
    Add input_schema, output_schema, outputs array
  - tasks table:
    Add is_document_verification boolean (or derive from task_definition)

Indexes to create:
  - case_documents: (case_id, document_type_code, status)
  - case_documents: (uploaded_at, status) for retention sweep
  - case_documents: (superseded_by_document_id) for version chains
  - document_requests: (case_id, status)
  - sensitive_fields: (field_path) for fast redaction lookups

Rules:
  - Do NOT add a file_content BYTEA column — only metadata
  - checksum_sha256 is indexed for duplicate detection
  - document_id is UUID for global uniqueness across distributed system
  - storage_path format: "{bucket}/{case_id}/{document_id}.{ext}"

After the DDL, produce the corresponding Go structs for
every new table with db and json tags. Use typed enums.

## 3. STORAGE ABSTRACTION LAYER

Define a storage interface before implementation:
```go
  // DocumentStorage abstracts object storage (S3, GCS, local).
  type DocumentStorage interface {
      // Upload stores a file and returns the storage path and URL.
      Upload(
          ctx      context.Context,
          bucket   string,
          key      string,
          content  io.Reader,
          metadata map[string]string,
      ) (path string, url string, err error)

      // Download retrieves a file by path.
      Download(
          ctx  context.Context,
          path string,
      ) (io.ReadCloser, error)

      // Delete removes a file by path.
      Delete(
          ctx  context.Context,
          path string,
      ) error

      // GeneratePresignedURL creates a time-limited download URL.
      GeneratePresignedURL(
          ctx        context.Context,
          path       string,
          expiration time.Duration,
      ) (string, error)
  }
```

Produce at least two implementations:

  ### S3Storage (using AWS SDK)
```go
  type S3Storage struct {
      client *s3.Client
      logger *slog.Logger
  }

  func (s *S3Storage) Upload(...) (...) {
      // Full S3 PutObject implementation
      // Compute checksum, set metadata, handle errors
  }
```

  ### LocalStorage (for dev/test)
```go
  type LocalStorage struct {
      basePath string
      logger   *slog.Logger
  }

  func (s *LocalStorage) Upload(...) (...) {
      // Write to local filesystem at basePath/bucket/key
      // Generate file:// URL
  }
```

Both implementations include:
  - Full error handling (distinguish transient vs permanent)
  - Checksum verification on upload
  - Unit tests with mocked SDK (for S3) or temp dir (for Local)

## 4. GO IMPLEMENTATION

For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums**
```go
  // Document-related enums, config structs, request/response types
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling
```

  **Event published**
  Define event types:
    DOCUMENT_UPLOADED, DOCUMENT_VERIFIED, DOCUMENT_REJECTED,
    DOCUMENT_DELETED, DOCUMENT_VERSION_CREATED,
    DOCUMENT_REQUIREMENT_FULFILLED, STAGE_TRANSITION_BLOCKED,
    TASK_CREATION_BLOCKED

  **Integration point**
  Show where this hooks into:
    - CreateTask: validate input against input_schema, resolve
      dependencies via ResolveTaskInputs
    - CompleteTask: validate output against output_schema, apply
      aggregation rules, check document requirements
    - RecordStageTransition: call CheckDocumentRequirements before
      allowing transition
    - HandleEvent: on TASK_COMPLETED, apply aggregation rules

## 5. SCHEMA VALIDATION ENGINE

Produce full implementation using gojsonschema:
```go
  type SchemaValidator struct {
      cache map[string]*gojsonschema.Schema
      mu    sync.RWMutex
  }

  // ValidateAgainstSchema validates data against a JSON Schema.
  func (v *SchemaValidator) ValidateAgainstSchema(
      ctx    context.Context,
      schema map[string]interface{},
      data   map[string]interface{},
  ) error {
      // Full implementation:
      // 1. Convert schema to gojsonschema.Schema (cache it)
      // 2. Validate data against schema
      // 3. On failure, return ValidationError with field-level details
  }

  // ValidateTaskInput validates a task's input payload.
  func ValidateTaskInput(
      ctx       context.Context,
      validator *SchemaValidator,
      taskDef   TaskDefinition,
      payload   map[string]interface{},
  ) error

  // ValidateTaskOutput validates a task's output payload.
  func ValidateTaskOutput(
      ctx       context.Context,
      validator *SchemaValidator,
      taskDef   TaskDefinition,
      payload   map[string]interface{},
  ) error
```

Include unit tests with table-driven cases:
  - Valid payload (passes)
  - Missing required field (fails with field name)
  - Type mismatch (string instead of int)
  - Enum violation (invalid value)
  - Pattern violation (SSN format)
  - Nested object validation

## 6. DOCUMENT UPLOAD & VERIFICATION FLOW

Produce the complete flow:
```go
  // UploadDocument handles file upload with validation.
  func UploadDocument(
      ctx      context.Context,
      tx       *sqlx.Tx,
      storage  DocumentStorage,
      caseID   string,
      docType  string,
      file     io.Reader,
      metadata DocumentUploadMetadata,
  ) (Document, error) {
      // Full implementation:
      // 1. Load document_type definition
      // 2. Validate file extension against allowed_extensions
      // 3. Validate file size against max_size_mb
      // 4. Compute SHA256 checksum
      // 5. Upload to storage (call storage.Upload)
      // 6. Insert row into case_documents with status = UPLOADED
      // 7. Publish DOCUMENT_UPLOADED event
      // 8. If requires_verification, create verification task
      // 9. Return document metadata
  }

  // ApproveDocument marks a document as verified.
  func ApproveDocument(
      ctx        context.Context,
      tx         *sqlx.Tx,
      documentID string,
      verifierID string,
  ) error {
      // 1. Update status = VERIFIED, verified_by, verified_at
      // 2. Increment document_request.current_count
      // 3. Check if document_request now FULFILLED
      // 4. Publish DOCUMENT_VERIFIED event
      // 5. If FULFILLED, publish DOCUMENT_REQUIREMENT_FULFILLED
  }

  // RejectDocument marks a document as rejected.
  func RejectDocument(
      ctx        context.Context,
      tx         *sqlx.Tx,
      documentID string,
      verifierID string,
      reason     string,
  ) error {
      // 1. Update status = REJECTED, rejection_reason
      // 2. Publish DOCUMENT_REJECTED event
      // 3. Optionally trigger notification to uploader
  }
```

Include unit tests for all three functions with mocked storage.

## 7. DATA PROPAGATION ENGINE

Produce the dependency resolution function:
```go
  // ResolveTaskInputs resolves input dependencies from completed tasks.
  func ResolveTaskInputs(
      ctx     context.Context,
      db      *sqlx.DB,
      caseID  string,
      taskDef TaskDefinition,
  ) (map[string]interface{}, error) {
      // Full implementation:
      // 1. For each input in taskDef.inputs:
      //    - If source_task specified, query completed tasks:
      //      SELECT output_payload FROM tasks
      //      WHERE case_id = $1
      //        AND task_definition_code = $2
      //        AND status = 'COMPLETED'
      //      ORDER BY completed_at DESC LIMIT 1
      //    - Extract source_field from output_payload
      //    - If required = true and field missing, return error
      //    - Insert into resolved inputs map
      // 2. Return fully resolved inputs
  }
```

Include unit tests:
  - All dependencies satisfied (success)
  - Required dependency missing (error)
  - Optional dependency missing (success, field omitted)
  - Source task not completed (error)
  - Multiple completed instances of source task (use latest)

## 8. CASE DATA AGGREGATION

Produce the aggregation rule application:
```go
  // ApplyAggregationRules updates case.metadata based on task output.
  func ApplyAggregationRules(
      ctx     context.Context,
      tx      *sqlx.Tx,
      caseID  string,
      task    Task,
      rules   []AggregationRule,
  ) error {
      // Full implementation:
      // 1. Filter rules to those matching task.task_definition_code
      // 2. For each matching rule:
      //    - Extract source_field from task.output_payload
      //    - Parse target_field path (e.g. "metadata.credit_score")
      //    - Use Postgres jsonb_set to update case.metadata at path
      // 3. Execute in a single UPDATE statement using chained jsonb_set
  }
```

Example SQL generated:
```sql
  UPDATE cases
  SET metadata = jsonb_set(
      jsonb_set(metadata, '{credit_score}', to_jsonb(750), true),
      '{loan_amount}', to_jsonb(250000), true
  )
  WHERE id = $1
```

Include unit tests verifying nested path updates and overwrite behavior.

## 9. SENSITIVE DATA REDACTION

Produce the redaction engine:
```go
  // RedactSensitiveData applies redaction rules to a data map.
  func RedactSensitiveData(
      ctx       context.Context,
      db        *sqlx.DB,
      data      map[string]interface{},
      requestor Actor,
  ) (map[string]interface{}, error) {
      // Full implementation:
      // 1. Load all sensitive_fields matching paths in data
      // 2. For each field:
      //    - Check if requestor.role in allowed_roles
      //    - If not, apply redaction_rule:
      //      - MASK: replace with mask_pattern
      //      - TRUNCATE: keep first N and last M chars
      //      - HIDE: remove field entirely
      // 3. Return redacted copy (do not mutate original)
  }
```

Mask pattern examples:
  - SSN: "***-**-{last4}" → "***-**-6789"
  - Email: "{first3}***@{domain}" → "joh***@example.com"
  - Credit card: "****-****-****-{last4}" → "****-****-****-1234"

Include unit tests:
  - Authorized user (no redaction)
  - Unauthorized user (redaction applied)
  - Multiple fields with different rules
  - Nested field paths (e.g. "metadata.borrower.ssn")

## 10. DOCUMENT RETENTION SWEEP JOB

Produce the background job:
```go
  type DocumentRetentionJob struct {
      db              *sqlx.DB
      storage         DocumentStorage
      archiveBucket   string
      sweepInterval   time.Duration
      batchSize       int
      logger          *slog.Logger
  }

  func (j *DocumentRetentionJob) Run(ctx context.Context) error {
      // Full implementation:
      // 1. Query case_documents WHERE uploaded_at + retention_days < NOW()
      //    AND status IN (UPLOADED, VERIFIED)
      //    AND legal_hold = false
      //    AND case_id IN (SELECT id FROM cases WHERE status IN (COMPLETED, CANCELLED))
      // 2. For each document:
      //    - Load document_type.retention_policy
      //    - If ARCHIVE: move to cold storage, update status = ARCHIVED
      //    - If DELETE: delete from storage, update status = DELETED
      // 3. Log each action, publish DOCUMENT_ARCHIVED or DOCUMENT_DELETED events
      // 4. Handle errors gracefully (log, continue batch)
  }

  func (j *DocumentRetentionJob) Start(ctx context.Context) {
      // Runs in a loop with ticker, graceful shutdown on ctx.Done()
  }
```

Recommended sweep interval: 1 day (retention is not time-critical).
Show arithmetic: at 100k cases/day with 5 docs/case avg = 500k docs/day.
After 7 years (2555 days), that's 1.27B documents. With 1% retention
sweep per day, that's 12.7M docs checked daily. Batch size 10k is reasonable.

## 11. TEST CASES

For each sub-capability, three table-driven tests:
  - Happy path
  - Edge case (upload document exceeding max_size_mb, verify
    already-verified document, create new version when current
    version is REJECTED, resolve task inputs when source task
    has multiple completed instances, apply aggregation rule
    to nested JSON path that doesn't exist yet, redact field
    with mask pattern containing special chars, retention sweep
    with legal_hold = true)
  - Failure mode (storage.Upload timeout, checksum mismatch,
    JSON schema validation failure, missing required document
    blocks stage transition, dependency resolution fails,
    aggregation rule references non-existent field)
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

Special test for end-to-end document flow:
```go
  func TestDocumentLifecycleEndToEnd(t *testing.T) {
      // 1. Create case with document requirement
      // 2. Upload document (status = UPLOADED)
      // 3. Verify document (status = VERIFIED)
      // 4. Check document_request (status = FULFILLED)
      // 5. Attempt stage transition (succeeds)
      // 6. Upload new version (old version ARCHIVED)
      // 7. Apply retention after 7 years (DELETED)
  }
```

## 12. INTEGRATION CHECKLIST

  - [ ] Migrations applied and verified (run up + down + up)
  - [ ] document_types seeded with standard types for loan origination:
        INCOME_PROOF, ID_DOCUMENT, BANK_STATEMENT, TAX_RETURN,
        CREDIT_REPORT, APPRAISAL_REPORT
  - [ ] Storage abstraction tested with both S3Storage and LocalStorage
  - [ ] Schema validator tested with 100% coverage of validation rules
  - [ ] UploadDocument tested with mocked storage
  - [ ] Document verification flow tested end-to-end
  - [ ] ResolveTaskInputs tested with complex dependency chains
  - [ ] ApplyAggregationRules tested with nested JSONB paths
  - [ ] RedactSensitiveData tested with all redaction rules
  - [ ] CheckDocumentRequirements integrated into RecordStageTransition
  - [ ] DocumentRetentionJob registered in main.go with 1-day interval
  - [ ] Legal hold respected by retention sweep (tested)
  - [ ] Prometheus metrics registered:
        documents_uploaded_total{case_type, document_type}
        documents_verified_total{document_type}
        documents_rejected_total{document_type, reason}
        document_upload_size_bytes{document_type}
        document_retention_enforced_total{action}
        schema_validation_failures_total{task_definition}
  - [ ] Alert rules defined:
        documents_rejected_total{document_type="ID_DOCUMENT"} > 10%
        schema_validation_failures > 100/hour
        document_retention_lag > 30 days
  - [ ] Load test: 10k concurrent document uploads with 10MB files each
  - [ ] Storage failover tested (S3 down, fallback behavior)
  - [ ] Document version chain integrity verified (no orphaned versions)

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- NEVER store file content in Postgres — metadata only
- All file uploads must compute and verify SHA256 checksum
- Document verification is a Task, not a separate workflow
- Versioning is append-only — old versions are never deleted
- Redaction is applied at read time, never written to database
- Schema validation must happen before task creation/completion
- Data propagation resolves dependencies at task creation time
- Aggregation rules are applied transactionally with task completion
- Retention policy is enforced only on completed/cancelled cases
- Legal hold suspends ALL automated deletion/archival
- Storage abstraction must support swappable backends (S3, GCS, local)
- Do not analyse any other capability dimension
═══════════════════════════════════════════════════════════════

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
  (e.g. 012_add_audit_compliance.up.sql / .down.sql)
- Event publishing: always via PublishEvent(ctx, tx, event)
  inside the same transaction as the state change (outbox)
- Worker polling: SELECT FOR UPDATE SKIP LOCKED
- No N+1 queries — batch or join at the SQL layer
- Tests: table-driven, testify/assert, DB mocked with sqlmock
- Error wrapping: fmt.Errorf("functionName: %w", err)
- Time handling: all timestamps stored as timestamptz in UTC
- Audit trail: IMMUTABLE — no updates, no deletes, append-only

═══════════════════════════════════════════════════════════════
CURRENT IMPLEMENTATION
═══════════════════════════════════════════════════════════════
All my DDLs are here : C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

ALL MY CURRENT GO STRUCTS AND CURRENT KEY FUNCTIONS HERE IN THE CURRENT PROJECT FOLDER YOU ARE IN.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
AUDIT, COMPLIANCE & REGULATORY
═══════════════════════════════════════════════════════════════
Sub-capabilities to implement — ALL of them, in this order:

  1. IMMUTABLE AUDIT LOG
     Every state change in the system is recorded in an append-only
     audit_log table. No updates, no deletes — only inserts.
     
     Schema:
       - audit_id (UUID PK)
       - entity_type (CASE | TASK | DOCUMENT | APPROVAL | USER |
         WORKBASKET | NOTIFICATION | SLA | CONFIG)
       - entity_id (UUID or string — polymorphic reference)
       - event_type (e.g. CREATED, UPDATED, DELETED, STATUS_CHANGED,
         ASSIGNED, APPROVED, REJECTED, UPLOADED, etc.)
       - actor_type (USER | SYSTEM | SERVICE | EXTERNAL_API)
       - actor_id (user ID, service name, or 'SYSTEM')
       - actor_ip_address (nullable)
       - actor_user_agent (nullable)
       - timestamp (timestamptz, indexed)
       - changes JSONB:
           {
             "before": {"status": "PENDING", "assignee": null},
             "after": {"status": "ASSIGNED", "assignee": "user123"},
             "fields_changed": ["status", "assignee"]
           }
       - reason (nullable text — why the change was made, e.g.
         "Manual reassignment by supervisor due to capacity")
       - trace_id (for distributed tracing, links related events)
       - session_id (optional — groups actions in a user session)
       - compliance_tags JSONB (e.g. {"pci_dss": true, "sox": true})
     
     Every function that modifies state must call:
       func LogAuditEvent(
           ctx       context.Context,
           tx        *sqlx.Tx,
           event     AuditEvent,
       ) error
     within the same transaction as the state change.
     
     Provide a query function:
       func GetAuditTrail(
           ctx        context.Context,
           db         *sqlx.DB,
           entityType EntityType,
           entityID   string,
           startTime  time.Time,
           endTime    time.Time,
       ) ([]AuditEvent, error)
     that returns the full change history for an entity.

  2. REGULATORY HOLD (LEGAL HOLD)
     A case, document, or task can be placed under regulatory hold,
     freezing all modifications and preventing automated deletion.
     
     Add columns to relevant tables:
       - cases: regulatory_hold boolean, hold_placed_by, hold_placed_at,
         hold_reason, hold_release_authorization_required boolean
       - case_documents: regulatory_hold (already added in doc management)
       - tasks: regulatory_hold boolean
     
     When regulatory_hold = true:
       - All UPDATE and DELETE operations are blocked (return
         ErrEntityUnderRegulatoryHold typed error)
       - Document retention policies are suspended
       - Case cannot be archived or deleted
       - Task cannot be cancelled or reassigned
       - All attempts to modify are logged in audit_log with
         event_type = REGULATORY_HOLD_MODIFICATION_BLOCKED
     
     Only a compliance officer role can place or release a hold.
     Release requires:
       - hold_release_authorization_required check
       - If true, a second compliance officer must approve via
         regulatory_hold_releases table:
           - entity_type, entity_id
           - requested_by, requested_at
           - authorized_by (nullable, second officer)
           - authorized_at
           - status: PENDING | APPROVED | DENIED
           - reason
     
     Provide functions:
       func PlaceRegulatoryHold(
           ctx        context.Context,
           tx         *sqlx.Tx,
           entityType EntityType,
           entityID   string,
           placedBy   string,
           reason     string,
       ) error
       
       func ReleaseRegulatoryHold(
           ctx        context.Context,
           tx         *sqlx.Tx,
           entityType EntityType,
           entityID   string,
           releasedBy string,
           reason     string,
       ) error

  3. FOUR-EYES PRINCIPLE (DUAL CONTROL)
     Certain sensitive operations require approval by two distinct
     users before execution. Define sensitive operations in a
     sensitive_operations table:
       - operation_code (e.g. CASE_EMERGENCY_CLOSE, AUTHORITY_GRANT,
         REGULATORY_HOLD_RELEASE, CONFIG_CHANGE, USER_ROLE_CHANGE)
       - requires_dual_control boolean
       - authorized_roles (array — which roles can authorize)
       - description
     
     When a sensitive operation is requested, create a
     dual_control_requests table entry:
       - request_id (UUID PK)
       - operation_code FK
       - entity_type, entity_id (what is being operated on)
       - requested_by (first user)
       - requested_at
       - authorized_by (nullable, second user)
       - authorized_at
       - status: PENDING | APPROVED | DENIED | EXPIRED
       - expires_at (requests expire after N hours)
       - justification (why the operation is needed)
       - evidence_refs JSONB (supporting documents)
     
     The operation is NOT executed when requested. It is held in
     PENDING state. A second user (authorized_by != requested_by)
     must call:
       func AuthorizeDualControlRequest(
           ctx        context.Context,
           tx         *sqlx.Tx,
           requestID  string,
           authorizer string,
       ) error
     which sets status = APPROVED and THEN executes the operation.
     
     A background sweep job expires requests past expires_at.
     
     Every sensitive operation check function before execution:
       func RequiresDualControl(
           ctx           context.Context,
           db            *sqlx.DB,
           operationCode string,
       ) (bool, error)
     
     If true and no approved request exists, return
     ErrDualControlRequired with the request_id to poll.

  4. DATA RETENTION & RIGHT TO ERASURE (GDPR)
     Support GDPR Article 17 (Right to Erasure) without violating
     audit trail immutability.
     
     Add a data_erasure_requests table:
       - erasure_request_id (UUID PK)
       - subject_type (BORROWER | GUARANTOR | CO_APPLICANT)
       - subject_id (e.g. borrower_id from case.metadata)
       - requested_by (user or the subject themselves)
       - requested_at
       - status: PENDING | IN_PROGRESS | COMPLETED | DENIED
       - denial_reason (if DENIED — e.g. "Case under legal hold")
       - completed_at
       - erasure_summary JSONB (which entities were anonymized)
     
     When an erasure request is approved:
       - Identify all cases, tasks, documents, notifications where
         subject_id appears in metadata
       - ANONYMIZE rather than DELETE:
           - Replace PII fields with "[REDACTED]" or generate a
             pseudonymous identifier
           - Update case.metadata, task.input_payload/output_payload,
             document metadata, notification bodies
           - DO NOT delete audit_log rows — mark them with
             erasure_applied = true and anonymize the changes JSONB
       - Log every anonymization in a data_erasure_audit table:
           - erasure_request_id FK
           - entity_type, entity_id
           - fields_anonymized (array)
           - anonymized_at
       - Document retention policies continue to apply to anonymized
         records (they can be deleted after retention period)
     
     Provide functions:
       func CreateErasureRequest(
           ctx         context.Context,
           tx          *sqlx.Tx,
           subjectType SubjectType,
           subjectID   string,
           requestedBy string,
       ) (string, error) // returns request_id
       
       func ExecuteErasureRequest(
           ctx       context.Context,
           db        *sqlx.DB,
           requestID string,
       ) error
       // Orchestrates the anonymization across all entities

  5. ACCESS CONTROL AUDIT (WHO SAW WHAT)
     Track every read access to sensitive data. Not just writes —
     READS too. This is required for HIPAA, PCI-DSS, and financial
     services regulations.
     
     Create an access_audit_log table:
       - access_id (UUID PK)
       - accessed_at (timestamptz, heavily indexed)
       - accessor_id (user ID)
       - accessor_role
       - accessor_ip_address
       - entity_type (CASE | TASK | DOCUMENT | BORROWER_PROFILE)
       - entity_id
       - access_type (READ | DOWNLOAD | EXPORT | PRINT)
       - sensitivity_level (PUBLIC | INTERNAL | CONFIDENTIAL |
         RESTRICTED)
       - access_granted boolean (false if denied by ACL)
       - denial_reason (nullable)
       - data_fields_accessed (array — which specific fields)
       - trace_id
     
     Every function that serves sensitive data to a user (API
     endpoints, document downloads, case detail views) must log:
       func LogAccessEvent(
           ctx   context.Context,
           db    *sqlx.DB,
           event AccessEvent,
       ) error
     This is NOT transactional with the read — it's a separate
     async write to avoid slowing down reads. Use a buffered
     channel and a background goroutine that batches inserts.
     
     Provide query functions for compliance officers:
       func GetUserAccessHistory(
           ctx        context.Context,
           db         *sqlx.DB,
           userID     string,
           startTime  time.Time,
           endTime    time.Time,
       ) ([]AccessEvent, error)
       
       func GetEntityAccessHistory(
           ctx        context.Context,
           db         *sqlx.DB,
           entityType EntityType,
           entityID   string,
       ) ([]AccessEvent, error)
       
       func DetectAnomalousAccess(
           ctx context.Context,
           db  *sqlx.DB,
       ) ([]AccessAnomaly, error)
       // Flags: bulk downloads, after-hours access to restricted
       // entities, access from unusual IP ranges, rapid sequential
       // access to unrelated cases (data mining pattern)

  6. COMPLIANCE REPORT GENERATION
     Generate pre-built compliance reports for auditors. Store
     report definitions in a compliance_reports table:
       - report_code (e.g. SOX_ACTIVITY_LOG, PCI_ACCESS_REPORT,
         GDPR_ERASURE_SUMMARY, SLA_BREACH_ANALYSIS)
       - report_name, description
       - query_template (SQL with placeholders for date range, etc.)
       - output_format (CSV | JSON | PDF)
       - required_parameters JSONB (e.g. {"start_date": "date",
         "end_date": "date", "case_type": "string"})
       - authorized_roles (array — who can run this report)
     
     Provide a function:
       func GenerateComplianceReport(
           ctx        context.Context,
           db         *sqlx.DB,
           reportCode string,
           parameters map[string]interface{},
           requestor  Actor,
       ) ([]byte, error)
       // Validates requestor.role, renders query with parameters,
       // executes, formats output, logs the report generation in
       // compliance_report_executions table
     
     Pre-built reports to include:
       - SOX Activity Log: all case state changes by user over period
       - PCI Access Report: all access to cases with PCI-tagged data
       - GDPR Erasure Summary: all erasure requests and their status
       - SLA Breach Analysis: all breaches with root cause
       - Approval Chain Integrity: all approvals with delegation chains
       - Document Verification Audit: all document verify/reject actions
       - Regulatory Hold Summary: all entities under hold
     
     Each report execution is logged:
       compliance_report_executions table:
         - execution_id (UUID PK)
         - report_code FK
         - executed_by, executed_at
         - parameters JSONB
         - row_count (how many rows returned)
         - execution_time_ms
         - output_file_path (if saved to storage)

  7. TAMPER DETECTION (AUDIT LOG INTEGRITY)
     Ensure the audit_log itself has not been tampered with.
     Use cryptographic hashing to detect unauthorized modifications.
     
     Add columns to audit_log:
       - previous_audit_id (nullable FK to self — links to prior entry)
       - hash (SHA256 hash of this entry + previous_hash, forming a chain)
     
     When inserting a new audit entry:
       1. Retrieve the latest audit_log row (ORDER BY timestamp DESC LIMIT 1)
       2. Extract its hash as previous_hash
       3. Compute current_hash = SHA256(
            audit_id || timestamp || entity_type || entity_id ||
            event_type || actor_id || changes || previous_hash
          )
       4. Insert new row with previous_audit_id and hash
     
     This creates a blockchain-like chain where tampering with any
     entry breaks the hash chain for all subsequent entries.
     
     Provide a function:
       func VerifyAuditLogIntegrity(
           ctx       context.Context,
           db        *sqlx.DB,
           startTime time.Time,
           endTime   time.Time,
       ) (valid bool, brokenAt *AuditEvent, err error)
       // Walks the chain from startTime to endTime, recomputes each
       // hash, detects if any hash doesn't match. Returns the first
       // entry where the chain is broken.
     
     Run this verification:
       - On-demand by compliance officers
       - Scheduled daily as a background job
       - After any suspected security incident

  8. CHANGE CONTROL & CONFIG VERSIONING
     All changes to case_type configs, approval policies, SLA
     definitions, notification templates, and document type configs
     are versioned and require approval before activation.
     
     Create a config_change_requests table:
       - change_request_id (UUID PK)
       - config_type (CASE_TYPE | APPROVAL_POLICY | SLA_DEFINITION |
         NOTIFICATION_TEMPLATE | DOCUMENT_TYPE)
       - config_id (ID of the entity being changed)
       - change_type (CREATE | UPDATE | DELETE | DEPRECATE)
       - proposed_changes JSONB (the new config or diff)
       - requested_by, requested_at
       - reviewed_by (nullable), reviewed_at
       - status: PENDING | APPROVED | REJECTED | DEPLOYED
       - rejection_reason
       - deployed_at (nullable)
       - rollback_config JSONB (previous version for rollback)
     
     Workflow:
       1. User proposes a change (e.g. update case_type SLA from 48h to 36h)
       2. System creates config_change_requests entry, status = PENDING
       3. A reviewer (supervisor or change control board) reviews:
          - If APPROVED: status = APPROVED
          - If REJECTED: status = REJECTED, rejection_reason
       4. Approved changes are deployed via:
            func DeployConfigChange(
                ctx       context.Context,
                tx        *sqlx.Tx,
                requestID string,
            ) error
          which applies the change and sets status = DEPLOYED
       5. All deployments are logged in audit_log
     
     Provide a rollback function:
       func RollbackConfigChange(
           ctx       context.Context,
           tx        *sqlx.Tx,
           requestID string,
       ) error
       // Reverts to rollback_config, creates a new change request
       // for the rollback (also requires approval)

  9. USER ACTIVITY MONITORING & SESSION TRACKING
     Track user sessions and activity for security monitoring.
     
     Create a user_sessions table:
       - session_id (UUID PK)
       - user_id FK
       - login_at, logout_at (nullable)
       - ip_address, user_agent
       - session_duration_seconds
       - activity_count (number of actions performed)
       - last_activity_at
       - logout_reason (NORMAL | TIMEOUT | FORCED | SUSPICIOUS_ACTIVITY)
     
     Create a user_activity_log table:
       - activity_id (UUID PK)
       - session_id FK
       - user_id
       - activity_type (PAGE_VIEW | CASE_OPENED | TASK_CLAIMED |
         DOCUMENT_DOWNLOADED | APPROVAL_SUBMITTED | SEARCH_EXECUTED)
       - entity_type (nullable), entity_id (nullable)
       - activity_timestamp
       - duration_ms (how long the action took)
       - result (SUCCESS | FAILURE | DENIED)
     
     Every HTTP request handler or API endpoint logs activity:
       func LogUserActivity(
           ctx      context.Context,
           db       *sqlx.DB,
           activity UserActivity,
       ) error
     
     Provide a session inactivity sweep job that logs out users
     after 30 minutes of inactivity (configurable).
     
     Provide anomaly detection:
       func DetectSuspiciousUserActivity(
           ctx context.Context,
           db  *sqlx.DB,
       ) ([]SuspiciousActivity, error)
       // Flags: logins from multiple IPs in short time, bulk case
       // access (>50 cases in 10 minutes), login outside normal hours,
       // failed login attempts (>5 in 10 minutes)

 10. COMPLIANCE DASHBOARD & METRICS
     Provide aggregated compliance metrics via a
     compliance_metrics_summary table (materialized view or
     periodically updated):
       - metric_date
       - total_cases_created
       - cases_under_regulatory_hold
       - sla_breaches_count
       - approval_rejection_rate
       - document_verification_rejection_rate
       - erasure_requests_pending
       - dual_control_requests_pending
       - suspicious_access_events_count
       - audit_log_integrity_status (VALID | COMPROMISED)
       - last_updated_at
     
     Provide a query function:
       func GetComplianceDashboard(
           ctx       context.Context,
           db        *sqlx.DB,
           startDate time.Time,
           endDate   time.Time,
       ) (ComplianceDashboard, error)
       // Returns aggregated metrics for the dashboard
     
     Expose Prometheus metrics:
       - audit_log_entries_total{entity_type, event_type}
       - regulatory_holds_active{entity_type}
       - dual_control_requests_pending
       - erasure_requests_pending
       - access_denied_total{entity_type, reason}
       - compliance_reports_generated_total{report_code}
       - audit_log_integrity_checks_failed_total

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

  -- FILE: 012_audit_compliance.up.sql
  [DDL — CREATE TABLE, ALTER TABLE, CREATE INDEX]
  [Include detailed column comments explaining purpose]

  -- FILE: 012_audit_compliance.down.sql
  [Full rollback DDL]

New tables to define:
  - audit_log
    (audit_id UUID PK, entity_type, entity_id, event_type,
     actor_type, actor_id, actor_ip_address, actor_user_agent,
     timestamp, changes JSONB, reason, trace_id, session_id,
     compliance_tags JSONB, previous_audit_id FK self, hash,
     erasure_applied boolean default false)
  
  - regulatory_hold_releases
    (id UUID PK, entity_type, entity_id, requested_by,
     requested_at, authorized_by, authorized_at, status,
     reason, created_at, updated_at)
  
  - sensitive_operations
    (operation_code PRIMARY KEY, requires_dual_control,
     authorized_roles, description)
  
  - dual_control_requests
    (request_id UUID PK, operation_code FK, entity_type,
     entity_id, requested_by, requested_at, authorized_by,
     authorized_at, status, expires_at, justification,
     evidence_refs JSONB, created_at, updated_at)
  
  - data_erasure_requests
    (erasure_request_id UUID PK, subject_type, subject_id,
     requested_by, requested_at, status, denial_reason,
     completed_at, erasure_summary JSONB)
  
  - data_erasure_audit
    (id UUID PK, erasure_request_id FK, entity_type, entity_id,
     fields_anonymized, anonymized_at)
  
  - access_audit_log
    (access_id UUID PK, accessed_at, accessor_id, accessor_role,
     accessor_ip_address, entity_type, entity_id, access_type,
     sensitivity_level, access_granted, denial_reason,
     data_fields_accessed, trace_id)
  
  - compliance_reports
    (report_code PRIMARY KEY, report_name, description,
     query_template, output_format, required_parameters JSONB,
     authorized_roles)
  
  - compliance_report_executions
    (execution_id UUID PK, report_code FK, executed_by,
     executed_at, parameters JSONB, row_count,
     execution_time_ms, output_file_path)
  
  - config_change_requests
    (change_request_id UUID PK, config_type, config_id,
     change_type, proposed_changes JSONB, requested_by,
     requested_at, reviewed_by, reviewed_at, status,
     rejection_reason, deployed_at, rollback_config JSONB)
  
  - user_sessions
    (session_id UUID PK, user_id, login_at, logout_at,
     ip_address, user_agent, session_duration_seconds,
     activity_count, last_activity_at, logout_reason)
  
  - user_activity_log
    (activity_id UUID PK, session_id FK, user_id, activity_type,
     entity_type, entity_id, activity_timestamp, duration_ms,
     result)
  
  - compliance_metrics_summary
    (metric_date PRIMARY KEY, total_cases_created,
     cases_under_regulatory_hold, sla_breaches_count,
     approval_rejection_rate, document_verification_rejection_rate,
     erasure_requests_pending, dual_control_requests_pending,
     suspicious_access_events_count, audit_log_integrity_status,
     last_updated_at)

Columns to add to existing tables:
  - cases: regulatory_hold, hold_placed_by, hold_placed_at,
    hold_reason, hold_release_authorization_required
  - tasks: regulatory_hold
  - case_documents: regulatory_hold (already added)

Indexes to create:
  - audit_log: (entity_type, entity_id, timestamp DESC)
  - audit_log: (timestamp DESC) for integrity verification
  - audit_log: (trace_id) for distributed tracing
  - access_audit_log: (accessed_at DESC) for time-range queries
  - access_audit_log: (accessor_id, accessed_at DESC)
  - access_audit_log: (entity_type, entity_id)
  - dual_control_requests: (status, expires_at)
  - data_erasure_requests: (status, requested_at)

Rules:
  - audit_log is append-only — add CHECK constraint preventing UPDATEs
  - All timestamps are timestamptz in UTC
  - hash column is indexed for integrity checks
  - Partitioning: audit_log and access_audit_log should be
    partitioned by month (use Postgres table partitioning)

After the DDL, produce the corresponding Go structs for
every new table with db and json tags. Use typed enums.

## 3. GO IMPLEMENTATION

For each sub-capability, produce:

  ### [Sub-capability Name]

  **New types / enums**
```go
  // Audit/compliance-related enums, config structs, request/response types
```

  **Core function(s)**
```go
  // production-ready, compiles, full error handling
```

  **Event published**
  Define event types:
    REGULATORY_HOLD_PLACED, REGULATORY_HOLD_RELEASED,
    DUAL_CONTROL_REQUEST_CREATED, DUAL_CONTROL_REQUEST_AUTHORIZED,
    ERASURE_REQUEST_CREATED, ERASURE_COMPLETED,
    AUDIT_LOG_INTEGRITY_COMPROMISED, CONFIG_CHANGE_DEPLOYED

  **Integration point**
  Show where this hooks into:
    - Every state-changing function: call LogAuditEvent before Commit
    - Every read endpoint: call LogAccessEvent (async, buffered)
    - RecordStageTransition: check regulatory_hold before transition
    - CreateTask, UpdateTask: check regulatory_hold
    - UploadDocument, DeleteDocument: check regulatory_hold
    - All sensitive operations: check RequiresDualControl

## 4. AUDIT LOG IMPLEMENTATION

Produce the complete audit logging system:
```go
  type AuditLogger struct {
      db     *sqlx.DB
      logger *slog.Logger
  }

  // LogAuditEvent records a state change in the immutable audit log.
  func (a *AuditLogger) LogAuditEvent(
      ctx   context.Context,
      tx    *sqlx.Tx,
      event AuditEvent,
  ) error {
      // Full implementation:
      // 1. Retrieve latest audit_log entry (previous_audit_id, hash)
      // 2. Compute current hash including previous_hash
      // 3. Insert new audit_log row with hash chain
      // 4. If insert fails due to concurrent access, retry once
  }

  // GetAuditTrail retrieves the full change history for an entity.
  func (a *AuditLogger) GetAuditTrail(
      ctx        context.Context,
      db         *sqlx.DB,
      entityType EntityType,
      entityID   string,
      startTime  time.Time,
      endTime    time.Time,
  ) ([]AuditEvent, error)

  // VerifyAuditLogIntegrity checks the hash chain for tampering.
  func (a *AuditLogger) VerifyAuditLogIntegrity(
      ctx       context.Context,
      db        *sqlx.DB,
      startTime time.Time,
      endTime   time.Time,
  ) (valid bool, brokenAt *AuditEvent, err error) {
      // Full implementation:
      // 1. Query audit_log WHERE timestamp BETWEEN startTime AND endTime
      //    ORDER BY timestamp ASC
      // 2. Walk the chain, recompute each hash
      // 3. If hash != stored hash, return broken entry
  }
```

Include unit tests:
  - Single audit entry (hash computed correctly)
  - Multiple entries (chain linking verified)
  - Integrity check on valid chain (returns valid = true)
  - Integrity check with tampered entry (returns broken entry)

## 5. REGULATORY HOLD IMPLEMENTATION

Produce the hold placement/release functions:
```go
  // PlaceRegulatoryHold freezes an entity from modification.
  func PlaceRegulatoryHold(
      ctx        context.Context,
      tx         *sqlx.Tx,
      entityType EntityType,
      entityID   string,
      placedBy   string,
      reason     string,
  ) error {
      // Full implementation:
      // 1. Update entity's regulatory_hold = true, hold_* fields
      // 2. Log in audit_log with event_type = REGULATORY_HOLD_PLACED
      // 3. Publish REGULATORY_HOLD_PLACED event
  }

  // ReleaseRegulatoryHold lifts the hold if authorized.
  func ReleaseRegulatoryHold(
      ctx        context.Context,
      tx         *sqlx.Tx,
      entityType EntityType,
      entityID   string,
      releasedBy string,
      reason     string,
  ) error {
      // Full implementation:
      // 1. If hold_release_authorization_required, check for approved
      //    regulatory_hold_release request
      // 2. Update regulatory_hold = false
      // 3. Log in audit_log
      // 4. Publish event
  }

  // CheckRegulatoryHold returns error if entity is under hold.
  func CheckRegulatoryHold(
      ctx        context.Context,
      db         *sqlx.DB,
      entityType EntityType,
      entityID   string,
  ) error
```

Add CheckRegulatoryHold call to every state-changing function:
  - RecordStageTransition
  - UpdateTaskStatus
  - UploadDocument, DeleteDocument
  - ApproveDocument, RejectDocument

Include unit tests:
  - Place hold (success)
  - Attempt modification while under hold (returns error)
  - Release hold with dual control (requires approval)
  - Release hold without authorization (denied)

## 6. DUAL CONTROL REQUEST SYSTEM

Produce the dual control flow:
```go
  // RequiresDualControl checks if an operation needs dual control.
  func RequiresDualControl(
      ctx           context.Context,
      db            *sqlx.DB,
      operationCode string,
  ) (bool, error)

  // CreateDualControlRequest initiates a sensitive operation.
  func CreateDualControlRequest(
      ctx         context.Context,
      tx          *sqlx.Tx,
      operationCode string,
      entityType  EntityType,
      entityID    string,
      requestedBy string,
      justification string,
  ) (string, error) // returns request_id

  // AuthorizeDualControlRequest approves and executes the operation.
  func AuthorizeDualControlRequest(
      ctx        context.Context,
      tx         *sqlx.Tx,
      requestID  string,
      authorizer string,
  ) error {
      // Full implementation:
      // 1. Load request, verify status = PENDING, not expired
      // 2. Verify authorizer != requested_by (cannot self-authorize)
      // 3. Verify authorizer.role in authorized_roles
      // 4. Update status = APPROVED, authorized_by, authorized_at
      // 5. Execute the operation (call the actual function)
      // 6. Log in audit_log
      // 7. Publish event
  }

  // DualControlExpirySweep marks expired requests as EXPIRED.
  func (j *DualControlExpirySweep) Run(ctx context.Context) error
```

Include unit tests:
  - Create request (PENDING)
  - Authorize by same user (denied)
  - Authorize by authorized role (approved, operation executed)
  - Authorize after expiry (denied)

## 7. DATA ERASURE ENGINE

Produce the GDPR erasure implementation:
```go
  // CreateErasureRequest initiates a GDPR erasure.
  func CreateErasureRequest(
      ctx         context.Context,
      tx          *sqlx.Tx,
      subjectType SubjectType,
      subjectID   string,
      requestedBy string,
  ) (string, error) // returns erasure_request_id

  // ExecuteErasureRequest anonymizes all data for a subject.
  func ExecuteErasureRequest(
      ctx       context.Context,
      db        *sqlx.DB,
      requestID string,
  ) error {
      // Full implementation:
      // 1. Load erasure request, verify status = PENDING
      // 2. Check for regulatory_hold on any related entities
      //    If hold exists, set status = DENIED, reason
      // 3. Identify all cases WHERE metadata @> {"borrower_id": subject_id}
      // 4. For each case:
      //    - Anonymize case.metadata (replace PII with "[REDACTED]")
      //    - For each task in case:
      //      - Anonymize input_payload, output_payload
      //    - For each document in case:
      //      - Anonymize document metadata
      //    - Log each anonymization in data_erasure_audit
      // 5. Anonymize audit_log entries (set erasure_applied = true,
      //    redact changes JSONB)
      // 6. Set erasure_request status = COMPLETED
      // 7. Log in audit_log
      // 8. Publish ERASURE_COMPLETED event
  }
```

Include unit tests:
  - Execute erasure (all PII anonymized)
  - Execute erasure on entity under hold (denied)
  - Verify audit_log entries marked erasure_applied = true

## 8. ACCESS AUDIT LOGGING

Produce the access logging system:
```go
  type AccessAuditor struct {
      db          *sqlx.DB
      eventBuffer chan AccessEvent
      batchSize   int
      flushInterval time.Duration
      logger      *slog.Logger
  }

  // Start begins the background batch flusher.
  func (a *AccessAuditor) Start(ctx context.Context)

  // LogAccessEvent queues an access event for async logging.
  func (a *AccessAuditor) LogAccessEvent(
      ctx   context.Context,
      event AccessEvent,
  ) error {
      // Send to buffered channel (non-blocking with timeout)
  }

  // flushBatch writes batched events to access_audit_log.
  func (a *AccessAuditor) flushBatch(ctx context.Context, events []AccessEvent) error

  // GetUserAccessHistory retrieves access history for a user.
  func GetUserAccessHistory(
      ctx        context.Context,
      db         *sqlx.DB,
      userID     string,
      startTime  time.Time,
      endTime    time.Time,
  ) ([]AccessEvent, error)

  // DetectAnomalousAccess flags suspicious access patterns.
  func DetectAnomalousAccess(
      ctx context.Context,
      db  *sqlx.DB,
  ) ([]AccessAnomaly, error) {
      // Full implementation:
      // 1. Bulk case access: COUNT(*) > 50 in 10 minutes
      // 2. Multiple IPs: DISTINCT ip_address > 3 for same user in 1 hour
      // 3. After-hours access: accessed_at NOT IN business hours
      // 4. Rapid sequential access to unrelated cases
  }
```

Include unit tests:
  - Log access event (buffered)
  - Flush batch (inserted into DB)
  - Detect bulk access anomaly
  - Detect multiple IP anomaly

## 9. COMPLIANCE REPORT ENGINE

Produce the report generation system:
```go
  // GenerateComplianceReport executes a pre-defined report.
  func GenerateComplianceReport(
      ctx        context.Context,
      db         *sqlx.DB,
      reportCode string,
      parameters map[string]interface{},
      requestor  Actor,
  ) ([]byte, error) {
      // Full implementation:
      // 1. Load compliance_reports row by reportCode
      // 2. Verify requestor.role in authorized_roles
      // 3. Validate parameters against required_parameters
      // 4. Render query_template with parameters
      // 5. Execute query
      // 6. Format result as output_format (CSV, JSON, PDF)
      // 7. Log in compliance_report_executions
      // 8. Return formatted output
  }
```

Pre-defined report queries (include in migration as INSERTs):
  - SOX Activity Log
  - PCI Access Report
  - GDPR Erasure Summary
  - SLA Breach Analysis
  - Approval Chain Integrity
  - Document Verification Audit
  - Regulatory Hold Summary

Include unit tests:
  - Generate report with valid parameters (success)
  - Generate report by unauthorized user (denied)
  - Invalid parameters (validation error)

## 10. TEST CASES

For each sub-capability, three table-driven tests:
  - Happy path
  - Edge case (audit log integrity check on empty table,
    regulatory hold release requires dual control, erasure
    request on subject with no data, access audit with
    concurrent flushes, dual control request self-authorization
    attempt, config change rollback of already-rolled-back change)
  - Failure mode (audit log hash computation failure, regulatory
    hold check on non-existent entity, dual control authorization
    after expiry, erasure execution on entity under hold,
    compliance report with SQL injection in parameters)
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

## 11. INTEGRATION CHECKLIST

  - [ ] Migrations applied and verified (run up + down + up)
  - [ ] audit_log partitioned by month (verify partition creation)
  - [ ] LogAuditEvent integrated into all state-changing functions
  - [ ] CheckRegulatoryHold integrated into all modification functions
  - [ ] RequiresDualControl checked in all sensitive operations
  - [ ] AccessAuditor started in main.go, buffered channel sized
  - [ ] LogAccessEvent called in all read endpoints (API, downloads)
  - [ ] sensitive_operations table seeded with standard operations
  - [ ] compliance_reports table seeded with pre-defined reports
  - [ ] Audit log integrity verification scheduled daily
  - [ ] Dual control expiry sweep registered with 1-hour interval
  - [ ] Access anomaly detection scheduled hourly
  - [ ] Compliance dashboard metrics refresh scheduled daily
  - [ ] Prometheus metrics registered:
        audit_log_entries_total{entity_type, event_type}
        regulatory_holds_active{entity_type}
        dual_control_requests_pending
        erasure_requests_pending
        access_denied_total{entity_type, reason}
        audit_log_integrity_checks_failed_total
  - [ ] Alert rules defined:
        regulatory_holds_active > [threshold]
        dual_control_requests_pending for > 24h
        erasure_requests_pending for > 7 days
        access_denied_total spike (> 100/hour)
        audit_log_integrity_checks_failed_total > 0
  - [ ] Load test: 1M audit log entries inserted, integrity verified
  - [ ] Erasure execution tested on case with 100+ related entities
  - [ ] Access audit buffer tested with 10k events/second
  - [ ] Compliance report generation tested for all pre-defined reports

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- audit_log is IMMUTABLE — no UPDATE or DELETE ever
- All audit logging must be transactional with state changes
- Access audit logging is ASYNC (buffered, non-blocking)
- Regulatory hold blocks ALL modifications, no exceptions
- Dual control requires DISTINCT users (cannot self-authorize)
- Data erasure ANONYMIZES, never deletes audit trail
- Hash chain integrity must be verifiable at any time
- All compliance reports require role-based authorization
- Config changes require approval before deployment
- Access anomaly detection runs hourly (not real-time)
- Partitioning on audit_log and access_audit_log is MANDATORY
- Do not analyse any other capability dimension
═══════════════════════════════════════════════════════════════

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
  (e.g. 010_add_exception_handling.up.sql / .down.sql)
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
All DDL migration files are in:
  C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

All current Go structs and key functions are in the project
folder open in this session.

Read and understand the full existing implementation before
producing any output. Do not restate or summarise what you
read — proceed directly to the gap analysis and implementation.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
EXCEPTION & ERROR HANDLING
═══════════════════════════════════════════════════════════════
Scope: analyse, then fully implement, the EXCEPTION & ERROR
HANDLING capability dimension only.

Do not analyse or produce code for any other capability dimension.

STEP 1 — GAP ANALYSIS
──────────────────────
Compare the current implementation against the full enterprise-
grade capability set for exception and error handling. Structure
your gap analysis as a table with these columns:

  Sub-Capability | Current State | Gap | Severity (P1/P2/P3)

Sub-capabilities to cover (ALL of them):

  1. Task-level error classification
     Distinguish TRANSIENT errors (network timeout, DB deadlock,
     downstream 503) from PERMANENT errors (business rule
     violation, malformed payload, missing required field).
     TRANSIENT errors trigger retry. PERMANENT errors move the
     task to a terminal FAILED state immediately.

  2. Retry policy engine
     Per-task-definition retry configuration stored in the
     case_type config blob:
       - max_retries (integer)
       - backoff_strategy: FIXED | LINEAR | EXPONENTIAL
       - base_interval_seconds (integer)
       - max_interval_seconds (integer, caps exponential growth)
       - retryable_error_codes (list of strings)
     Compute next_attempt_at from the task's current retry_count
     and the policy. Enforce max_retries — after exhaustion,
     transition task to FAILED and publish TASK_FAILED event.

  3. Dead letter queue (DLQ)
     Tasks that exhaust retries are moved to a task_dlq table,
     not deleted. Schema:
       - dlq_id (PK, UUID)
       - task_id (FK)
       - case_id (FK)
       - failure_reason (text)
       - error_detail (JSONB — full error context)
       - moved_at (timestamptz)
       - requeue_count (integer, how many times replayed)
       - last_requeue_at (nullable timestamptz)
     Operators can requeue a DLQ entry: the task is reset to
     PENDING with retry_count = 0 and a new idempotency_key.

  4. Case-level exception escalation
     If a blocking task fails permanently, the parent Case
     transitions to EXCEPTION status. Define the rules:
       - Which task failure severities escalate the Case
       - How sub-Case failures propagate to the parent Case
       - What events are published on Case exception

  5. Error detail capture
     Every task failure must capture a structured error_detail
     JSONB blob containing at minimum:
       - error_code (string)
       - error_class: TRANSIENT | PERMANENT | UNKNOWN
       - message (human-readable)
       - source_service (which service reported the error)
       - occurred_at (timestamptz)
       - stack_context (optional, for Go panics recovered
         in the worker)
       - upstream_error (raw response from downstream system,
         if applicable)

  6. Poison pill detection
     Detect tasks that repeatedly fail across multiple requeue
     attempts without making progress. Flag as POISON_PILL after
     a configurable threshold of total failures across all retries
     and DLQ requeues. Quarantine them — prevent auto-requeue
     until an operator explicitly releases them.

  7. Saga / compensation
     For multi-task workflows where partial completion must be
     rolled back on failure, define a compensation pattern:
       - task_definitions may declare a compensating_task_code
       - on failure of the forward task, if compensation is
         defined, create a compensation Task and publish
         COMPENSATION_STARTED event
       - track compensation state: PENDING | IN_PROGRESS |
         COMPLETED | FAILED

  8. Operator exception dashboard support
     Produce query functions (not UI) that the dashboard can call:
       - ListExceptionCases: cases in EXCEPTION status with
         failure summary, oldest first
       - GetDLQEntries: DLQ entries for a case, with full
         error_detail
       - GetRetryHistory: all retry attempts for a task,
         ordered by attempt number
       - RequeueDLQEntry: move a DLQ task back to PENDING
         (must be transactional and publish TASK_REQUEUED event)

STEP 2 — FULL PRODUCTION IMPLEMENTATION
─────────────────────────────────────────
After the gap analysis, produce the complete production
implementation for every sub-capability listed above.

Output in this exact order:

  1. Schema migrations
     Sequential migration files following project conventions.
     Include both .up.sql and .down.sql. Add indexes for all
     query patterns used in Step 2 functions. No data loss on
     down migration — use soft deletes or archiving where needed.

  2. Go type definitions
     All new structs, typed enums, and error types. Place in the
     appropriate package. No duplication of existing types.

  3. Core logic functions
     Full implementations — no stubs. Every function must:
       - Accept ctx context.Context as first argument
       - Wrap errors with fmt.Errorf("functionName: %w", err)
       - Use structured logging on every non-trivial branch
       - Be safe to call concurrently (no shared mutable state
         outside of DB transactions)

  4. Integration into the existing event loop
     Show exactly where and how each new function hooks into
     the existing HandleEvent / worker poll loop. Produce a diff-
     style annotation — do not rewrite the entire event loop,
     only show the insertion points with surrounding context.

  5. Table-driven tests
     For each sub-capability: happy path, edge case, failure mode.
     Use sqlmock for DB interactions. Use testify/assert.
     Name tests as Test[SubCapabilityName]_[Scenario].

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables
- All state changes publish an outbox event in the same tx
- Error classification must be deterministic — same error code
  always maps to TRANSIENT or PERMANENT, never ambiguous
- DLQ entries are append-only — never update, only insert and
  soft-delete on requeue
- Retry backoff must be computed from the task definition
  config, not hardcoded
- Compensation tasks are created in the same transaction as
  the forward task failure
- Poison pill threshold is configurable per case_type, not
  hardcoded globally
- Do not analyse or implement any other capability dimension
═══════════════════════════════════════════════════════════════

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
  (e.g. 013_add_multitenancy.up.sql /
        013_add_multitenancy.down.sql)
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
All DDL migration files are in:
  C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

All current Go structs and key functions are in the project
folder open in this session.

Read and understand the full existing implementation before
producing any output. Do not restate or summarise what you
read — proceed directly to the gap analysis and then the
implementation.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
MULTI-TENANCY & PARTITIONING
═══════════════════════════════════════════════════════════════
Scope: analyse, then fully implement, the MULTI-TENANCY &
PARTITIONING capability dimension only.

Do not analyse or produce code for any other capability dimension.

STEP 1 — GAP ANALYSIS
──────────────────────
Compare the current implementation against the full enterprise-
grade capability set for multi-tenancy and partitioning.
Structure your gap analysis as a table with these columns:

  Sub-Capability | Current State | Gap | Severity (P1/P2/P3)

Sub-capabilities to cover (ALL of them):

  1. Tenant model and registry
     A tenant represents an isolated organisational unit —
     a bank, a broker, or a business division — sharing the
     same physical database but with strict data isolation.
     Store tenants in a tenants table:
       - tenant_id (PK, UUID)
       - tenant_code (unique, short identifier e.g. "ANZ", "CBA")
       - name (display name)
       - status: ACTIVE | SUSPENDED | OFFBOARDED
       - tier: STANDARD | PREMIUM | ENTERPRISE
           (controls rate limits, feature flags, and SLA targets)
       - config (JSONB — tenant-specific overrides: max_cases,
           max_concurrent_tasks, allowed_case_type_codes,
           feature_flags, sla_multiplier)
       - created_at, updated_at (timestamptz)
     Tenant status transitions:
       ACTIVE → SUSPENDED  (operator action; blocks new case
                            creation but preserves in-flight)
       SUSPENDED → ACTIVE  (operator reinstates)
       ACTIVE | SUSPENDED → OFFBOARDED (terminal; no new work,
                            data retained for audit)
     Produce typed sentinel errors:
       ErrTenantSuspended, ErrTenantOffboarded,
       ErrTenantNotFound, ErrCaseTypeForbidden

  2. Tenant context propagation
     Every request entering the engine carries a tenant_id.
     It must be extracted from the incoming context and
     threaded through every DB query, event, log line, and
     outbox publish for the lifetime of that request.
     Produce:

       // TenantFromContext extracts the tenant_id from ctx.
       // Returns ErrTenantNotFound if absent.
       func TenantFromContext(ctx context.Context) (string, error)

       // WithTenant returns a child context carrying tenant_id.
       func WithTenant(ctx context.Context, tenantID string) context.Context

       // TenantMiddleware validates the tenant_id from the
       // incoming request header or JWT claim, loads the tenant
       // record, asserts ACTIVE status, and injects into ctx.
       // Returns ErrTenantSuspended or ErrTenantOffboarded
       // if applicable.
       func TenantMiddleware(
           db   *sqlx.DB,
           next http.Handler,
       ) http.Handler

     Every structured log call throughout the engine must
     include tenant_id as a field. Every event published via
     PublishEvent must include tenant_id in its payload.
     Show the exact changes required to PublishEvent and the
     worker poll loop to enforce this.

  3. Row-level tenant isolation
     All core tables (cases, tasks, case_stage_transitions,
     events, notification_queue, task_dlq, and all reporting
     snapshot tables) must carry a tenant_id column with a
     NOT NULL FK to tenants. Every query in the engine must
     filter by tenant_id from context — no query may return
     rows belonging to a different tenant.
     Produce a query guard function:

       // AssertTenantScope appends AND tenant_id = $N to the
       // query being built and binds the tenant_id from ctx.
       // Returns an error if tenant_id is absent from ctx.
       func AssertTenantScope(
           ctx   context.Context,
           query string,
           args  []interface{},
       ) (string, []interface{}, error)

     This function is called at the top of every DB query
     function that touches tenant-scoped tables. Demonstrate
     its integration in at minimum: GetActiveCaseTypeVersion,
     ListCaseTypeVersions, and the worker poll query.
     Include a test that proves a query constructed without
     AssertTenantScope cannot compile without a linter warning
     — or, if that is not feasible in Go, produce a runtime
     assertion that panics in development mode and returns an
     error in production.

  4. Tenant-scoped CaseType catalogue
     A CaseType version may be:
       - GLOBAL: available to all tenants (e.g. a standard
         HOME_LOAN template published by the platform team)
       - TENANT_SPECIFIC: owned by one tenant; not visible
         to others
     Add a tenant_id column to case_type_versions:
       - NULL means GLOBAL
       - Non-null means TENANT_SPECIFIC, owned by that tenant
     Enforce in GetActiveCaseTypeVersion and
     ListCaseTypeVersions: a tenant may only resolve a
     CaseType if it is GLOBAL or owned by that tenant.
     When a tenant creates a Case against a GLOBAL CaseType,
     the Case is stamped with the tenant's tenant_id — the
     CaseType itself remains shared. Produce:

       func IsCaseTypeVisibleToTenant(
           ctx          context.Context,
           db           *sqlx.DB,
           caseTypeCode string,
           tenantID     string,
       ) (bool, error)

  5. Tenant rate limiting and capacity enforcement
     Each tenant has configurable capacity limits stored in
     tenants.config:
       - max_active_cases (integer — reject case creation
         if current active cases >= limit)
       - max_concurrent_tasks (integer — reject task claim
         if tenant's in-progress tasks >= limit)
       - max_cases_per_minute (integer — sliding window rate
         limit on case creation)
     Produce:

       func EnforceTenantCaseLimits(
           ctx      context.Context,
           db       *sqlx.DB,
           tenantID string,
       ) error  // returns ErrTenantCapacityExceeded if breached

       func EnforceTenantTaskLimits(
           ctx      context.Context,
           db       *sqlx.DB,
           tenantID string,
       ) error  // returns ErrTenantCapacityExceeded if breached

     Rate limit state is stored in a tenant_rate_limit_counters
     table (not in memory — must survive engine restarts):
       - tenant_id (FK)
       - window_start (timestamptz — truncated to the minute)
       - case_count (integer)
       PRIMARY KEY (tenant_id, window_start)
     Use INSERT ... ON CONFLICT DO UPDATE to increment
     atomically. Expired windows (> 5 minutes old) are pruned
     by a background cleanup job — produce that job.

  6. Tenant feature flags
     tenants.config carries a feature_flags JSONB object.
     Feature flags gate optional engine capabilities per tenant:
       - compensation_enabled (bool)
       - dlq_requeue_enabled (bool)
       - notification_enabled (bool)
       - sub_case_enabled (bool)
       - sla_enforcement_enabled (bool)
     Produce:

       func TenantFeatureEnabled(
           ctx         context.Context,
           db          *sqlx.DB,
           tenantID    string,
           featureFlag TenantFeatureFlag,
       ) (bool, error)

     Where TenantFeatureFlag is a typed string enum of the
     flags above. This function is called at the top of any
     code path that implements a flagged capability. Cache the
     tenant config in-process for a configurable TTL (default
     60 seconds) using a sync.Map — invalidate on tenant
     config update. Show how cache invalidation is triggered
     when a tenant config update event is published.

  7. Tenant data partitioning strategy
     At the scale target (100k cases / 1M events per day
     across all tenants), a single cases table with a
     tenant_id filter will degrade without a partitioning
     strategy. Produce a documented recommendation (as
     inline SQL comments) and implement ONE of:

       Option A — Postgres declarative partitioning
         PARTITION BY LIST (tenant_id) on cases and tasks.
         Produce the DDL for the parent table and two example
         partition templates. Show how new tenant onboarding
         creates a partition.

       Option B — Tenant shard column with partial indexes
         Keep a single table, add tenant_id to every index
         as the leading column. Produce all revised index
         definitions for cases and tasks.

     Choose the option that better fits the scale target and
     justify the choice in a comment block at the top of the
     migration file. Do not implement both — pick one and
     defend it.

  8. Tenant onboarding and offboarding workflows
     Tenant lifecycle is managed by operator-initiated
     functions, not automatic processes. Produce:

       func OnboardTenant(
           ctx    context.Context,
           db     *sqlx.DB,
           input  OnboardTenantInput,
       ) (Tenant, error)

     OnboardTenantInput contains: tenant_code, name, tier,
     config overrides. OnboardTenant must:
       a. Validate tenant_code is unique and matches pattern
          [A-Z0-9_]{2,20}
       b. Validate config overrides are within platform-defined
          maximums for the requested tier
       c. Insert the tenant record
       d. If Option A partitioning was chosen, create the
          partition for this tenant within the same transaction
       e. Publish TENANT_ONBOARDED event via outbox
       f. Seed any GLOBAL CaseType versions as visible to
          this tenant (insert into tenant_case_type_access
          if that join table is used in your design)

       func OffboardTenant(
           ctx          context.Context,
           db           *sqlx.DB,
           tenantID     string,
           offboardedBy string,
       ) error

     OffboardTenant must:
       a. Assert no active Cases remain (reject if any exist)
       b. Transition status to OFFBOARDED
       c. Revoke all feature flags (set all to false)
       d. Publish TENANT_OFFBOARDED event via outbox
       e. Do NOT delete data — all rows are retained for audit

  9. Cross-tenant isolation tests
     Produce a dedicated test file: multitenancy_isolation_test.go
     It must prove, for each core query function, that a query
     executed with tenant_id = "TENANT_A" in context never
     returns rows belonging to "TENANT_B", even if both tenants
     have cases with identical case_type_codes. Required tests:

       TestTenantIsolation_CaseQuery
       TestTenantIsolation_TaskQuery
       TestTenantIsolation_EventQuery
       TestTenantIsolation_NotificationQueue
       TestTenantIsolation_DLQQuery
       TestTenantIsolation_WorkerPollDoesNotCrossLeak
       TestTenantIsolation_CaseTypeVisibility_Global
       TestTenantIsolation_CaseTypeVisibility_TenantSpecific

     Each test must set up rows for two tenants, execute the
     query under one tenant's context, and assert zero rows
     from the other tenant are returned.

  10. Tenant observability
      Every Prometheus metric emitted by the engine must carry
      a tenant_id label. Add tenant_id to:
        - cases_created_total{tenant_id, case_type_code}
        - tasks_claimed_total{tenant_id, assigned_service}
        - tasks_failed_total{tenant_id, assigned_service, reason}
        - notifications_queued_total{tenant_id, channel}
        - sla_breached_total{tenant_id, case_type_code}
      Produce the updated metric registrations. Add a
      tenant_active_cases gauge that is updated by the
      MetricsRefreshJob (from the Reporting capability) per
      tenant. Produce an alert rule:
        tenant_active_cases > max_active_cases_config
      where max_active_cases_config is a recording rule derived
      from tenants.config.

STEP 2 — FULL PRODUCTION IMPLEMENTATION
─────────────────────────────────────────
After the gap analysis, produce the complete production
implementation for every sub-capability listed above.

Output in this exact order:

  1. Schema migrations
     Sequential .up.sql and .down.sql files following project
     conventions. Include:
       - tenants table with all columns and constraints
       - tenant_rate_limit_counters table with composite PK
       - tenant_id column additions to all core tables
       - NOT NULL + FK constraints on tenant_id for all
         tenant-scoped tables
       - Partial unique indexes where needed (e.g. one ACTIVE
         tenant per tenant_code)
       - All indexes revised to include tenant_id as leading
         column if Option B partitioning is chosen
       - Partition DDL if Option A is chosen
       - Inline comments justifying every non-obvious decision
     No data loss on down migration — tenant_id columns are
     dropped, not tables.

  2. Go type definitions
     All new structs, typed enums, sentinel errors, and context
     key types. Required at minimum:
       Tenant               (maps tenants table)
       TenantStatus         (ACTIVE | SUSPENDED | OFFBOARDED)
       TenantTier           (STANDARD | PREMIUM | ENTERPRISE)
       TenantConfig         (typed struct parsed from JSONB)
       TenantFeatureFlag    (typed string enum of all flags)
       OnboardTenantInput
       TenantRateLimitCounter
       ErrTenantSuspended   (sentinel, implements error)
       ErrTenantOffboarded  (sentinel, implements error)
       ErrTenantNotFound    (sentinel, implements error)
       ErrCaseTypeForbidden (sentinel, implements error)
       ErrTenantCapacityExceeded (sentinel, implements error)
       tenantContextKey     (unexported, for context injection)
     No duplication of existing types.

  3. Core logic functions
     Full implementations of all functions defined in
     sub-capabilities 1 through 10. No stubs. Every function:
       - Accepts ctx context.Context as first argument
       - Extracts tenant_id via TenantFromContext at the top
         of every tenant-scoped operation
       - Wraps errors with fmt.Errorf("functionName: %w", err)
       - Emits a structured log line with tenant_id on every
         non-trivial branch
       - Is safe to call concurrently across goroutines
     TenantFeatureEnabled must use the sync.Map cache with TTL.
     Show the cache invalidation path end-to-end.

  4. Integration into existing write paths and event loop
     Show exactly where tenant enforcement hooks into:
       - Case creation (capacity check + suspended/offboarded guard)
       - Task claim / worker poll (task limit check + tenant filter)
       - PublishEvent (tenant_id injected into every event payload)
       - The existing HandleEvent dispatcher (tenant extracted
         from event payload and injected into ctx before dispatch)
     Produce diff-style annotations with surrounding context —
     do not rewrite the entire event loop.

  5. Rate limit cleanup background job
     Full implementation of the job that prunes expired rows
     from tenant_rate_limit_counters (windows older than 5
     minutes). Show integration into main.go with a ticker
     and graceful shutdown via context cancellation.

  6. Table-driven tests
     For each sub-capability: happy path, edge case, failure
     mode. Naming: Test[SubCapabilityName]_[Scenario].
     Use sqlmock for DB. Use testify/assert.
     Required edge cases include:
       - TenantMiddleware with suspended tenant (returns
         ErrTenantSuspended, request blocked)
       - TenantMiddleware with offboarded tenant (returns
         ErrTenantOffboarded, request blocked)
       - EnforceTenantCaseLimits exactly at limit (rejected)
         and one below limit (allowed)
       - EnforceTenantCaseLimits with expired window present
         (expired window must not count toward current limit)
       - TenantFeatureEnabled cache hit (no DB call made)
       - TenantFeatureEnabled cache miss after TTL expiry
         (DB called, cache refreshed)
       - OnboardTenant with tenant_code violating pattern
         (rejected before DB insert)
       - OnboardTenant with config exceeding tier maximums
         (rejected with typed error listing violations)
       - OffboardTenant with active cases remaining
         (rejected — active cases listed in error)
       - AssertTenantScope called with no tenant in ctx
         (returns error, query not executed)
       - IsCaseTypeVisibleToTenant — GLOBAL type visible to
         any tenant; TENANT_SPECIFIC type invisible to other
         tenant (both cases tested)
     Plus the full isolation test file described in
     sub-capability 9.

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables beyond
  adding the tenant_id column with a NOT NULL FK
- Every DB query on a tenant-scoped table must filter by
  tenant_id — AssertTenantScope is called without exception
- A suspended tenant's in-flight Cases and Tasks continue
  processing — only new Case creation is blocked
- An offboarded tenant's data is never deleted — all rows
  retained with status = OFFBOARDED on the tenant record
- Rate limit counters are stored in Postgres, not in memory —
  they must survive engine restarts and work correctly across
  multiple engine instances
- TenantFeatureEnabled cache TTL is configurable, not
  hardcoded — pass as a parameter or engine config struct
- Partition strategy choice (Option A or B) must be justified
  in a comment block — do not implement both
- Cross-tenant data leakage is a P0 defect — the isolation
  tests in sub-capability 9 are non-negotiable
- tenant_id must appear in every Prometheus metric label set
  and every structured log line emitted by the engine
- Do not analyse or implement any other capability dimension
═══════════════════════════════════════════════════════════════

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
  (e.g. 014_add_integration.up.sql /
        014_add_integration.down.sql)
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
All DDL migration files are in:
  C:\MyProjects\MyLoanOriginationWorkflowApp\workflow-engine-go\db

All current Go structs and key functions are in the project
folder open in this session.

Read and understand the full existing implementation before
producing any output. Do not restate or summarise what you
read — proceed directly to the gap analysis and then the
implementation.

═══════════════════════════════════════════════════════════════
CAPABILITY UNDER REVIEW
INTEGRATION & EXTENSIBILITY
═══════════════════════════════════════════════════════════════
Scope: analyse, then fully implement, the INTEGRATION &
EXTENSIBILITY capability dimension only.

Do not analyse or produce code for any other capability dimension.

STEP 1 — GAP ANALYSIS
──────────────────────
Compare the current implementation against the full enterprise-
grade capability set for integration and extensibility. Structure
your gap analysis as a table with these columns:

  Sub-Capability | Current State | Gap | Severity (P1/P2/P3)

Sub-capabilities to cover (ALL of them):

  1. Webhook outbound integration
     Allow external systems to subscribe to engine events via
     webhooks. Store subscriptions in a webhook_subscriptions
     table:
       - subscription_id (PK, UUID)
       - tenant_id (FK — scoped per tenant)
       - subscriber_code (unique per tenant, e.g. "CRM_SYSTEM")
       - target_url (the HTTPS endpoint to call)
       - event_types (text[] — list of event types to receive,
           e.g. {"CASE_CREATED","TASK_COMPLETED"}; empty array
           means subscribe to ALL events)
       - signing_secret (stored encrypted — used to sign the
           outbound payload with HMAC-SHA256)
       - status: ACTIVE | PAUSED | FAILED
           FAILED is set automatically after max_failures
           consecutive delivery failures
       - max_failures (integer, default 5)
       - failure_count (integer, reset on successful delivery)
       - headers (JSONB — additional headers to include in
           every outbound request, e.g. API keys)
       - timeout_seconds (integer, default 10)
       - created_at, updated_at (timestamptz)

     Outbound webhook delivery uses the existing outbox pattern:
       - When an event is published via PublishEvent, the engine
         checks for ACTIVE webhook_subscriptions matching that
         event_type and tenant_id
       - For each matching subscription, insert one row into
         webhook_delivery_queue (separate from
         notification_queue) within the same transaction
       - A webhook dispatcher polls webhook_delivery_queue
         using SELECT FOR UPDATE SKIP LOCKED, delivers the
         payload, and records the result

     webhook_delivery_queue schema:
       - delivery_id (PK, UUID)
       - subscription_id (FK)
       - tenant_id (FK)
       - event_type
       - payload (JSONB — the full event payload)
       - status: PENDING | DELIVERED | FAILED | ABANDONED
       - attempts (integer)
       - max_attempts (integer — copied from subscription
           at enqueue time so config changes mid-flight are
           safe)
       - scheduled_at (timestamptz — supports delayed retry)
       - delivered_at (nullable timestamptz)
       - last_attempt_at (nullable timestamptz)
       - response_status_code (nullable integer)
       - response_body (nullable text, truncated to 1024 chars)
       - error_detail (JSONB)
       - created_at, updated_at (timestamptz)

     Payload signing: every outbound request must include a
     header X-Webhook-Signature: sha256=<hmac> computed over
     the raw JSON body using the subscription's signing_secret.
     The receiver can verify authenticity using the same secret.

     Produce:
       func EnqueueWebhookDeliveries(
           ctx      context.Context,
           tx       *sqlx.Tx,
           tenantID string,
           event    Event,
       ) error  // called inside PublishEvent transaction

       func DispatchWebhook(
           ctx      context.Context,
           db       *sqlx.DB,
           delivery WebhookDelivery,
           client   *http.Client,
       ) error  // called by the webhook dispatcher worker

       func SignWebhookPayload(
           secret  string,
           payload []byte,
       ) string  // returns "sha256=<hex>"

       type WebhookDispatcher struct {
           db       *sqlx.DB
           client   *http.Client
           logger   *slog.Logger
           interval time.Duration
       }

       func (d *WebhookDispatcher) Run(ctx context.Context) error

     Retry policy: exponential backoff, base 30 seconds,
     max 1 hour between attempts. After max_attempts, set
     status = ABANDONED and increment subscription.failure_count.
     If failure_count >= max_failures, set subscription
     status = FAILED and publish WEBHOOK_SUBSCRIPTION_FAILED
     event via outbox.

  2. Inbound event API (external task completion)
     External polyglot services complete Tasks by calling
     back into the engine. This is the primary integration
     seam for non-Go workers. Define the contract:

       func CompleteTaskFromExternal(
           ctx            context.Context,
           db             *sqlx.DB,
           idempotencyKey string,
           output         ExternalTaskCompletion,
       ) error

     ExternalTaskCompletion struct:
       - task_id (string)
       - assigned_service (string — must match task record)
       - status: COMPLETED | FAILED
       - output_payload (JSONB)
       - error_detail (JSONB, populated if status = FAILED)
       - completed_at (timestamptz)
       - idempotency_key (string — callers must supply this;
           duplicate calls with same key are silently accepted)

     This function must:
       a. Look up the Task by task_id, assert it belongs to
          the caller's tenant (from ctx)
       b. Assert assigned_service matches — reject with typed
          error ErrServiceMismatch if not
       c. Assert Task status is IN_PROGRESS — reject
          ErrInvalidTaskTransition if not
       d. Check idempotency_key against a completed_task_keys
          table — if already present, return nil (idempotent)
       e. Apply the completion: update Task status, write
          output_payload, record completed_at
       f. Insert idempotency_key into completed_task_keys
       g. Publish TASK_COMPLETED or TASK_FAILED event via outbox
       All steps in a single transaction.

  3. Plugin / handler registry
     The engine supports pluggable task handlers for Go-native
     workers. Handlers are registered at startup and invoked
     by the worker poll loop when a Task's assigned_service
     matches a registered handler name. Define:

       type TaskHandler interface {
           ServiceName() string
           Handle(ctx context.Context, task Task) (TaskResult, error)
       }

       type TaskResult struct {
           Status        TaskStatus  // COMPLETED | FAILED
           OutputPayload []byte      // raw JSON
           ErrorDetail   []byte      // raw JSON, populated if FAILED
       }

       type HandlerRegistry struct {
           handlers map[string]TaskHandler
           mu       sync.RWMutex
       }

       func NewHandlerRegistry() *HandlerRegistry

       func (r *HandlerRegistry) Register(h TaskHandler) error
         // Returns ErrHandlerAlreadyRegistered if ServiceName
         // is already registered. Registration is only allowed
         // before the engine starts (enforce with a started bool
         // guarded by the mutex).

       func (r *HandlerRegistry) Lookup(
           serviceName string,
       ) (TaskHandler, bool)

       func (r *HandlerRegistry) MustRegister(h TaskHandler)
         // Panics if registration fails — intended for use in
         // main.go init blocks only.

     The worker poll loop must:
       - After claiming a Task, call registry.Lookup(task.AssignedService)
       - If found: invoke the handler, apply the result via the
         existing task completion path
       - If not found: leave the Task claimed but log a warning
         and release the claim — external services will complete
         it via CompleteTaskFromExternal
     Show the exact integration point in the worker poll loop
     as a diff-style annotation.

  4. Inbound webhook / event ingestion endpoint
     External systems push events into the engine to trigger
     workflow transitions. These are not task completions —
     they are domain events from upstream systems (e.g.
     "CREDIT_BUREAU_RESULT_RECEIVED"). Define the ingestion
     contract:

       func IngestExternalEvent(
           ctx   context.Context,
           db    *sqlx.DB,
           input ExternalEventInput,
       ) error

     ExternalEventInput struct:
       - tenant_id (string — must match ctx tenant)
       - case_id (string — the Case this event relates to)
       - event_type (string)
       - source_system (string — e.g. "CREDIT_BUREAU")
       - payload (JSONB)
       - idempotency_key (string)
       - occurred_at (timestamptz)

     This function must:
       a. Validate tenant_id matches ctx tenant
       b. Validate case_id exists and belongs to tenant
       c. Check idempotency_key against ingested_event_keys
          table — duplicate keys return nil silently
       d. Insert into the engine's events table with
          source = EXTERNAL and the provided payload
       e. Insert idempotency_key into ingested_event_keys
       f. The existing HandleEvent loop picks it up from there
          — IngestExternalEvent does NOT call HandleEvent
          directly
       All steps in a single transaction.

     Store ingested_event_keys:
       - idempotency_key (PK)
       - tenant_id (FK)
       - case_id
       - received_at (timestamptz)
       - expires_at (timestamptz — keys older than 7 days
           are pruned by a cleanup job)

  5. External service registry
     Track every polyglot service that integrates with the
     engine. Store in an external_services table:
       - service_id (PK, UUID)
       - tenant_id (FK)
       - service_code (unique per tenant — matches
           task.assigned_service values)
       - display_name
       - protocol: HTTP_CALLBACK | POLLING | EVENT_DRIVEN
           HTTP_CALLBACK: service calls CompleteTaskFromExternal
           POLLING: service polls the engine's task queue API
           EVENT_DRIVEN: service publishes via IngestExternalEvent
       - health_check_url (nullable — if set, engine pings
           this URL on a schedule and records status)
       - status: ACTIVE | DEGRADED | OFFLINE | DECOMMISSIONED
       - last_health_check_at (nullable timestamptz)
       - last_successful_at (nullable timestamptz)
       - metadata (JSONB — protocol-specific config, e.g.
           polling_interval_seconds for POLLING services)
       - created_at, updated_at (timestamptz)

     Produce a health check job:
       type ServiceHealthChecker struct {
           db       *sqlx.DB
           client   *http.Client
           logger   *slog.Logger
           interval time.Duration
       }

       func (s *ServiceHealthChecker) Run(ctx context.Context) error

     The job polls all ACTIVE and DEGRADED services with a
     non-null health_check_url on the configured interval.
     On HTTP 200: set status = ACTIVE, update last_successful_at.
     On any non-200 or timeout: set status = DEGRADED.
     After 3 consecutive DEGRADED results: set status = OFFLINE
     and publish SERVICE_OFFLINE event via outbox.
     Status transitions back to ACTIVE only on a successful
     health check after being DEGRADED or OFFLINE.

  6. Idempotency key management
     The engine uses multiple idempotency key stores:
       - completed_task_keys (sub-capability 2)
       - ingested_event_keys (sub-capability 4)
     Both follow the same pattern. Consolidate into a single
     reusable function:

       func CheckAndRecordIdempotencyKey(
           ctx       context.Context,
           tx        *sqlx.Tx,
           keyspace  IdempotencyKeyspace,
           key       string,
           tenantID  string,
           expiresAt time.Time,
       ) (isDuplicate bool, err error)

     Where IdempotencyKeyspace is a typed enum:
       TASK_COMPLETION | EXTERNAL_EVENT_INGESTION |
       WEBHOOK_DELIVERY

     This function must:
       - Attempt INSERT into idempotency_keys table (single
         unified table with a keyspace column)
       - On unique constraint violation: return isDuplicate=true
       - On success: return isDuplicate=false
       - Never return an error on duplicate — duplicates are
         expected and handled by the caller
     The unified idempotency_keys table:
       - keyspace (IdempotencyKeyspace enum)
       - key (text)
       - tenant_id (FK)
       - reference_id (nullable — task_id, case_id, etc.)
       - created_at (timestamptz)
       - expires_at (timestamptz)
       PRIMARY KEY (keyspace, key)
     A cleanup job prunes expired keys daily — produce that job.

  7. Integration event catalogue
     Maintain a machine-readable catalogue of all event types
     the engine can emit or consume. Store in an
     event_type_catalogue table:
       - event_type_code (PK, e.g. "CASE_CREATED")
       - direction: EMITTED | CONSUMED | BOTH
       - description (text)
       - payload_schema (JSONB — JSON Schema describing the
           event payload structure)
       - introduced_in_version (text — engine version when
           this event type was added)
       - deprecated_at (nullable timestamptz)
       - example_payload (JSONB)

     Produce:
       func ValidateEventPayload(
           ctx       context.Context,
           db        *sqlx.DB,
           eventType string,
           payload   []byte,
       ) error  // validates payload against catalogue schema

       func ListEventTypes(
           ctx       context.Context,
           db        *sqlx.DB,
           direction EventDirection, // EMITTED | CONSUMED | BOTH
           page, size int,
       ) ([]EventTypeCatalogueEntry, int, error)

     ValidateEventPayload is called by IngestExternalEvent
     before inserting the event — reject unknown event types
     or payloads that fail schema validation. Use
     github.com/santhosh-tekuri/jsonschema/v5 for validation.
     Do not use encoding/json alone — the payload schema in
     the catalogue is a full JSON Schema document.

  8. Integration audit log
     All inbound and outbound integration activity is recorded
     in an integration_audit_log table (append-only):
       - audit_id (PK, UUID)
       - tenant_id (FK)
       - direction: INBOUND | OUTBOUND
       - integration_type: WEBHOOK | EXTERNAL_TASK_COMPLETION |
                           EXTERNAL_EVENT_INGESTION |
                           HEALTH_CHECK
       - source_or_target (string — service_code or target_url)
       - event_type (nullable)
       - case_id (nullable FK)
       - task_id (nullable FK)
       - status: SUCCESS | FAILURE | DUPLICATE_REJECTED
       - request_payload (JSONB — truncated to 4096 bytes
           if larger; never store secrets or signing keys)
       - response_payload (JSONB — nullable, truncated to 1024)
       - duration_ms (integer)
       - occurred_at (timestamptz)

     Produce:
       func RecordIntegrationAudit(
           ctx   context.Context,
           tx    *sqlx.Tx,
           entry IntegrationAuditEntry,
       ) error  // called inside the same tx as the operation

     RecordIntegrationAudit is called by:
       - DispatchWebhook (after delivery attempt)
       - CompleteTaskFromExternal (after completion or rejection)
       - IngestExternalEvent (after ingestion or rejection)
       - ServiceHealthChecker (after each health check)
     The audit log must never be the cause of a transaction
     rollback — if RecordIntegrationAudit itself fails, log
     the error and continue (best-effort audit).

  9. Integration observability
     Register the following Prometheus metrics. All metrics
     carry tenant_id and, where applicable, service_code or
     event_type labels:

       webhook_deliveries_total{tenant_id, event_type, status}
       webhook_delivery_latency_seconds{tenant_id, event_type}
       webhook_subscription_failures_total{tenant_id,
                                           subscriber_code}
       external_task_completions_total{tenant_id,
                                       assigned_service, status}
       external_events_ingested_total{tenant_id, event_type,
                                      source_system}
       external_events_rejected_total{tenant_id, event_type,
                                      reason}
       service_health_status{tenant_id, service_code}
           // gauge: 1=ACTIVE, 0.5=DEGRADED, 0=OFFLINE
       idempotency_duplicates_total{tenant_id, keyspace}

     Produce alert rules:
       - webhook_deliveries_total{status="ABANDONED"} > 10
         within 5 minutes → WEBHOOK_DELIVERY_DEGRADED
       - service_health_status{} == 0 for 3 minutes →
         SERVICE_OFFLINE_ALERT
       - external_events_rejected_total > 50 within 1 minute →
         EVENT_INGESTION_DEGRADED

  10. Integration configuration query functions
      Produce read-only query functions for use by operator
      dashboards and API handlers:

        func GetWebhookSubscription(
            ctx            context.Context,
            db             *sqlx.DB,
            subscriptionID string,
            tenantID       string,
        ) (WebhookSubscription, error)

        func ListWebhookSubscriptions(
            ctx      context.Context,
            db       *sqlx.DB,
            tenantID string,
            status   WebhookSubscriptionStatus, // empty = all
            page, size int,
        ) ([]WebhookSubscription, int, error)

        func GetWebhookDeliveryHistory(
            ctx            context.Context,
            db             *sqlx.DB,
            subscriptionID string,
            tenantID       string,
            page, size     int,
        ) ([]WebhookDelivery, int, error)

        func ListExternalServices(
            ctx      context.Context,
            db       *sqlx.DB,
            tenantID string,
            status   ExternalServiceStatus, // empty = all
            page, size int,
        ) ([]ExternalService, int, error)

        func GetIntegrationAuditLog(
            ctx      context.Context,
            db       *sqlx.DB,
            tenantID string,
            filters  IntegrationAuditFilters,
            page, size int,
        ) ([]IntegrationAuditEntry, int, error)

      IntegrationAuditFilters struct:
        - CaseID (nullable)
        - TaskID (nullable)
        - Direction (nullable)
        - IntegrationType (nullable)
        - From, To (time.Time — required, max range 30 days)

STEP 2 — FULL PRODUCTION IMPLEMENTATION
─────────────────────────────────────────
After the gap analysis, produce the complete production
implementation for every sub-capability listed above.

Output in this exact order:

  1. Schema migrations
     Sequential .up.sql and .down.sql files following project
     conventions. Include:
       - webhook_subscriptions table
       - webhook_delivery_queue table
       - external_services table
       - idempotency_keys unified table with composite PK
       - event_type_catalogue table
       - integration_audit_log table
       - All indexes for query patterns used in Step 2,
         with tenant_id as leading column on every index
       - Partial indexes where appropriate (e.g.
         status IN ('PENDING') on webhook_delivery_queue)
       - Inline comments on all non-obvious constraints
     No data loss on down migration.

  2. Go type definitions
     All new structs, typed enums, interfaces, and sentinel
     errors. Required at minimum:
       WebhookSubscription
       WebhookSubscriptionStatus  (ACTIVE | PAUSED | FAILED)
       WebhookDelivery
       WebhookDeliveryStatus      (PENDING | DELIVERED |
                                   FAILED | ABANDONED)
       ExternalTaskCompletion
       ExternalEventInput
       ExternalService
       ExternalServiceStatus      (ACTIVE | DEGRADED |
                                   OFFLINE | DECOMMISSIONED)
       ExternalServiceProtocol    (HTTP_CALLBACK | POLLING |
                                   EVENT_DRIVEN)
       TaskHandler                (interface)
       TaskResult
       HandlerRegistry
       IdempotencyKeyspace        (typed enum)
       EventTypeCatalogueEntry
       EventDirection             (EMITTED | CONSUMED | BOTH)
       IntegrationAuditEntry
       IntegrationAuditFilters
       IntegrationDirection       (INBOUND | OUTBOUND)
       IntegrationType            (typed enum of all types)
       ErrHandlerAlreadyRegistered (sentinel)
       ErrServiceMismatch          (sentinel)
       ErrInvalidTaskTransition    (sentinel — if not already
                                    defined by error handling
                                    capability)
     No duplication of existing types.

  3. Core logic functions
     Full implementations of all functions defined in
     sub-capabilities 1 through 10. No stubs. Every function:
       - Accepts ctx context.Context as first argument
       - Extracts tenant_id via TenantFromContext where
         tenant-scoped
       - Wraps errors with fmt.Errorf("functionName: %w", err)
       - Emits structured log lines with tenant_id, case_id,
         task_id where available
       - Is safe to call concurrently
     SignWebhookPayload must use crypto/hmac and crypto/sha256
     from the standard library only — no third-party crypto.
     ValidateEventPayload must use the jsonschema library —
     do not implement schema validation by hand.

  4. Integration into the existing event loop and write paths
     Show exactly where integration hooks into:
       - PublishEvent: call EnqueueWebhookDeliveries inside
         the same transaction, after the outbox insert
       - Worker poll loop: call registry.Lookup after claiming
         a Task; invoke handler or leave for external completion
       - HandleEvent: IngestExternalEvent routes external events
         into the existing dispatcher without calling it directly
     Produce diff-style annotations with surrounding context —
     do not rewrite the entire event loop.

  5. Background job implementations
     Full implementations of:
       - WebhookDispatcher.Run (poll, deliver, retry, abandon)
       - ServiceHealthChecker.Run (poll services, update status)
       - Idempotency key cleanup job (prune expired keys daily)
       - Ingested event key cleanup job (prune keys > 7 days)
     All jobs must accept ctx context.Context and shut down
     cleanly on cancellation. Show registration in main.go
     with tickers and graceful shutdown.

  6. Table-driven tests
     For each sub-capability: happy path, edge case, failure
     mode. Naming: Test[SubCapabilityName]_[Scenario].
     Use sqlmock for DB. Use testify/assert.
     Required edge cases:
       - EnqueueWebhookDeliveries with zero matching
         subscriptions (no rows inserted, no error)
       - EnqueueWebhookDeliveries with PAUSED subscription
         (no row inserted — PAUSED subscriptions are skipped)
       - DispatchWebhook with non-200 response (retry
         scheduled, attempts incremented, response_status_code
         recorded)
       - DispatchWebhook after max_attempts reached (status
         set to ABANDONED, subscription failure_count
         incremented)
       - DispatchWebhook with failure_count reaching
         max_failures (subscription status set to FAILED,
         WEBHOOK_SUBSCRIPTION_FAILED event published)
       - CompleteTaskFromExternal with duplicate idempotency_key
         (returns nil, no state change, audit log records
         DUPLICATE_REJECTED)
       - CompleteTaskFromExternal with wrong assigned_service
         (returns ErrServiceMismatch)
       - IngestExternalEvent with unknown event_type (rejected
         by ValidateEventPayload before insert)
       - IngestExternalEvent with payload failing JSON Schema
         validation (rejected with structured error listing
         all violations)
       - HandlerRegistry.Register called after engine started
         (returns ErrHandlerAlreadyRegistered)
       - ServiceHealthChecker: service returns 200 after two
         DEGRADED checks (status back to ACTIVE, not yet
         OFFLINE)
       - ServiceHealthChecker: service returns non-200 three
         consecutive times (status → OFFLINE, SERVICE_OFFLINE
         event published)
       - CheckAndRecordIdempotencyKey concurrent calls with
         same key and keyspace (exactly one succeeds,
         remainder return isDuplicate=true, no error)

═══════════════════════════════════════════════════════════════
CONSTRAINTS — NEVER VIOLATE THESE
═══════════════════════════════════════════════════════════════
- No breaking changes to tasks, cases, or events tables beyond
  adding foreign key references from new integration tables
- All webhook deliveries go through webhook_delivery_queue —
  no direct HTTP calls inside PublishEvent or HandleEvent
- EnqueueWebhookDeliveries is always called inside the same
  transaction as PublishEvent — delivery queue rows and outbox
  rows are committed atomically
- Webhook signing_secret is never logged, never included in
  integration_audit_log, and never returned in query responses
  — treat it as a write-only field after creation
- CompleteTaskFromExternal and IngestExternalEvent are fully
  idempotent — identical calls with the same idempotency_key
  must return nil without side effects
- HandlerRegistry.Register is only permitted before the engine
  starts — enforce with a mutex-guarded started flag
- ValidateEventPayload uses github.com/santhosh-tekuri/jsonschema/v5
  — do not substitute another library or implement by hand
- RecordIntegrationAudit failures must never cause a
  transaction rollback — log and continue on audit failure
- All query functions returning potentially unbounded result
  sets require pagination — max page size 200
- service_health_status transitions to OFFLINE only after
  3 consecutive failures — a single failure sets DEGRADED
- idempotency_keys table is the single unified store for all
  key namespaces — do not create separate tables per keyspace
- Do not analyse or implement any other capability dimension
═══════════════════════════════════════════════════════════════