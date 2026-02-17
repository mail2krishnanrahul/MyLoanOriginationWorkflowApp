-- 000020_outbox_processing_status.up.sql
-- Add 'PROCESSING' to the events_outbox status CHECK constraint.
-- Required by the poll-and-claim pattern: events are set to PROCESSING
-- when claimed by a poller, preventing duplicate delivery.

ALTER TABLE events_outbox DROP CONSTRAINT IF EXISTS events_outbox_status_check;
ALTER TABLE events_outbox ADD CONSTRAINT events_outbox_status_check
    CHECK (status IN ('PENDING', 'PROCESSING', 'DELIVERED', 'FAILED'));
