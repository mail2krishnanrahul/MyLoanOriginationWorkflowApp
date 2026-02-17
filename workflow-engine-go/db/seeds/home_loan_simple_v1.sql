-- home_loan_simple_v1.sql
-- Seeds a simple 1-step HOME_LOAN case type for testing.
-- One stage → one activity → one task (SYSTEM type).

INSERT INTO case_types (code, version, name, description, config, status)
VALUES (
    'HOME_LOAN',
    1,
    'Home Loan Application (Simple)',
    'Fictitious 1-step home loan workflow for testing.',
    '{
        "stages": [
            {
                "code": "APPLICATION_REVIEW",
                "name": "Application Review",
                "description": "Single-step review of the home loan application.",
                "sequence_order": 1,
                "activities": [
                    {
                        "code": "DOCUMENT_CHECK",
                        "name": "Document Check",
                        "description": "Verify all applicant documents are in order.",
                        "sequence_order": 1,
                        "task_definitions": [
                            {
                                "code": "VERIFY_DOCUMENTS",
                                "name": "Verify Documents",
                                "description": "Automated verification of submitted loan documents.",
                                "type": "SYSTEM",
                                "required": true,
                                "sequence_order": 1,
                                "config": {
                                    "integration": {
                                        "endpoint": "https://api.doc-verify.example.io/v1/check",
                                        "method": "POST",
                                        "timeout_seconds": 30,
                                        "retry_policy": {
                                            "max_retries": 3,
                                            "backoff_ms": 1000
                                        }
                                    }
                                }
                            }
                        ]
                    }
                ]
            }
        ]
    }'::jsonb,
    'ACTIVE'
)
ON CONFLICT ON CONSTRAINT uq_case_types_code_version DO UPDATE
SET config = EXCLUDED.config,
    status = EXCLUDED.status,
    name   = EXCLUDED.name,
    description = EXCLUDED.description;
