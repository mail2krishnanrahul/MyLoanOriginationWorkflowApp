-- 000029_integration_extensibility.down.sql
--
-- Non-destructive rollback:
-- To avoid data loss, integration tables are archived via rename instead of DROP.
-- This allows roll-forward (up) to recreate empty live tables while preserving history.

DROP TRIGGER IF EXISTS integration_audit_log_block_mutation_000029 ON integration_audit_log;
DROP FUNCTION IF EXISTS trg_block_integration_audit_log_mutation_000029();

DROP TRIGGER IF EXISTS event_type_catalogue_updated_at_000029 ON event_type_catalogue;
DROP TRIGGER IF EXISTS external_services_updated_at_000029 ON external_services;
DROP TRIGGER IF EXISTS webhook_delivery_queue_updated_at_000029 ON webhook_delivery_queue;
DROP TRIGGER IF EXISTS webhook_subscriptions_updated_at_000029 ON webhook_subscriptions;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'integration_audit_log') THEN
        EXECUTE 'ALTER TABLE integration_audit_log RENAME TO integration_audit_log_archive_000029';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'event_type_catalogue') THEN
        EXECUTE 'ALTER TABLE event_type_catalogue RENAME TO event_type_catalogue_archive_000029';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'idempotency_keys') THEN
        EXECUTE 'ALTER TABLE idempotency_keys RENAME TO idempotency_keys_archive_000029';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'external_services') THEN
        EXECUTE 'ALTER TABLE external_services RENAME TO external_services_archive_000029';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'webhook_delivery_queue') THEN
        EXECUTE 'ALTER TABLE webhook_delivery_queue RENAME TO webhook_delivery_queue_archive_000029';
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'webhook_subscriptions') THEN
        EXECUTE 'ALTER TABLE webhook_subscriptions RENAME TO webhook_subscriptions_archive_000029';
    END IF;
END;
$$;
