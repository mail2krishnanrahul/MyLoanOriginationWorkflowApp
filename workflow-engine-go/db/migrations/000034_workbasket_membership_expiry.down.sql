-- 000034_workbasket_membership_expiry.down.sql
DROP INDEX IF EXISTS idx_workbasket_members_expiry;

ALTER TABLE workbasket_members
    DROP COLUMN IF EXISTS expires_at;
