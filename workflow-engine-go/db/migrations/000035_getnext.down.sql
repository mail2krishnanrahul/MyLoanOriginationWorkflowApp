-- 000035_getnext.down.sql

DROP MATERIALIZED VIEW IF EXISTS case_user_affinity;
DROP TABLE IF EXISTS getnext_queue_snapshots;
DROP TABLE IF EXISTS case_allocation_transitions;
DROP TABLE IF EXISTS getnext_claims;
DROP TABLE IF EXISTS getnext_weights;

DROP INDEX IF EXISTS idx_cases_required_skills_gin_035;
DROP INDEX IF EXISTS idx_cases_getnext_allocatable_035;

-- Restore original cases.status CHECK (without ALLOCATABLE)
ALTER TABLE cases DROP CONSTRAINT IF EXISTS cases_status_check;
ALTER TABLE cases
    ADD CONSTRAINT cases_status_check CHECK (status IN (
        'OPEN', 'IN_PROGRESS', 'COMPLETED', 'CANCELLED', 'SUSPENDED'
    ));
