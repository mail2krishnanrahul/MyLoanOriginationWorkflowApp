-- 000017_work_assignment.down.sql

-- Drop Triggers
DROP TRIGGER IF EXISTS workbaskets_updated_at ON workbaskets;
DROP TRIGGER IF EXISTS workers_updated_at ON workers;

-- Drop Indexes (Cascase drops columns usually handle this, but for clarity)
DROP INDEX IF EXISTS idx_sla_breach_task;
DROP INDEX IF EXISTS idx_tasks_assignee;
DROP INDEX IF EXISTS idx_tasks_workbasket;
DROP INDEX IF EXISTS idx_worker_availability_worker;
DROP INDEX IF EXISTS idx_worker_skills_skill;

-- Revert Tasks Table
ALTER TABLE tasks
    DROP COLUMN IF EXISTS required_skills,
    DROP COLUMN IF EXISTS assignee_id,
    DROP COLUMN IF EXISTS workbasket_id;

-- Drop Tables
DROP TABLE IF EXISTS sla_breach_log;
DROP TABLE IF EXISTS task_delegations;
DROP TABLE IF EXISTS worker_availability;
DROP TABLE IF EXISTS workbasket_members;
DROP TABLE IF EXISTS workbaskets;
DROP TABLE IF EXISTS worker_skills;
DROP TABLE IF EXISTS workers;
DROP TABLE IF EXISTS skills;
