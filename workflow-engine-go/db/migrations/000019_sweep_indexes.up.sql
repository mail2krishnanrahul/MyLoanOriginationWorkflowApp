-- 000019_sweep_indexes.up.sql
-- Performance indexes for background sweep jobs (expiry, archival, SLA).

-- Expiry sweep: find active cases that have exceeded their TTL
CREATE INDEX IF NOT EXISTS idx_cases_expiry_sweep
ON cases (status, created_at)
WHERE status IN ('OPEN', 'IN_PROGRESS');

-- Archival sweep: find completed/cancelled cases past archive TTL
CREATE INDEX IF NOT EXISTS idx_cases_archival_sweep
ON cases (status, completed_at)
WHERE status IN ('COMPLETED', 'CANCELLED') AND completed_at IS NOT NULL;

-- SLA sweep: tasks with due_at that need priority promotion
CREATE INDEX IF NOT EXISTS idx_tasks_sla_sweep
ON tasks (status, due_at, priority)
WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS') AND due_at IS NOT NULL;

-- SLA breach check: tasks past due_at not yet logged
CREATE INDEX IF NOT EXISTS idx_tasks_breach_candidates
ON tasks (due_at, status)
WHERE status IN ('PENDING', 'ASSIGNED', 'IN_PROGRESS') AND due_at IS NOT NULL;
