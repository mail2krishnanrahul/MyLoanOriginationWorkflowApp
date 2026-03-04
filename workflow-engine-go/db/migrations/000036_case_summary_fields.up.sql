-- Migration 000036: Case summary editable fields
-- Adds case_complexity, is_vip columns to cases, and target_close_date
-- These drive the classifier workflow step (1st human action on a case).

-- 1. case_complexity enum + column
DO $$ BEGIN
  CREATE TYPE case_complexity AS ENUM (
    'SIMPLE',
    'STANDARD_1',
    'STANDARD_2',
    'COMPLEX',
    'NON_STANDARD'
  );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE cases
  ADD COLUMN IF NOT EXISTS case_complexity  case_complexity,
  ADD COLUMN IF NOT EXISTS is_vip           BOOLEAN      NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS target_close_date DATE,
  ADD COLUMN IF NOT EXISTS channel          TEXT,
  ADD COLUMN IF NOT EXISTS officer_name     TEXT;

-- Index for supervisor reporting on complexity
CREATE INDEX IF NOT EXISTS idx_cases_complexity
  ON cases (case_complexity)
  WHERE case_complexity IS NOT NULL;

-- Index for VIP fast-path
CREATE INDEX IF NOT EXISTS idx_cases_is_vip
  ON cases (is_vip)
  WHERE is_vip = TRUE;

COMMENT ON COLUMN cases.case_complexity   IS 'Classifier-set deal complexity tier. Drives SLA target date.';
COMMENT ON COLUMN cases.is_vip            IS 'VIP flag set manually by classifier or team lead.';
COMMENT ON COLUMN cases.target_close_date IS 'Projected close date, auto-set from complexity SLA or overridden.';
COMMENT ON COLUMN cases.channel           IS 'Origination channel (e.g. BROKER, DIRECT, MOBILE).';
COMMENT ON COLUMN cases.officer_name      IS 'Name of the responsible loan officer (denormalised for display).';
