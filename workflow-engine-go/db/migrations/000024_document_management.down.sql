-- 000024_document_management.down.sql
-- Rollback document & data management capability.

-- ---------------------------------------------------------------------------
-- 1) Drop task extension column
-- ---------------------------------------------------------------------------
ALTER TABLE tasks
    DROP COLUMN IF EXISTS is_document_verification;

-- ---------------------------------------------------------------------------
-- 2) Drop triggers
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS document_verification_tasks_updated_at ON document_verification_tasks;
DROP TRIGGER IF EXISTS document_requests_updated_at ON document_requests;
DROP TRIGGER IF EXISTS case_documents_updated_at ON case_documents;
DROP TRIGGER IF EXISTS document_types_updated_at ON document_types;

-- ---------------------------------------------------------------------------
-- 3) Drop indexes
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_document_verification_tasks_document;
DROP INDEX IF EXISTS idx_sensitive_fields_path;
DROP INDEX IF EXISTS idx_document_requests_case_status;
DROP INDEX IF EXISTS idx_case_documents_case_latest;
DROP INDEX IF EXISTS idx_case_documents_checksum_sha256;
DROP INDEX IF EXISTS idx_case_documents_superseded_by;
DROP INDEX IF EXISTS idx_case_documents_uploaded_status;
DROP INDEX IF EXISTS idx_case_documents_case_type_status;
DROP INDEX IF EXISTS idx_document_types_case_type;

-- ---------------------------------------------------------------------------
-- 4) Drop tables (dependency order)
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS document_verification_tasks;
DROP TABLE IF EXISTS sensitive_fields;
DROP TABLE IF EXISTS document_requests;
DROP TABLE IF EXISTS case_documents;
DROP TABLE IF EXISTS document_types;
