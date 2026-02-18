-- 000022_approval_gates.down.sql
-- Rollback Approval & decision gates schema.

-- ---------------------------------------------------------------------------
-- 1) Drop approval outbox index
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_outbox_approval_events;

-- ---------------------------------------------------------------------------
-- 2) Drop append-only triggers
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS approval_audit_log_no_update_delete ON approval_audit_log;
DROP TRIGGER IF EXISTS authority_limit_history_no_update_delete ON authority_limit_history;

-- Keep trg_reject_mutation if used by other migrations.

-- ---------------------------------------------------------------------------
-- 3) Revert tasks/cases extensions
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_tasks_approval_amount;
DROP INDEX IF EXISTS idx_tasks_approval_gate_fk;
DROP INDEX IF EXISTS idx_tasks_requires_approval;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS fk_tasks_approval_gate;
ALTER TABLE tasks
    DROP COLUMN IF EXISTS approval_amount,
    DROP COLUMN IF EXISTS approval_gate_id,
    DROP COLUMN IF EXISTS requires_approval;

ALTER TABLE cases
    DROP COLUMN IF EXISTS max_rework_attempts,
    DROP COLUMN IF EXISTS rework_count;

ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases ADD CONSTRAINT cases_status_check
    CHECK (status IN ('OPEN', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED', 'SUSPENDED', 'CLONED', 'REJECTED'));

-- ---------------------------------------------------------------------------
-- 4) Drop mutable-table triggers and indexes
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS user_authority_updated_at ON user_authority;
DROP TRIGGER IF EXISTS authority_limit_history_updated_at ON authority_limit_history;
DROP TRIGGER IF EXISTS approval_audit_log_updated_at ON approval_audit_log;
DROP TRIGGER IF EXISTS approval_chain_state_updated_at ON approval_chain_state;
DROP TRIGGER IF EXISTS approval_requests_updated_at ON approval_requests;
DROP TRIGGER IF EXISTS approval_gates_updated_at ON approval_gates;
DROP TRIGGER IF EXISTS users_updated_at ON users;

DROP INDEX IF EXISTS idx_approval_audit_event_type;
DROP INDEX IF EXISTS idx_approval_audit_request;
DROP INDEX IF EXISTS idx_authority_limit_history_user;
DROP INDEX IF EXISTS uq_user_authority_active;
DROP INDEX IF EXISTS idx_user_authority_expiry;
DROP INDEX IF EXISTS idx_user_authority_active_lookup;
DROP INDEX IF EXISTS idx_approval_chain_state_gate_fk;
DROP INDEX IF EXISTS idx_approval_chain_state_case;
DROP INDEX IF EXISTS uq_approval_requests_gate_approver_tier_pending;
DROP INDEX IF EXISTS idx_approval_requests_gate_fk;
DROP INDEX IF EXISTS idx_approval_requests_expiry_sweep;
DROP INDEX IF EXISTS idx_approval_requests_approver_pending;
DROP INDEX IF EXISTS idx_approval_requests_gate;
DROP INDEX IF EXISTS idx_approval_gates_task_fk;
DROP INDEX IF EXISTS idx_approval_gates_status;
DROP INDEX IF EXISTS idx_approval_gates_case;
DROP INDEX IF EXISTS idx_users_manager;
DROP INDEX IF EXISTS idx_users_role_status;

-- ---------------------------------------------------------------------------
-- 5) Drop approval tables
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS approval_audit_log;
DROP TABLE IF EXISTS authority_limit_history;
DROP TABLE IF EXISTS user_authority;
DROP TABLE IF EXISTS approval_chain_state;
DROP TABLE IF EXISTS approval_requests;
DROP TABLE IF EXISTS approval_gates;

-- ---------------------------------------------------------------------------
-- 6) Drop users directory table
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS users;
