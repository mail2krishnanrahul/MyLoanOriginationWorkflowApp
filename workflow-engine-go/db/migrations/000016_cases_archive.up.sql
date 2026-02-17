-- Create archive tables for cases and tasks

CREATE TABLE cases_archive (
    -- Mirror of cases table
    id UUID PRIMARY KEY,
    reference_number TEXT NOT NULL,
    case_type_id UUID NOT NULL,
    case_type_version INT NOT NULL,
    parent_case_id UUID,
    current_stage_code TEXT,
    current_stage_ordinal INT,
    status TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    assigned_to TEXT,
    row_version INT DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    
    -- Lifecycle columns
    source_case_id UUID,
    suspend_reason TEXT,
    resume_at TIMESTAMPTZ,
    withdrawal_reason TEXT,
    emergency_closed_at TIMESTAMPTZ,
    emergency_reason TEXT,
    supervisor_id TEXT,

    -- Archive specific
    archived_at TIMESTAMPTZ DEFAULT NOW(),
    archived_reason TEXT
);

CREATE TABLE tasks_archive (
    -- Mirror of tasks table
    id UUID PRIMARY KEY,
    case_id UUID NOT NULL,
    task_definition_code TEXT NOT NULL,
    activity_code TEXT NOT NULL,
    stage_code TEXT NOT NULL,
    status TEXT NOT NULL,
    priority INT NOT NULL,
    assigned_service TEXT,
    assigned_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    due_at TIMESTAMPTZ,
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    input_payload JSONB DEFAULT '{}',
    output_payload JSONB DEFAULT '{}',
    metadata JSONB DEFAULT '{}',
    error_detail JSONB,
    idempotency_key TEXT,
    version INT DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    -- Archive specific
    archived_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for archive searching/retrieval
CREATE INDEX idx_cases_archive_ref ON cases_archive(reference_number);
CREATE INDEX idx_cases_archive_orig_id ON cases_archive(id);
CREATE INDEX idx_tasks_archive_case_id ON tasks_archive(case_id);
