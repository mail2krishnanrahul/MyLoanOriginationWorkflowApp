-- 000032_business_lending_deal_ingestion.down.sql

DROP TRIGGER IF EXISTS case_deal_links_updated_at_000032 ON case_deal_links;
DROP TABLE IF EXISTS case_deal_links;

DROP TABLE IF EXISTS deal_ingest_idempotency_cache;
DROP TABLE IF EXISTS deal_ingestion_history;

DROP TRIGGER IF EXISTS ingested_deals_updated_at_000032 ON ingested_deals;
DROP TABLE IF EXISTS ingested_deals;

-- Keep case_types row to avoid destructive rollback of potentially used runtime type.
