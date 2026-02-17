-- Redesign: Recursive Workflow Schema

-- 1. Case Definitions: High-level types (e.g. 'Business_Loan', 'Personal_Loan')
CREATE TABLE case_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 2. Version Registry: Pins a complete workflow structure to a version
CREATE TABLE version_registry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_definition_id UUID NOT NULL REFERENCES case_definitions(id),
    version INT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'DRAFT', -- DRAFT, ACTIVE, ARCHIVED
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(case_definition_id, version)
);

-- 3. Workflow Components: The core self-referencing table
-- Types: STAGE, ACTIVITY, TASK
CREATE TABLE workflow_components (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version_id UUID NOT NULL REFERENCES version_registry(id) ON DELETE CASCADE,
    parent_component_id UUID REFERENCES workflow_components(id) ON DELETE CASCADE, -- Recursive FK
    type VARCHAR(50) NOT NULL, -- STAGE, ACTIVITY, TASK
    name VARCHAR(255) NOT NULL,
    execution_strategy VARCHAR(50) NOT NULL DEFAULT 'SEQUENTIAL', -- SEQUENTIAL, PARALLEL
    execution_order INT NOT NULL DEFAULT 0,
    config JSONB, -- UI config, Integration endpoints, etc.
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index for tree traversal
CREATE INDEX idx_workflow_components_parent ON workflow_components(parent_component_id);
CREATE INDEX idx_workflow_components_version ON workflow_components(version_id);

-- 4. Component Hooks: Pre/Post execution logic
CREATE TABLE component_hooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    component_id UUID NOT NULL REFERENCES workflow_components(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL, -- PRE_EXECUTE, POST_EXECUTE
    action VARCHAR(50) NOT NULL, -- NOTIFY, WEBHOOK, LAMBDA
    config JSONB,
    execution_order INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 5. Updated Cases Table (referencing the new version registry)
-- Note: You might need to drop the old 'cases' table or migrate it.
-- This definition replaces the old one.
/*
CREATE TABLE cases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_definition_id UUID NOT NULL REFERENCES case_definitions(id),
    pinned_version_id UUID NOT NULL REFERENCES version_registry(id),
    global_status VARCHAR(50) NOT NULL,
    applicant_data JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
*/

-- 6. Component Instances: Tracking execution state of the tree for a Case
-- Replaces old 'task_instances' and 'stage_instances'
CREATE TABLE component_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id UUID NOT NULL REFERENCES cases(id) ON DELETE CASCADE, -- Assuming 'cases' exists
    component_id UUID NOT NULL REFERENCES workflow_components(id),
    status VARCHAR(50) NOT NULL, -- PENDING, IN_PROGRESS, COMPLETED, FAILED, SKIPPED
    data_payload JSONB, -- Task output or input
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_component_instances_case ON component_instances(case_id);
