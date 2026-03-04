-- Rollback migration 000036: remove case summary editable fields
ALTER TABLE cases
  DROP COLUMN IF EXISTS case_complexity,
  DROP COLUMN IF EXISTS is_vip,
  DROP COLUMN IF EXISTS target_close_date,
  DROP COLUMN IF EXISTS channel,
  DROP COLUMN IF EXISTS officer_name;

DROP TYPE IF EXISTS case_complexity;
