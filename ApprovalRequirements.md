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