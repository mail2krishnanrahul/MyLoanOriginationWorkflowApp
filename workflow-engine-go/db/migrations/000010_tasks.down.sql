-- 000010_tasks.down.sql
-- Rollback: drop tasks table and its trigger.

DROP TRIGGER IF EXISTS tasks_updated_at ON tasks;
DROP TABLE IF EXISTS tasks CASCADE;
