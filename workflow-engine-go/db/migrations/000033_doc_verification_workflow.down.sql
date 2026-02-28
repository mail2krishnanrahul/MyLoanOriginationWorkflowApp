-- 000033_doc_verification_workflow.down.sql
-- Reverses all changes from 000033 up.

-- Drop constraints before dropping tables
ALTER TABLE cases
    DROP CONSTRAINT IF EXISTS fk_cases_assigned_user_033;

ALTER TABLE tasks
    DROP COLUMN IF EXISTS is_adhoc,
    DROP COLUMN IF EXISTS is_blocking,
    DROP COLUMN IF EXISTS external_reference,
    DROP COLUMN IF EXISTS display_name,
    DROP COLUMN IF EXISTS description;

ALTER TABLE cases
    DROP COLUMN IF EXISTS assigned_user_id,
    DROP COLUMN IF EXISTS case_complexity,
    DROP COLUMN IF EXISTS required_skills,
    DROP COLUMN IF EXISTS submitted_by_banker_id;

DROP TABLE IF EXISTS qa_staged_changes CASCADE;
DROP TABLE IF EXISTS additional_info_requests CASCADE;
DROP TABLE IF EXISTS credit_memo_checklist CASCADE;
DROP TABLE IF EXISTS case_tags CASCADE;
DROP TABLE IF EXISTS document_error_tags CASCADE;
DROP TABLE IF EXISTS case_documents CASCADE;
