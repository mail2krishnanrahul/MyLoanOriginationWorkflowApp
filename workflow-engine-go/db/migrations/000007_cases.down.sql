-- 000007_cases.down.sql
-- Rollback: drop cases table, triggers, and sequence.

DROP TRIGGER IF EXISTS cases_updated_at ON cases;
DROP TRIGGER IF EXISTS cases_generate_ref ON cases;
DROP FUNCTION IF EXISTS trg_generate_case_ref();
DROP TABLE IF EXISTS cases CASCADE;
DROP SEQUENCE IF EXISTS case_ref_seq;
