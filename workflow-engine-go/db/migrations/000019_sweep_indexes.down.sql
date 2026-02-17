-- 000019_sweep_indexes.down.sql

DROP INDEX CONCURRENTLY IF EXISTS idx_tasks_breach_candidates;
DROP INDEX CONCURRENTLY IF EXISTS idx_tasks_sla_sweep;
DROP INDEX CONCURRENTLY IF EXISTS idx_cases_archival_sweep;
DROP INDEX CONCURRENTLY IF EXISTS idx_cases_expiry_sweep;
