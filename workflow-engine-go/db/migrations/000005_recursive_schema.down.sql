-- 000005_recursive_schema.down.sql
-- Rollback: drop recursive-schema tables in reverse dependency order.

DROP TABLE IF EXISTS component_instances CASCADE;
DROP TABLE IF EXISTS component_hooks CASCADE;
DROP TABLE IF EXISTS workflow_components CASCADE;
DROP TABLE IF EXISTS version_registry CASCADE;
DROP TABLE IF EXISTS case_definitions CASCADE;
