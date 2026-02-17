-- 000012_task_heartbeat.down.sql
-- Rollback: drop heartbeat and retry columns from tasks.

DROP INDEX IF EXISTS idx_tasks_retry;
DROP INDEX IF EXISTS idx_tasks_heartbeat;
ALTER TABLE tasks DROP COLUMN IF EXISTS next_retry_at;
ALTER TABLE tasks DROP COLUMN IF EXISTS last_heartbeat_at;
