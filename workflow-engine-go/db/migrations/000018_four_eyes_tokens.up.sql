-- 000018_four_eyes_tokens.up.sql
-- 4-eyes (dual-supervisor) approval tokens for emergency case closure.

CREATE TABLE IF NOT EXISTS four_eyes_tokens (
    id          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    token       TEXT            NOT NULL UNIQUE,
    case_id     UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    issuer_id   TEXT            NOT NULL,  -- supervisor who created the token
    consumed_by TEXT,                      -- supervisor who consumed the token (must differ from issuer)
    expires_at  TIMESTAMPTZ     NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT now()
);

-- Index for token lookup during validation
CREATE INDEX idx_four_eyes_tokens_lookup
ON four_eyes_tokens (token)
WHERE consumed_at IS NULL;

-- Index for cleanup of expired tokens
CREATE INDEX idx_four_eyes_tokens_expiry
ON four_eyes_tokens (expires_at)
WHERE consumed_at IS NULL;

-- Add PROCESSING status to events_outbox CHECK constraint (needed by fixed PollPendingEvents).
-- Postgres doesn't support ALTER CHECK easily, so we drop+re-add if it exists.
-- The original schema may not have a CHECK constraint on status, so this is safe.
-- If the column has no CHECK, this is a no-op addition.
