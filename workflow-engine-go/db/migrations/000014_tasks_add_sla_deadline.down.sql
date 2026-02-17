-- 000009_tasks_add_sla_deadline.down.sql
-- Rollback: remove sla_deadline_at column from tasks.

DROP INDEX IF EXISTS idx_tasks_sla_deadline;
ALTER TABLE tasks DROP COLUMN IF EXISTS sla_deadline_at;
