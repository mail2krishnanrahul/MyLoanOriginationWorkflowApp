-- 000020_outbox_processing_status.down.sql

ALTER TABLE events_outbox DROP CONSTRAINT IF EXISTS events_outbox_status_check;
ALTER TABLE events_outbox ADD CONSTRAINT events_outbox_status_check
    CHECK (status IN ('PENDING', 'DELIVERED', 'FAILED'));
