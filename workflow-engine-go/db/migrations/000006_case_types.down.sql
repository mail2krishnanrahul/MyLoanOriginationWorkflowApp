-- 000006_case_types.down.sql
-- Rollback: drop case_types table and its trigger/function.

DROP TRIGGER IF EXISTS case_types_updated_at ON case_types;
DROP TABLE IF EXISTS case_types CASCADE;
-- Note: trg_set_updated_at() is shared; do NOT drop it here.
