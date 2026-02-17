-- Revert lifecycle management columns

DROP INDEX IF EXISTS idx_cases_resume_at;
DROP INDEX IF EXISTS idx_cases_source_case_id;

ALTER TABLE cases
DROP COLUMN IF EXISTS supervisor_id,
DROP COLUMN IF EXISTS emergency_reason,
DROP COLUMN IF EXISTS emergency_closed_at,
DROP COLUMN IF EXISTS withdrawal_reason,
DROP COLUMN IF EXISTS resume_at,
DROP COLUMN IF EXISTS suspend_reason,
DROP COLUMN IF EXISTS source_case_id;
