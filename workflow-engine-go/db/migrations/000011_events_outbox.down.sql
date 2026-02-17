-- 000011_events_outbox.down.sql
-- Rollback: drop events_outbox table.

DROP TABLE IF EXISTS events_outbox CASCADE;
