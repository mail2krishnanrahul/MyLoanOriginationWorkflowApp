-- 009-activities.sql
-- Design-time activity definitions: grouping of tasks within a stage.
-- Activities are NOT runtime rows — they exist only as configuration.

CREATE TABLE IF NOT EXISTS activity_definitions (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_type_id        UUID            NOT NULL REFERENCES case_types(id) ON DELETE CASCADE,
    case_type_version   INT             NOT NULL,
    stage_code          VARCHAR(100)    NOT NULL,
    activity_code       VARCHAR(100)    NOT NULL,
    activity_name       VARCHAR(255)    NOT NULL,
    description         TEXT,
    ordinal             INT             NOT NULL,          -- order within the stage
    is_optional         BOOLEAN         NOT NULL DEFAULT false,
    completion_policy   VARCHAR(20)     NOT NULL DEFAULT 'ALL_TASKS'
                            CHECK (completion_policy IN ('ALL_TASKS', 'ANY_TASK', 'MANUAL')),
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),

    -- One activity_code per stage per case_type version
    CONSTRAINT uq_activity_def_code
        UNIQUE (case_type_id, case_type_version, stage_code, activity_code),

    -- One ordinal per stage per case_type version
    CONSTRAINT uq_activity_def_ordinal
        UNIQUE (case_type_id, case_type_version, stage_code, ordinal)
);

-- Fast lookup: all activities for a stage, ordered
CREATE INDEX IF NOT EXISTS idx_activity_defs_stage
    ON activity_definitions (case_type_id, case_type_version, stage_code, ordinal);
