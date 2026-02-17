-- 000017_work_assignment.up.sql

-- 1. Skills Master List
CREATE TABLE IF NOT EXISTS skills (
    code            VARCHAR(100)    PRIMARY KEY,
    description     TEXT
);

-- 2. Workers (Service Agents or Humans)
CREATE TABLE IF NOT EXISTS workers (
    id                      TEXT            PRIMARY KEY, -- e.g. email or username
    max_concurrent_tasks    INT             NOT NULL DEFAULT 5,
    status                  VARCHAR(20)     NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now()
);

-- 3. Worker Skills Mapping
CREATE TABLE IF NOT EXISTS worker_skills (
    worker_id       TEXT            NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    skill_code      VARCHAR(100)    NOT NULL REFERENCES skills(code) ON DELETE CASCADE,
    proficiency     VARCHAR(20)     NOT NULL CHECK (proficiency IN ('BEGINNER', 'COMPETENT', 'EXPERT')),
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (worker_id, skill_code)
);

-- 4. Workbaskets (Team Queues)
CREATE TABLE IF NOT EXISTS workbaskets (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT            NOT NULL UNIQUE,
    type                VARCHAR(20)     NOT NULL CHECK (type IN ('GENERAL', 'SPECIALIST', 'ESCALATION')),
    strategy            VARCHAR(20)     NOT NULL DEFAULT 'ROUND_ROBIN' CHECK (strategy IN ('ROUND_ROBIN', 'LEAST_LOADED', 'SKILL_SCORE')),
    round_robin_cursor  TEXT,           -- ID of last assigned worker
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now()
);

-- 5. Workbasket Members
CREATE TABLE IF NOT EXISTS workbasket_members (
    workbasket_id   UUID            NOT NULL REFERENCES workbaskets(id) ON DELETE CASCADE,
    worker_id       TEXT            NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    joined_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    PRIMARY KEY (workbasket_id, worker_id)
);

-- 6. Worker Availability (OOO)
CREATE TABLE IF NOT EXISTS worker_availability (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    worker_id       TEXT            NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    available_from  TIMESTAMPTZ     NOT NULL,
    available_until TIMESTAMPTZ     NOT NULL,
    timezone        TEXT            NOT NULL DEFAULT 'UTC',
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CHECK (available_until > available_from)
);

-- 7. Task Delegations Audit
CREATE TABLE IF NOT EXISTS task_delegations (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID            NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    from_assignee   TEXT,           -- null if from system/unassigned
    to_assignee     TEXT,           -- null if to basket? No, usually to someone or basket.
    to_workbasket_id UUID          REFERENCES workbaskets(id),
    reason          TEXT,
    delegation_type VARCHAR(20)     NOT NULL CHECK (delegation_type IN ('MANUAL', 'AUTO_ESCALATION', 'OUT_OF_OFFICE')),
    delegated_by    TEXT            NOT NULL, -- user or 'system'
    delegated_at    TIMESTAMPTZ     NOT NULL DEFAULT now()
);

-- 8. SLA Breach Log
CREATE TABLE IF NOT EXISTS sla_breach_log (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id             UUID            NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    case_id             UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    original_due_at     TIMESTAMPTZ,
    breach_detected_at  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    assignee_at_breach  TEXT,
    elapsed_percentage  INT
);

-- 9. Alter Tasks Table
-- We add columns to support assignment.
-- Note: 'assigned_service' in tasks was a text field. We keep it or migrate it?
-- The plan says "Add assignee_id (Text, FK to workers)".
-- 'assigned_service' might be used for 'service name', while 'assignee_id' is user/worker.
-- Let's add 'assignee_id' and 'workbasket_id'.

ALTER TABLE tasks
    ADD COLUMN workbasket_id   UUID REFERENCES workbaskets(id),
    ADD COLUMN assignee_id     TEXT REFERENCES workers(id),
    ADD COLUMN required_skills JSONB DEFAULT '[]'::jsonb; -- Array of {code, min_proficiency}

-- Indexes
CREATE INDEX IF NOT EXISTS idx_tasks_workbasket ON tasks(workbasket_id, priority DESC, due_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee_id, status);
CREATE INDEX IF NOT EXISTS idx_worker_skills_skill ON worker_skills(skill_code);
CREATE INDEX IF NOT EXISTS idx_worker_availability_worker ON worker_availability(worker_id);
CREATE INDEX IF NOT EXISTS idx_sla_breach_task ON sla_breach_log(task_id);

-- Trigger for workers updated_at
CREATE TRIGGER workers_updated_at
    BEFORE UPDATE ON workers
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- Trigger for workbaskets updated_at
CREATE TRIGGER workbaskets_updated_at
    BEFORE UPDATE ON workbaskets
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();
