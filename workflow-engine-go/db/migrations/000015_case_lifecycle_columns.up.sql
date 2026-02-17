-- Add lifecycle management columns to cases table

ALTER TABLE cases
ADD COLUMN source_case_id UUID REFERENCES cases(id),
ADD COLUMN suspend_reason TEXT,
ADD COLUMN resume_at TIMESTAMPTZ,
ADD COLUMN withdrawal_reason TEXT,
ADD COLUMN emergency_closed_at TIMESTAMPTZ,
ADD COLUMN emergency_reason TEXT,
ADD COLUMN supervisor_id TEXT;

-- Add indexes for common queries
CREATE INDEX idx_cases_source_case_id ON cases(source_case_id);
CREATE INDEX idx_cases_resume_at ON cases(resume_at) WHERE status = 'SUSPENDED';
