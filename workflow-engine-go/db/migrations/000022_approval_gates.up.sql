-- 000022_approval_gates.up.sql
-- Approval & decision gates: gate definitions, requests, chains, authority,
-- immutable audit/history, and rework loop support.

-- ---------------------------------------------------------------------------
-- 0) Supporting user directory for role/reporting chain resolution.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id              TEXT            PRIMARY KEY,
    full_name       TEXT            NOT NULL,
    role_code       VARCHAR(100)    NOT NULL,
    manager_id      TEXT            REFERENCES users(id) ON DELETE SET NULL,
    status          VARCHAR(20)     NOT NULL DEFAULT 'ACTIVE'
                        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now()
);

COMMENT ON TABLE users IS 'User directory for approval routing, role resolution, and reporting-chain traversal.';
COMMENT ON COLUMN users.role_code IS 'Primary role used by ROLE_BASED approver selection.';
COMMENT ON COLUMN users.manager_id IS 'Direct supervisor used by REPORTING_CHAIN approver selection and escalation.';

CREATE INDEX IF NOT EXISTS idx_users_role_status
    ON users (role_code, status)
    WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_users_manager
    ON users (manager_id)
    WHERE manager_id IS NOT NULL;

DROP TRIGGER IF EXISTS users_updated_at ON users;
CREATE TRIGGER users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 1) Approval gates (one gate per approval-enabled task instance)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS approval_gates (
    id                          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id                     UUID            NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    case_id                     UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    approval_policy             VARCHAR(30)     NOT NULL
                                    CHECK (approval_policy IN (
                                        'SINGLE_APPROVER',
                                        'ALL_MUST_APPROVE',
                                        'ANY_CAN_APPROVE',
                                        'MAJORITY',
                                        'CONSENSUS'
                                    )),
    required_approver_count     INT             NOT NULL DEFAULT 1
                                    CHECK (required_approver_count > 0),
    approver_selection          VARCHAR(30)     NOT NULL
                                    CHECK (approver_selection IN (
                                        'EXPLICIT_LIST',
                                        'ROLE_BASED',
                                        'REPORTING_CHAIN',
                                        'DYNAMIC_RULE'
                                    )),
    approvers                   JSONB           NOT NULL DEFAULT '[]',
    authority_limit             NUMERIC(18,2),
    approval_amount             NUMERIC(18,2),
    approval_timeout_hours      NUMERIC(10,2)   NOT NULL DEFAULT 24
                                    CHECK (approval_timeout_hours > 0),
    on_timeout_action           VARCHAR(20)     NOT NULL DEFAULT 'ESCALATE'
                                    CHECK (on_timeout_action IN ('AUTO_APPROVE', 'AUTO_REJECT', 'ESCALATE')),
    rejection_behavior          VARCHAR(20)     NOT NULL DEFAULT 'SEND_TO_REWORK'
                                    CHECK (rejection_behavior IN ('SEND_TO_REWORK', 'TERMINAL_FAIL')),
    rework_target_stage_code    VARCHAR(100),
    fallback_supervisor_role    VARCHAR(100),
    dynamic_rule_expression     TEXT,
    chain_definition            JSONB,
    gate_status                 VARCHAR(40)     NOT NULL DEFAULT 'PENDING'
                                    CHECK (gate_status IN (
                                        'PENDING',
                                        'ACTIVE',
                                        'SATISFIED',
                                        'FAILED',
                                        'REJECTED',
                                        'REJECTED_REWORK_INITIATED',
                                        'EXPIRED',
                                        'CANCELLED'
                                    )),
    opened_at                   TIMESTAMPTZ,
    closed_at                   TIMESTAMPTZ,
    version                     INT             NOT NULL DEFAULT 1,
    created_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT uq_approval_gates_task UNIQUE (task_id),
    CONSTRAINT chk_approval_gate_approvers_array CHECK (jsonb_typeof(approvers) = 'array'),
    CONSTRAINT chk_approval_gate_chain_json CHECK (chain_definition IS NULL OR jsonb_typeof(chain_definition) = 'array'),
    CONSTRAINT chk_approval_gate_amount_non_negative CHECK (approval_amount IS NULL OR approval_amount >= 0),
    CONSTRAINT chk_approval_gate_authority_non_negative CHECK (authority_limit IS NULL OR authority_limit >= 0),
    CONSTRAINT chk_approval_gate_rework_target_required
        CHECK (
            rejection_behavior <> 'SEND_TO_REWORK'
            OR rework_target_stage_code IS NOT NULL
        )
);

COMMENT ON TABLE approval_gates IS 'Runtime approval gate spawned for approval-enabled tasks.';
COMMENT ON COLUMN approval_gates.approvers IS 'Approver seed list. EXPLICIT_LIST stores user IDs; ROLE_BASED stores roles; DYNAMIC_RULE can seed derived values.';
COMMENT ON COLUMN approval_gates.chain_definition IS 'Tiered approval_chain snapshot from case_type config; immutable after gate creation.';
COMMENT ON COLUMN approval_gates.gate_status IS 'Gate lifecycle status. REJECTED_REWORK_INITIATED preserves history after regression.';

CREATE INDEX IF NOT EXISTS idx_approval_gates_case
    ON approval_gates (case_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_approval_gates_status
    ON approval_gates (gate_status, opened_at)
    WHERE gate_status IN ('PENDING', 'ACTIVE');

CREATE INDEX IF NOT EXISTS idx_approval_gates_task_fk
    ON approval_gates (task_id);

DROP TRIGGER IF EXISTS approval_gates_updated_at ON approval_gates;
CREATE TRIGGER approval_gates_updated_at
    BEFORE UPDATE ON approval_gates
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 2) Approval requests (one row per approver per gate/tier)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS approval_requests (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_gate_id        UUID            NOT NULL REFERENCES approval_gates(id) ON DELETE CASCADE,
    approver_id             TEXT            NOT NULL,
    tier                    INT,
    status                  VARCHAR(20)     NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXPIRED', 'DELEGATED')),
    decision                TEXT,
    evidence_refs           JSONB           NOT NULL DEFAULT '[]',
    decided_at              TIMESTAMPTZ,
    decided_by              TEXT,
    expires_at              TIMESTAMPTZ     NOT NULL,
    delegated_to_id         TEXT,
    delegated_at            TIMESTAMPTZ,
    delegation_chain        JSONB           NOT NULL DEFAULT '[]',
    version                 INT             NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_approval_request_evidence_array CHECK (jsonb_typeof(evidence_refs) = 'array'),
    CONSTRAINT chk_approval_request_chain_array CHECK (jsonb_typeof(delegation_chain) = 'array'),
    CONSTRAINT chk_approval_request_decision_terminal
        CHECK (
            (status NOT IN ('APPROVED', 'REJECTED'))
            OR (decision IS NOT NULL AND decided_at IS NOT NULL)
        ),
    CONSTRAINT chk_approval_request_delegate_requires_target
        CHECK (
            status <> 'DELEGATED'
            OR delegated_to_id IS NOT NULL
        )
);

COMMENT ON TABLE approval_requests IS 'Per-approver decision rows for each approval gate and tier.';
COMMENT ON COLUMN approval_requests.delegation_chain IS 'Append-only chain of delegation hops as JSON array.';

CREATE INDEX IF NOT EXISTS idx_approval_requests_gate
    ON approval_requests (approval_gate_id, status, tier);

CREATE INDEX IF NOT EXISTS idx_approval_requests_approver_pending
    ON approval_requests (approver_id, status, expires_at)
    WHERE status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_approval_requests_expiry_sweep
    ON approval_requests (expires_at, status)
    WHERE status = 'PENDING';

CREATE INDEX IF NOT EXISTS idx_approval_requests_gate_fk
    ON approval_requests (approval_gate_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_approval_requests_gate_approver_tier_pending
    ON approval_requests (approval_gate_id, approver_id, COALESCE(tier, 0))
    WHERE status = 'PENDING';

DROP TRIGGER IF EXISTS approval_requests_updated_at ON approval_requests;
CREATE TRIGGER approval_requests_updated_at
    BEFORE UPDATE ON approval_requests
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 3) Approval chain runtime state
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS approval_chain_state (
    id                          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id                     UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    approval_gate_id            UUID            NOT NULL REFERENCES approval_gates(id) ON DELETE CASCADE,
    approval_chain_definition   JSONB           NOT NULL,
    current_tier                INT             NOT NULL DEFAULT 1,
    tier_status                 VARCHAR(20)     NOT NULL DEFAULT 'PENDING'
                                    CHECK (tier_status IN ('PENDING', 'APPROVED', 'REJECTED', 'SKIPPED')),
    tier_started_at             TIMESTAMPTZ,
    tier_completed_at           TIMESTAMPTZ,
    chain_status                VARCHAR(20)     NOT NULL DEFAULT 'PENDING'
                                    CHECK (chain_status IN ('PENDING', 'IN_PROGRESS', 'COMPLETED', 'FAILED')),
    created_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT uq_approval_chain_state_gate UNIQUE (approval_gate_id),
    CONSTRAINT chk_approval_chain_definition_array CHECK (jsonb_typeof(approval_chain_definition) = 'array')
);

COMMENT ON TABLE approval_chain_state IS 'Mutable runtime state for tiered approval chains.';

CREATE INDEX IF NOT EXISTS idx_approval_chain_state_case
    ON approval_chain_state (case_id, chain_status);

CREATE INDEX IF NOT EXISTS idx_approval_chain_state_gate_fk
    ON approval_chain_state (approval_gate_id);

DROP TRIGGER IF EXISTS approval_chain_state_updated_at ON approval_chain_state;
CREATE TRIGGER approval_chain_state_updated_at
    BEFORE UPDATE ON approval_chain_state
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) Authority limits and immutable history
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS user_authority (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 TEXT            NOT NULL,
    authority_type          VARCHAR(50)     NOT NULL,
    max_amount              NUMERIC(18,2)   NOT NULL CHECK (max_amount >= 0),
    granted_by              TEXT            NOT NULL,
    granted_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    expires_at              TIMESTAMPTZ,
    revoked_at              TIMESTAMPTZ,
    revoked_by              TEXT,
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_user_authority_expiry CHECK (expires_at IS NULL OR expires_at > granted_at),
    CONSTRAINT chk_user_authority_revoke CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);

COMMENT ON TABLE user_authority IS 'Current effective delegated authority grants used during approver selection.';

CREATE INDEX IF NOT EXISTS idx_user_authority_active_lookup
    ON user_authority (authority_type, user_id, max_amount)
    WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_user_authority_expiry
    ON user_authority (expires_at)
    WHERE revoked_at IS NULL AND expires_at IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_authority_active
    ON user_authority (user_id, authority_type)
    WHERE revoked_at IS NULL;

DROP TRIGGER IF EXISTS user_authority_updated_at ON user_authority;
CREATE TRIGGER user_authority_updated_at
    BEFORE UPDATE ON user_authority
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

CREATE TABLE IF NOT EXISTS authority_limit_history (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 TEXT            NOT NULL,
    authority_type          VARCHAR(50)     NOT NULL,
    max_amount              NUMERIC(18,2)   NOT NULL CHECK (max_amount >= 0),
    change_type             VARCHAR(20)     NOT NULL
                                CHECK (change_type IN ('GRANTED', 'REVOKED', 'MODIFIED')),
    changed_by              TEXT            NOT NULL,
    changed_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    reason                  TEXT,
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now()
);

COMMENT ON TABLE authority_limit_history IS 'Append-only history of authority grants/revocations/modifications.';

CREATE INDEX IF NOT EXISTS idx_authority_limit_history_user
    ON authority_limit_history (user_id, authority_type, changed_at DESC);

DROP TRIGGER IF EXISTS authority_limit_history_updated_at ON authority_limit_history;
CREATE TRIGGER authority_limit_history_updated_at
    BEFORE UPDATE ON authority_limit_history
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 5) Immutable approval audit trail
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS approval_audit_log (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_request_id     UUID            NOT NULL REFERENCES approval_requests(id) ON DELETE CASCADE,
    event_type              VARCHAR(20)     NOT NULL
                                CHECK (event_type IN (
                                    'REQUESTED',
                                    'APPROVED',
                                    'REJECTED',
                                    'DELEGATED',
                                    'EXPIRED',
                                    'AUTO_APPROVED',
                                    'AUTO_REJECTED',
                                    'ESCALATED'
                                )),
    actor_id                TEXT            NOT NULL,
    decision_text           TEXT,
    evidence_refs           JSONB           NOT NULL DEFAULT '[]',
    previous_state          VARCHAR(20),
    new_state               VARCHAR(20),
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_approval_audit_evidence_array CHECK (jsonb_typeof(evidence_refs) = 'array')
);

COMMENT ON TABLE approval_audit_log IS 'Append-only immutable audit trail for all approval request actions.';

CREATE INDEX IF NOT EXISTS idx_approval_audit_request
    ON approval_audit_log (approval_request_id, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_approval_audit_event_type
    ON approval_audit_log (event_type, created_at DESC);

DROP TRIGGER IF EXISTS approval_audit_log_updated_at ON approval_audit_log;
CREATE TRIGGER approval_audit_log_updated_at
    BEFORE UPDATE ON approval_audit_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 6) Extend existing tables (cases/tasks)
-- ---------------------------------------------------------------------------
ALTER TABLE cases
    ADD COLUMN IF NOT EXISTS rework_count INT NOT NULL DEFAULT 0 CHECK (rework_count >= 0),
    ADD COLUMN IF NOT EXISTS max_rework_attempts INT NOT NULL DEFAULT 3 CHECK (max_rework_attempts >= 0);

COMMENT ON COLUMN cases.rework_count IS
'Number of SEND_TO_REWORK loops executed for this case.';
COMMENT ON COLUMN cases.max_rework_attempts IS
'Cap on allowed rework loops before terminal rejection.';

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS approval_gate_id UUID,
    ADD COLUMN IF NOT EXISTS approval_amount NUMERIC(18,2);

COMMENT ON COLUMN tasks.requires_approval IS
'Denormalized flag from task definition; true means task execution is gated by approval policy.';
COMMENT ON COLUMN tasks.approval_gate_id IS
'Current approval gate for this task instance (nullable if no approval required).';
COMMENT ON COLUMN tasks.approval_amount IS
'Monetary amount used for authority-limit filtering during approver selection.';

ALTER TABLE tasks
    ADD CONSTRAINT fk_tasks_approval_gate
        FOREIGN KEY (approval_gate_id) REFERENCES approval_gates(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_requires_approval
    ON tasks (requires_approval, status)
    WHERE requires_approval = TRUE;

CREATE INDEX IF NOT EXISTS idx_tasks_approval_gate_fk
    ON tasks (approval_gate_id)
    WHERE approval_gate_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_approval_amount
    ON tasks (approval_amount)
    WHERE approval_amount IS NOT NULL;

-- allow terminal REJECTED status for approval terminal-fail and max-rework paths.
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check
    CHECK (status IN ('OPEN', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED', 'SUSPENDED', 'REJECTED', 'CLONED'));

-- ---------------------------------------------------------------------------
-- 7) Append-only guards for immutable tables
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION trg_reject_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'append-only table: % does not allow %', TG_TABLE_NAME, TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS authority_limit_history_no_update_delete ON authority_limit_history;
CREATE TRIGGER authority_limit_history_no_update_delete
    BEFORE UPDATE OR DELETE ON authority_limit_history
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_mutation();

DROP TRIGGER IF EXISTS approval_audit_log_no_update_delete ON approval_audit_log;
CREATE TRIGGER approval_audit_log_no_update_delete
    BEFORE UPDATE OR DELETE ON approval_audit_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_reject_mutation();

-- ---------------------------------------------------------------------------
-- 8) Outbox indexes for approval high-volume events
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_outbox_approval_events
    ON events_outbox (event_type, status, created_at)
    WHERE event_type IN (
        'APPROVAL_GATE_CREATED',
        'APPROVAL_REQUESTED',
        'APPROVAL_GRANTED',
        'APPROVAL_REJECTED',
        'APPROVAL_DELEGATED',
        'APPROVAL_EXPIRED',
        'APPROVAL_GATE_SATISFIED',
        'APPROVAL_GATE_FAILED',
        'CASE_SENT_TO_REWORK',
        'CASE_REJECTED',
        'CASE_MAX_REWORK_EXCEEDED',
        'NO_ELIGIBLE_APPROVER'
    )
      AND status IN ('PENDING', 'PROCESSING');
