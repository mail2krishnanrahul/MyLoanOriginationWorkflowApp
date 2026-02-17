-- 012-task-heartbeat.sql
-- Adds heartbeat and retry scheduling columns to the tasks table.

-- Heartbeat: workers touch this while processing to prove liveness
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ;

-- Retry scheduling: exponential backoff target time
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;

-- Index for stale-task reclamation queries
CREATE INDEX IF NOT EXISTS idx_tasks_heartbeat
    ON tasks (status, last_heartbeat_at)
    WHERE status IN ('ASSIGNED', 'IN_PROGRESS');

-- Index for retry scheduling (find retryable tasks)
CREATE INDEX IF NOT EXISTS idx_tasks_retry
    ON tasks (status, next_retry_at)
    WHERE status = 'FAILED' AND next_retry_at IS NOT NULL;
