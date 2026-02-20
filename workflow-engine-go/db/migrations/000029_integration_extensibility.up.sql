-- 000029_integration_extensibility.up.sql
--
-- Integration & extensibility model.
-- Non-obvious design decision:
-- 1) All operational indexes lead with tenant_id to preserve tenant pruning.
-- 2) Webhook delivery is always queued (never inline HTTP) to preserve outbox atomicity.
-- 3) integration_audit_log is append-only via trigger guard.

-- ---------------------------------------------------------------------------
-- 1) Webhook subscriptions
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    subscription_id      UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    subscriber_code      VARCHAR(120) NOT NULL,
    target_url           TEXT         NOT NULL,
    event_types          TEXT[]       NOT NULL DEFAULT '{}',
    signing_secret_enc   BYTEA        NOT NULL,
    status               VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE'
                         CHECK (status IN ('ACTIVE', 'PAUSED', 'FAILED')),
    max_failures         INT          NOT NULL DEFAULT 5 CHECK (max_failures > 0),
    failure_count        INT          NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
    headers              JSONB        NOT NULL DEFAULT '{}',
    timeout_seconds      INT          NOT NULL DEFAULT 10 CHECK (timeout_seconds > 0 AND timeout_seconds <= 120),
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_webhook_subscriptions_tenant_code_000029 UNIQUE (tenant_id, subscriber_code),
    CONSTRAINT chk_webhook_subscriptions_target_url_https_000029 CHECK (target_url ~ '^https://')
);

COMMENT ON TABLE webhook_subscriptions IS
'Tenant-scoped webhook subscriptions. Empty event_types means subscribe to all events.';
COMMENT ON COLUMN webhook_subscriptions.signing_secret_enc IS
'Encrypted signing secret used for X-Webhook-Signature HMAC-SHA256.';

CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_tenant_status_000029
    ON webhook_subscriptions (tenant_id, status, subscriber_code);

CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_tenant_created_000029
    ON webhook_subscriptions (tenant_id, created_at DESC);

DROP TRIGGER IF EXISTS webhook_subscriptions_updated_at_000029 ON webhook_subscriptions;
CREATE TRIGGER webhook_subscriptions_updated_at_000029
    BEFORE UPDATE ON webhook_subscriptions
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 2) Webhook delivery queue
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS webhook_delivery_queue (
    delivery_id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id        UUID         NOT NULL REFERENCES webhook_subscriptions(subscription_id) ON DELETE CASCADE,
    tenant_id              UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    event_type             VARCHAR(120) NOT NULL,
    payload                JSONB        NOT NULL DEFAULT '{}',
    status                 VARCHAR(20)  NOT NULL DEFAULT 'PENDING'
                           CHECK (status IN ('PENDING', 'DELIVERED', 'FAILED', 'ABANDONED')),
    attempts               INT          NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts           INT          NOT NULL CHECK (max_attempts > 0),
    scheduled_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    delivered_at           TIMESTAMPTZ,
    last_attempt_at        TIMESTAMPTZ,
    response_status_code   INT,
    response_body          TEXT,
    error_detail           JSONB,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT chk_webhook_delivery_response_body_len_000029 CHECK (response_body IS NULL OR length(response_body) <= 1024)
);

COMMENT ON TABLE webhook_delivery_queue IS
'Per-subscription delivery queue populated atomically with outbox rows.';
COMMENT ON COLUMN webhook_delivery_queue.max_attempts IS
'Copied from subscription at enqueue-time to keep retry behavior stable for in-flight rows.';

CREATE INDEX IF NOT EXISTS idx_webhook_delivery_queue_tenant_poll_000029
    ON webhook_delivery_queue (tenant_id, status, scheduled_at ASC, created_at ASC)
    WHERE status IN ('PENDING', 'FAILED');

CREATE INDEX IF NOT EXISTS idx_webhook_delivery_queue_tenant_subscription_000029
    ON webhook_delivery_queue (tenant_id, subscription_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_webhook_delivery_queue_tenant_event_000029
    ON webhook_delivery_queue (tenant_id, event_type, created_at DESC);

DROP TRIGGER IF EXISTS webhook_delivery_queue_updated_at_000029 ON webhook_delivery_queue;
CREATE TRIGGER webhook_delivery_queue_updated_at_000029
    BEFORE UPDATE ON webhook_delivery_queue
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 3) External service registry
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS external_services (
    service_id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    service_code            VARCHAR(120) NOT NULL,
    display_name            VARCHAR(255) NOT NULL,
    protocol                VARCHAR(30)  NOT NULL
                            CHECK (protocol IN ('HTTP_CALLBACK', 'POLLING', 'EVENT_DRIVEN')),
    health_check_url        TEXT,
    status                  VARCHAR(20)  NOT NULL DEFAULT 'ACTIVE'
                            CHECK (status IN ('ACTIVE', 'DEGRADED', 'OFFLINE', 'DECOMMISSIONED')),
    consecutive_failures    INT          NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    last_health_check_at    TIMESTAMPTZ,
    last_successful_at      TIMESTAMPTZ,
    metadata                JSONB        NOT NULL DEFAULT '{}',
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT uq_external_services_tenant_code_000029 UNIQUE (tenant_id, service_code),
    CONSTRAINT chk_external_services_health_url_https_000029 CHECK (health_check_url IS NULL OR health_check_url ~ '^https://')
);

COMMENT ON TABLE external_services IS
'Polyglot external service registry keyed by tenant/service_code.';

CREATE INDEX IF NOT EXISTS idx_external_services_tenant_status_000029
    ON external_services (tenant_id, status, service_code);

CREATE INDEX IF NOT EXISTS idx_external_services_tenant_health_000029
    ON external_services (tenant_id, status, last_health_check_at ASC)
    WHERE health_check_url IS NOT NULL
      AND status IN ('ACTIVE', 'DEGRADED');

DROP TRIGGER IF EXISTS external_services_updated_at_000029 ON external_services;
CREATE TRIGGER external_services_updated_at_000029
    BEFORE UPDATE ON external_services
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 4) Unified idempotency keys
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS idempotency_keys (
    keyspace         VARCHAR(40)  NOT NULL
                     CHECK (keyspace IN (
                         'TASK_COMPLETION',
                         'EXTERNAL_EVENT_INGESTION',
                         'WEBHOOK_DELIVERY'
                     )),
    key              TEXT         NOT NULL,
    tenant_id        UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    reference_id     TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (keyspace, key)
);

COMMENT ON TABLE idempotency_keys IS
'Single store for idempotency keyspaces (task completion, external ingestion, webhook delivery).';

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_tenant_keyspace_exp_000029
    ON idempotency_keys (tenant_id, keyspace, expires_at ASC);

CREATE INDEX IF NOT EXISTS idx_idempotency_keys_tenant_exp_000029
    ON idempotency_keys (tenant_id, expires_at ASC);

-- ---------------------------------------------------------------------------
-- 5) Integration event catalogue
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS event_type_catalogue (
    event_type_code         VARCHAR(120) PRIMARY KEY,
    direction               VARCHAR(20)  NOT NULL
                            CHECK (direction IN ('EMITTED', 'CONSUMED', 'BOTH')),
    description             TEXT         NOT NULL,
    payload_schema          JSONB        NOT NULL,
    introduced_in_version   TEXT         NOT NULL,
    deprecated_at           TIMESTAMPTZ,
    example_payload         JSONB,
    created_at              TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE event_type_catalogue IS
'Machine-readable catalogue for event contracts and JSON Schemas.';

CREATE INDEX IF NOT EXISTS idx_event_type_catalogue_direction_000029
    ON event_type_catalogue (direction, event_type_code);

DROP TRIGGER IF EXISTS event_type_catalogue_updated_at_000029 ON event_type_catalogue;
CREATE TRIGGER event_type_catalogue_updated_at_000029
    BEFORE UPDATE ON event_type_catalogue
    FOR EACH ROW
    EXECUTE FUNCTION trg_set_updated_at();

-- ---------------------------------------------------------------------------
-- 6) Integration audit log (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS integration_audit_log (
    audit_id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             UUID         NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    direction             VARCHAR(20)  NOT NULL CHECK (direction IN ('INBOUND', 'OUTBOUND')),
    integration_type      VARCHAR(40)  NOT NULL CHECK (integration_type IN (
                            'WEBHOOK',
                            'EXTERNAL_TASK_COMPLETION',
                            'EXTERNAL_EVENT_INGESTION',
                            'HEALTH_CHECK'
                          )),
    source_or_target      TEXT         NOT NULL,
    event_type            VARCHAR(120),
    case_id               UUID         REFERENCES cases(id) ON DELETE SET NULL,
    task_id               UUID         REFERENCES tasks(id) ON DELETE SET NULL,
    status                VARCHAR(30)  NOT NULL CHECK (status IN ('SUCCESS', 'FAILURE', 'DUPLICATE_REJECTED')),
    request_payload       JSONB,
    response_payload      JSONB,
    duration_ms           INT          NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    occurred_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now()
);

COMMENT ON TABLE integration_audit_log IS
'Best-effort append-only integration audit log; callers must not fail business flow on insert errors.';

CREATE INDEX IF NOT EXISTS idx_integration_audit_log_tenant_time_000029
    ON integration_audit_log (tenant_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_integration_audit_log_tenant_filters_000029
    ON integration_audit_log (tenant_id, direction, integration_type, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_integration_audit_log_tenant_case_task_000029
    ON integration_audit_log (tenant_id, case_id, task_id, occurred_at DESC)
    WHERE case_id IS NOT NULL OR task_id IS NOT NULL;

CREATE OR REPLACE FUNCTION trg_block_integration_audit_log_mutation_000029()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'integration_audit_log is append-only; updates/deletes are not allowed';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS integration_audit_log_block_mutation_000029 ON integration_audit_log;
CREATE TRIGGER integration_audit_log_block_mutation_000029
    BEFORE UPDATE OR DELETE ON integration_audit_log
    FOR EACH ROW
    EXECUTE FUNCTION trg_block_integration_audit_log_mutation_000029();
