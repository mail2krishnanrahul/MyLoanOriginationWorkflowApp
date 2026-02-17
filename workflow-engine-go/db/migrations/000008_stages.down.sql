-- 000008_stages.down.sql
-- Rollback: drop stage_definitions and case_stage_transitions tables.

DROP TABLE IF EXISTS case_stage_transitions CASCADE;
DROP TABLE IF EXISTS stage_definitions CASCADE;
