-- 000024_document_management.up.sql
-- Document & data management capability:
-- typed document definitions, document metadata storage (no file bytes),
-- requirement tracking, sensitive field redaction rules, and retention controls.

-- ---------------------------------------------------------------------------
-- 1) Document type definitions
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS document_types (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_type_code          VARCHAR(100)    NOT NULL,
    case_type_version       INT             NOT NULL,
    document_type_code      VARCHAR(100)    NOT NULL,
    display_name            VARCHAR(255)    NOT NULL,
    description             TEXT,
    allowed_extensions      TEXT[]          NOT NULL DEFAULT '{}',
    max_size_mb             INT             NOT NULL CHECK (max_size_mb > 0),
    required_at_stage       VARCHAR(100),
    required_count_min      INT             NOT NULL DEFAULT 1 CHECK (required_count_min >= 0),
    required_count_max      INT             NOT NULL DEFAULT 1 CHECK (required_count_max >= required_count_min),
    is_sensitive            BOOLEAN         NOT NULL DEFAULT FALSE,
    requires_verification   BOOLEAN         NOT NULL DEFAULT FALSE,
    verification_role       VARCHAR(100),
    retention_days          INT             NOT NULL CHECK (retention_days > 0),
    retention_policy        VARCHAR(20)     NOT NULL DEFAULT 'ARCHIVE'
                                CHECK (retention_policy IN ('ARCHIVE', 'DELETE')),
    allowed_viewers         TEXT[]          NOT NULL DEFAULT ARRAY['PUBLIC'],
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),

    CONSTRAINT uq_document_types_case_doc UNIQUE (case_type_code, case_type_version, document_type_code),
    CONSTRAINT fk_document_types_case_type
        FOREIGN KEY (case_type_code, case_type_version)
        REFERENCES case_types(code, version)
        ON DELETE CASCADE,
    CONSTRAINT chk_document_types_extensions_not_empty
        CHECK (array_length(allowed_extensions, 1) IS NULL OR array_length(allowed_extensions, 1) > 0),
    CONSTRAINT chk_document_types_allowed_viewers_not_empty
        CHECK (array_length(allowed_viewers, 1) IS NULL OR array_length(allowed_viewers, 1) > 0),
    CONSTRAINT chk_document_types_verification_role_required
        CHECK (
            (requires_verification = FALSE)
            OR (verification_role IS NOT NULL AND btrim(verification_role) <> '')
        )
);

COMMENT ON TABLE document_types IS
'Case-type-scoped document definitions and constraints (size/type/retention/viewer controls).';
COMMENT ON COLUMN document_types.allowed_extensions IS
'Whitelisted file extensions (for example pdf,jpg,png). Used to validate uploads.';
COMMENT ON COLUMN document_types.retention_policy IS
'Post-retention action for completed/cancelled cases: ARCHIVE to cold storage or DELETE from object storage.';
COMMENT ON COLUMN document_types.allowed_viewers IS
'Allowed viewer roles. Use PUBLIC for unrestricted access.';

CREATE INDEX IF NOT EXISTS idx_document_types_case_type
    ON document_types (case_type_code, case_type_version, required_at_stage);

DROP TRIGGER IF EXISTS document_types_updated_at ON document_types;
CREATE TRIGGER document_types_updated_at
    BEFORE UPDATE ON document_types
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 2) Case document metadata (content remains in object storage)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_documents (
    id                          UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id                     UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    task_id                     UUID            REFERENCES tasks(id) ON DELETE SET NULL,
    stage_code                  VARCHAR(100)    NOT NULL,
    case_type_code              VARCHAR(100)    NOT NULL,
    case_type_version           INT             NOT NULL,
    document_type_code          VARCHAR(100)    NOT NULL,
    filename                    TEXT            NOT NULL,
    file_extension              VARCHAR(20)     NOT NULL,
    file_size_bytes             BIGINT          NOT NULL CHECK (file_size_bytes > 0),
    storage_provider            VARCHAR(20)     NOT NULL
                                    CHECK (storage_provider IN ('S3', 'GCS', 'AZURE_BLOB', 'LOCAL')),
    storage_path                TEXT            NOT NULL,
    storage_url                 TEXT,
    checksum_sha256             CHAR(64)        NOT NULL,
    uploaded_by                 TEXT            NOT NULL,
    uploaded_at                 TIMESTAMPTZ     NOT NULL DEFAULT now(),
    status                      VARCHAR(30)     NOT NULL DEFAULT 'PENDING_UPLOAD'
                                    CHECK (status IN (
                                        'PENDING_UPLOAD',
                                        'UPLOADED',
                                        'VERIFIED',
                                        'REJECTED',
                                        'ARCHIVED',
                                        'DELETED'
                                    )),
    rejection_reason            TEXT,
    verified_by                 TEXT,
    verified_at                 TIMESTAMPTZ,
    metadata                    JSONB           NOT NULL DEFAULT '{}',
    version                     INT             NOT NULL DEFAULT 1 CHECK (version > 0),
    superseded_by_document_id   UUID            REFERENCES case_documents(id) ON DELETE SET NULL,
    legal_hold                  BOOLEAN         NOT NULL DEFAULT FALSE,
    created_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ     NOT NULL DEFAULT now(),

    CONSTRAINT fk_case_documents_document_type
        FOREIGN KEY (case_type_code, case_type_version, document_type_code)
        REFERENCES document_types(case_type_code, case_type_version, document_type_code)
        ON DELETE RESTRICT,
    CONSTRAINT chk_case_documents_sha256
        CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_case_documents_storage_path
        CHECK (storage_path ~ '^[^/]+/.+'),
    CONSTRAINT chk_case_documents_rejection_reason
        CHECK (status <> 'REJECTED' OR (rejection_reason IS NOT NULL AND btrim(rejection_reason) <> '')),
    CONSTRAINT chk_case_documents_verified_columns
        CHECK (
            status <> 'VERIFIED'
            OR (verified_by IS NOT NULL AND verified_at IS NOT NULL)
        ),
    CONSTRAINT chk_case_documents_self_reference
        CHECK (superseded_by_document_id IS NULL OR superseded_by_document_id <> id)
);

COMMENT ON TABLE case_documents IS
'Metadata-only document registry. Raw file bytes are stored in external object storage, never in Postgres.';
COMMENT ON COLUMN case_documents.storage_path IS
'Canonical object path formatted as {bucket}/{case_id}/{document_id}.{ext}.';
COMMENT ON COLUMN case_documents.legal_hold IS
'Compliance hold flag. When true, automated archival/deletion is suspended.';
COMMENT ON COLUMN case_documents.metadata IS
'Extensible JSON metadata (OCR output, extraction tags, provider attributes).';

CREATE INDEX IF NOT EXISTS idx_case_documents_case_type_status
    ON case_documents (case_id, document_type_code, status);

CREATE INDEX IF NOT EXISTS idx_case_documents_uploaded_status
    ON case_documents (uploaded_at, status);

CREATE INDEX IF NOT EXISTS idx_case_documents_superseded_by
    ON case_documents (superseded_by_document_id);

CREATE INDEX IF NOT EXISTS idx_case_documents_checksum_sha256
    ON case_documents (checksum_sha256);

CREATE INDEX IF NOT EXISTS idx_case_documents_case_latest
    ON case_documents (case_id, document_type_code, version DESC)
    WHERE superseded_by_document_id IS NULL;

DROP TRIGGER IF EXISTS case_documents_updated_at ON case_documents;
CREATE TRIGGER case_documents_updated_at
    BEFORE UPDATE ON case_documents
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 3) Document requirement tracking
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS document_requests (
    id                      UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id                 UUID            NOT NULL REFERENCES cases(id) ON DELETE CASCADE,
    case_type_code          VARCHAR(100)    NOT NULL,
    case_type_version       INT             NOT NULL,
    document_type_code      VARCHAR(100)    NOT NULL,
    required_at_stage       VARCHAR(100),
    required_count_min      INT             NOT NULL CHECK (required_count_min >= 0),
    required_count_max      INT             NOT NULL CHECK (required_count_max >= required_count_min),
    current_count           INT             NOT NULL DEFAULT 0 CHECK (current_count >= 0),
    status                  VARCHAR(30)     NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING', 'PARTIALLY_FULFILLED', 'FULFILLED', 'WAIVED')),
    requested_at            TIMESTAMPTZ     NOT NULL DEFAULT now(),
    fulfilled_at            TIMESTAMPTZ,
    waived_by               TEXT,
    waived_at               TIMESTAMPTZ,
    waiver_reason           TEXT,
    created_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT now(),

    CONSTRAINT uq_document_requests_case_doc_stage
        UNIQUE (case_id, document_type_code, required_at_stage),
    CONSTRAINT fk_document_requests_document_type
        FOREIGN KEY (case_type_code, case_type_version, document_type_code)
        REFERENCES document_types(case_type_code, case_type_version, document_type_code)
        ON DELETE RESTRICT,
    CONSTRAINT chk_document_requests_current_count_max
        CHECK (current_count <= required_count_max),
    CONSTRAINT chk_document_requests_waiver
        CHECK (
            status <> 'WAIVED'
            OR (waived_by IS NOT NULL AND waived_at IS NOT NULL AND waiver_reason IS NOT NULL AND btrim(waiver_reason) <> '')
        )
);

COMMENT ON TABLE document_requests IS
'Tracks required documents per case/stage and fulfillment progress.';
COMMENT ON COLUMN document_requests.current_count IS
'Current number of qualifying documents (typically UPLOADED/VERIFIED depending on policy).';

CREATE INDEX IF NOT EXISTS idx_document_requests_case_status
    ON document_requests (case_id, status);

DROP TRIGGER IF EXISTS document_requests_updated_at ON document_requests;
CREATE TRIGGER document_requests_updated_at
    BEFORE UPDATE ON document_requests
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) Sensitive field redaction rules
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sensitive_fields (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    field_path          TEXT            NOT NULL,
    redaction_rule      VARCHAR(20)     NOT NULL
                            CHECK (redaction_rule IN ('MASK', 'TRUNCATE', 'HIDE')),
    mask_pattern        TEXT,
    allowed_roles       TEXT[]          NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),

    CONSTRAINT uq_sensitive_fields_path UNIQUE (field_path)
);

COMMENT ON TABLE sensitive_fields IS
'Read-time redaction policy for sensitive payload/metadata fields.';
COMMENT ON COLUMN sensitive_fields.field_path IS
'Dot path (for example input_payload.borrower_ssn or metadata.bank_account_number).';

CREATE INDEX IF NOT EXISTS idx_sensitive_fields_path
    ON sensitive_fields (field_path);

-- ---------------------------------------------------------------------------
-- 5) Optional document verification audit table
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS document_verification_tasks (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id             UUID            NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
    document_id         UUID            NOT NULL REFERENCES case_documents(id) ON DELETE CASCADE,
    requested_role      VARCHAR(100)    NOT NULL,
    status              VARCHAR(30)     NOT NULL DEFAULT 'PENDING'
                            CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'REQUEST_REUPLOAD', 'CANCELLED')),
    verifier_id         TEXT,
    verified_at         TIMESTAMPTZ,
    reason              TEXT,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT now()
);

COMMENT ON TABLE document_verification_tasks IS
'Optional audit mapping between verification tasks and their target documents.';

CREATE INDEX IF NOT EXISTS idx_document_verification_tasks_document
    ON document_verification_tasks (document_id, status);

DROP TRIGGER IF EXISTS document_verification_tasks_updated_at ON document_verification_tasks;
CREATE TRIGGER document_verification_tasks_updated_at
    BEFORE UPDATE ON document_verification_tasks
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 6) Tasks extension for document verification marker
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS is_document_verification BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN tasks.is_document_verification IS
'True when task is dedicated to document verification workflow.';
