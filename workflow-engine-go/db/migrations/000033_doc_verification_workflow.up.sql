-- 000033_doc_verification_workflow.up.sql
-- Adds all schema objects needed for the HOME_LOAN_DOC_VERIFICATION workflow.
--
-- Tables added:
--   case_documents, document_error_tags, case_tags,
--   credit_memo_checklist, additional_info_requests, qa_staged_changes
--
-- Columns added to existing tables:
--   cases.assigned_user_id         -- direct case-level assignee for allocation
--   tasks.is_adhoc                 -- marks ad-hoc (free-form) tasks
--   tasks.is_blocking              -- marks tasks that block stage advancement

-- ---------------------------------------------------------------------------
-- 0) Cleanup: Drop any tables partially created by a previous failed run.
--    These use CASCADE so dependent tables are also cleaned.
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS qa_staged_changes        CASCADE;
DROP TABLE IF EXISTS additional_info_requests CASCADE;
DROP TABLE IF EXISTS credit_memo_checklist    CASCADE;
DROP TABLE IF EXISTS case_tags                CASCADE;
DROP TABLE IF EXISTS document_error_tags      CASCADE;
DROP TABLE IF EXISTS case_documents           CASCADE;


-- ---------------------------------------------------------------------------
-- 1) Extend cases table: direct assignee + workflow classification fields
-- ---------------------------------------------------------------------------
ALTER TABLE cases
    ADD COLUMN IF NOT EXISTS assigned_user_id UUID,
    ADD COLUMN IF NOT EXISTS case_complexity   TEXT,
    ADD COLUMN IF NOT EXISTS required_skills   TEXT[],
    ADD COLUMN IF NOT EXISTS submitted_by_banker_id TEXT;

CREATE INDEX IF NOT EXISTS idx_cases_assigned_user_033
    ON cases (tenant_id, assigned_user_id, status, current_stage_code)
    WHERE assigned_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cases_allocatable_033
    ON cases (tenant_id, current_stage_code, status, created_at ASC)
    WHERE assigned_user_id IS NULL AND status = 'OPEN';

-- ---------------------------------------------------------------------------
-- 2) Extend tasks table: ad-hoc flag and blocking flag
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS is_adhoc    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS is_blocking BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS external_reference TEXT,
    ADD COLUMN IF NOT EXISTS display_name TEXT,
    ADD COLUMN IF NOT EXISTS description  TEXT;

CREATE INDEX IF NOT EXISTS idx_tasks_adhoc_case_033
    ON tasks (tenant_id, case_id, is_adhoc, status)
    WHERE is_adhoc = TRUE;

CREATE INDEX IF NOT EXISTS idx_tasks_blocking_open_033
    ON tasks (tenant_id, case_id, is_blocking, status)
    WHERE is_blocking = TRUE AND status NOT IN ('COMPLETED','CANCELLED','SKIPPED','FAILED');

-- ---------------------------------------------------------------------------
-- 3) case_documents — ECM document metadata per case
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_documents (
    document_id       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_id           UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    ecm_document_id   TEXT         NOT NULL,
    ecm_reference     TEXT,
    document_category TEXT         NOT NULL,
    document_name     TEXT         NOT NULL,
    document_type     TEXT         NOT NULL DEFAULT 'PDF',
    page_count        INT,
    received_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    -- ecm_url is sensitive: never log, never include in notifications
    ecm_url           TEXT         NOT NULL,
    status            TEXT         NOT NULL DEFAULT 'PENDING_REVIEW'
                      CHECK (status IN (
                          'PENDING_REVIEW','UNDER_REVIEW','REVIEWED',
                          'ERROR_TAGGED','ACCEPTED','REQUESTED_RESUBMIT'
                      )),
    reviewed_by       UUID,
    reviewed_at       TIMESTAMPTZ,
    metadata          JSONB        NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_case_document_ecm_033 UNIQUE (tenant_id, case_id, ecm_document_id)
);

CREATE INDEX IF NOT EXISTS idx_case_documents_case_033
    ON case_documents (tenant_id, case_id, document_category, received_at ASC);

CREATE INDEX IF NOT EXISTS idx_case_documents_status_033
    ON case_documents (tenant_id, case_id, status);

DROP TRIGGER IF EXISTS case_documents_updated_at_033 ON case_documents;
CREATE TRIGGER case_documents_updated_at_033
    BEFORE UPDATE ON case_documents
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) document_error_tags — error tags applied to a document
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS document_error_tags (
    tag_id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    document_id       UUID         NOT NULL REFERENCES case_documents(document_id) ON DELETE CASCADE,
    case_id           UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    error_code        TEXT         NOT NULL,
    error_description TEXT,
    severity          TEXT         NOT NULL
                      CHECK (severity IN ('MINOR','MAJOR','BLOCKING')),
    tagged_by         UUID         NOT NULL,
    tagged_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ,
    resolved_by       UUID,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_doc_error_tags_document_033
    ON document_error_tags (tenant_id, document_id, severity);

CREATE INDEX IF NOT EXISTS idx_doc_error_tags_case_unresolved_033
    ON document_error_tags (tenant_id, case_id, severity)
    WHERE resolved_at IS NULL;

-- ---------------------------------------------------------------------------
-- 5) case_tags — free-form labels on a case
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_tags (
    tag_id      UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_id     UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    tag_code    TEXT         NOT NULL,
    tag_value   TEXT,
    tagged_by   UUID         NOT NULL,
    tagged_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    removed_at  TIMESTAMPTZ,
    removed_by  UUID
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_case_tags_active_033
    ON case_tags (tenant_id, case_id, tag_code)
    WHERE removed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_case_tags_case_033
    ON case_tags (tenant_id, case_id, removed_at);

-- ---------------------------------------------------------------------------
-- 6) credit_memo_checklist — structured cross-check checklist
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS credit_memo_checklist (
    checklist_id       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_id            UUID         NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    task_id            UUID         NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    checklist_version  INT          NOT NULL DEFAULT 1,
    items              JSONB        NOT NULL DEFAULT '[]',
    overall_status     TEXT         NOT NULL DEFAULT 'INCOMPLETE'
                       CHECK (overall_status IN (
                           'INCOMPLETE','PASSED','FAILED','PASSED_WITH_EXCEPTIONS'
                       )),
    completed_by       UUID,
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_credit_memo_checklist_task_033 UNIQUE (tenant_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_credit_memo_checklist_case_033
    ON credit_memo_checklist (tenant_id, case_id, overall_status);

DROP TRIGGER IF EXISTS credit_memo_checklist_updated_at_033 ON credit_memo_checklist;
CREATE TRIGGER credit_memo_checklist_updated_at_033
    BEFORE UPDATE ON credit_memo_checklist
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 7) additional_info_requests — loan officer requests to banker
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS additional_info_requests (
    request_id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID        NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_id               UUID        NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    requested_by          UUID        NOT NULL,
    requested_documents   JSONB       NOT NULL DEFAULT '[]',
    message_to_banker     TEXT,
    due_date              TIMESTAMPTZ NOT NULL,
    status                TEXT        NOT NULL DEFAULT 'PENDING'
                          CHECK (status IN ('PENDING','RECEIVED','OVERDUE','CANCELLED')),
    banker_notes          TEXT,
    submitted_ecm_ids     TEXT[],
    received_at           TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_add_info_requests_case_033
    ON additional_info_requests (tenant_id, case_id, status, created_at DESC);

DROP TRIGGER IF EXISTS additional_info_requests_updated_at_033 ON additional_info_requests;
CREATE TRIGGER additional_info_requests_updated_at_033
    BEFORE UPDATE ON additional_info_requests
    FOR EACH ROW EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 8) qa_staged_changes — changes intercepted while case is QA-locked
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS qa_staged_changes (
    change_id     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    case_id       UUID        NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    change_type   TEXT        NOT NULL
                  CHECK (change_type IN (
                      'DOCUMENT_UPDATE','CASE_METADATA_UPDATE',
                      'DOCUMENT_ADD','ERROR_TAG_UPDATE'
                  )),
    payload       JSONB       NOT NULL DEFAULT '{}',
    submitted_by  UUID        NOT NULL,
    submitted_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    status        TEXT        NOT NULL DEFAULT 'PENDING'
                  CHECK (status IN ('PENDING','APPLIED','DISCARDED')),
    applied_at    TIMESTAMPTZ,
    applied_by    UUID
);

CREATE INDEX IF NOT EXISTS idx_qa_staged_changes_case_pending_033
    ON qa_staged_changes (tenant_id, case_id, status, submitted_at ASC)
    WHERE status = 'PENDING';

-- ---------------------------------------------------------------------------
-- 9) Seed: activity_definitions for HOME_LOAN_DOC_VERIFICATION (if table exists)
-- ---------------------------------------------------------------------------
-- The case type config JSONB blob is inserted by the Go seed function.
-- No additional SQL seeding is needed here.
