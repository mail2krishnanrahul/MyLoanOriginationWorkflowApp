-- 000009_tasks_add_sla_deadline.up.sql
-- Backward-compatible migration: adds sla_deadline_at column to tasks.
--
-- Pattern: ADD COLUMN with DEFAULT NULL (no table rewrite, no lock).
-- App code in Release N ignores this column (backward compatible).
-- App code in Release N+1 starts writing/reading it.

-- Step 1: Add the column (instant on Postgres 11+ for nullable columns)
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS sla_deadline_at TIMESTAMPTZ;

-- Step 2: Add a comment explaining the column
COMMENT ON COLUMN tasks.sla_deadline_at IS
    'SLA deadline: task must be completed by this time. NULL = no SLA.';

-- Step 3: Add an index CONCURRENTLY (does not lock the table)
-- NOTE: golang-migrate runs each file in a transaction by default.
-- CREATE INDEX CONCURRENTLY cannot run inside a transaction.
-- You must either:
--   a) Use the "x-multi-statement" source parameter, OR
--   b) Run this index creation as a separate manual step.
-- For safety, we use a regular CREATE INDEX here. For tables >10M rows,
-- switch to CONCURRENTLY and run outside a transaction.
CREATE INDEX IF NOT EXISTS idx_tasks_sla_deadline
    ON tasks (sla_deadline_at)
    WHERE sla_deadline_at IS NOT NULL AND status NOT IN ('COMPLETED', 'CANCELLED', 'SKIPPED');
