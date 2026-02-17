-- 000013_audit_trail.down.sql
-- Rollback: drop audit_trail table.

DROP TABLE IF EXISTS audit_trail CASCADE;
