-- 000034_workbasket_membership_expiry.up.sql
-- Add time-bounded membership support to workbasket_members.

ALTER TABLE workbasket_members
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- Index on (workbasket_id, expires_at) to efficiently query non-expired members.
-- A partial index with now() is not allowed in Postgres (non-IMMUTABLE).
-- A regular index on expires_at lets the planner filter on (expires_at IS NULL OR expires_at > now()).
CREATE INDEX IF NOT EXISTS idx_workbasket_members_expiry
    ON workbasket_members (workbasket_id, expires_at);

COMMENT ON COLUMN workbasket_members.expires_at IS
    'NULL = permanent membership; non-null = on-call rotation end time';
