-- 000032_business_lending_deal_ingestion.up.sql
-- Business lending snapshot ingestion persistence and deterministic document-verification case linkage.

-- ---------------------------------------------------------------------------
-- 1) Canonical snapshot store by deal
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ingested_deals (
    tenant_id                UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    deal_id                  TEXT         NOT NULL,
    snapshot_hash            TEXT         NOT NULL,
    deal_snapshot            JSONB        NOT NULL,
    first_seen_at            TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_seen_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_snapshot_timestamp  TIMESTAMPTZ  NOT NULL,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, deal_id)
);

COMMENT ON TABLE ingested_deals IS
'Latest canonical deal snapshot per tenant/deal_id for snapshot-driven delta detection.';

CREATE INDEX IF NOT EXISTS idx_ingested_deals_tenant_last_seen_000032
    ON ingested_deals (tenant_id, last_seen_at DESC);

DROP TRIGGER IF EXISTS ingested_deals_updated_at_000032 ON ingested_deals;
CREATE TRIGGER ingested_deals_updated_at_000032
    BEFORE UPDATE ON ingested_deals
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 2) Ingestion execution history (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS deal_ingestion_history (
    ingest_id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    deal_id               TEXT         NOT NULL,
    snapshot_timestamp    TIMESTAMPTZ  NOT NULL,
    received_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    snapshot_hash         TEXT         NOT NULL,
    outcome               VARCHAR(40)  NOT NULL,
    case_action           VARCHAR(60)  NOT NULL DEFAULT 'NONE',
    case_id               UUID         REFERENCES cases(id) ON DELETE SET NULL,
    diff_summary          JSONB,
    warnings              JSONB        NOT NULL DEFAULT '[]'::jsonb,
    source_system         VARCHAR(40),
    correlation_id        TEXT,
    exception_detail      JSONB,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE deal_ingestion_history IS
'Append-only audit history of each deal ingestion decision and resulting case action.';

CREATE INDEX IF NOT EXISTS idx_deal_ingestion_history_tenant_deal_time_000032
    ON deal_ingestion_history (tenant_id, deal_id, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_deal_ingestion_history_tenant_outcome_time_000032
    ON deal_ingestion_history (tenant_id, outcome, received_at DESC);

-- ---------------------------------------------------------------------------
-- 3) Idempotency response cache for X-Idempotency-Key
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS deal_ingest_idempotency_cache (
    tenant_id          UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    idempotency_key    TEXT         NOT NULL,
    http_status        INT          NOT NULL,
    response_payload   JSONB        NOT NULL,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at         TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (tenant_id, idempotency_key)
);

COMMENT ON TABLE deal_ingest_idempotency_cache IS
'24-hour response cache for POST /ingest/deals idempotency keys.';

CREATE INDEX IF NOT EXISTS idx_deal_ingest_idempotency_exp_000032
    ON deal_ingest_idempotency_cache (expires_at ASC);

-- ---------------------------------------------------------------------------
-- 4) Case <-> deal linkage and immutable snapshot envelope fields
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS case_deal_links (
    case_id                     UUID         PRIMARY KEY REFERENCES cases(id) ON DELETE CASCADE,
    tenant_id                   UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    deal_id                     TEXT         NOT NULL,
    deal_name                   TEXT,
    customer_reference          TEXT,
    trigger_deal_status         TEXT,
    current_deal_status         TEXT,
    umbrella_limit_amount       NUMERIC(18,2),
    umbrella_limit_currency     TEXT,
    deal_snapshot               JSONB        NOT NULL,
    deal_snapshot_hash          TEXT         NOT NULL,
    last_snapshot_refreshed_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    case_priority               TEXT,
    sla_target_completion_date  DATE,
    sla_warning_threshold_date  DATE,
    notify_assignee             BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at                  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE case_deal_links IS
'Case envelope fields sourced from ingestion. Case-owned workflow state remains in cases/tasks tables.';

CREATE INDEX IF NOT EXISTS idx_case_deal_links_tenant_deal_000032
    ON case_deal_links (tenant_id, deal_id, created_at DESC);

DROP TRIGGER IF EXISTS case_deal_links_updated_at_000032 ON case_deal_links;
CREATE TRIGGER case_deal_links_updated_at_000032
    BEFORE UPDATE ON case_deal_links
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 5) Seed DOCUMENT_VERIFICATION case type used by ingestion if absent
-- ---------------------------------------------------------------------------
DO $$
DECLARE
    active_case_type_exists BOOLEAN;
    next_version INT;
BEGIN
    SELECT EXISTS (
        SELECT 1
        FROM case_types
        WHERE code = 'DOCUMENT_VERIFICATION'
          AND status = 'ACTIVE'
    )
    INTO active_case_type_exists;

    IF NOT active_case_type_exists THEN
        SELECT COALESCE(MAX(version), 0) + 1
          INTO next_version
          FROM case_types
         WHERE code = 'DOCUMENT_VERIFICATION';

        INSERT INTO case_types (code, version, name, description, config, status)
        VALUES (
            'DOCUMENT_VERIFICATION',
            next_version,
            'Document Verification',
            'Ingestion-driven case type for business lending document verification.',
            '{
              "stages": [
                {
                  "code": "DOCUMENT_VERIFICATION",
                  "name": "Document Verification",
                  "description": "Auto-generated verification checklist for ingested deals.",
                  "sequence_order": 1,
                  "activities": [
                    {
                      "code": "DOCUMENT_VERIFICATION_ACTIVITY",
                      "name": "Document Verification",
                      "description": "Generated tasks are attached here by ingestion service.",
                      "sequence_order": 1,
                      "task_definitions": []
                    }
                  ]
                }
              ],
              "max_rework_attempts": 3
            }'::jsonb,
            'ACTIVE'
        );
    END IF;
END;
$$;
