ALTER TABLE providers ADD COLUMN origin_fingerprint TEXT NOT NULL
    DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'
    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*');

CREATE TABLE provider_tests (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    CHECK (completed_at IS NULL OR started_at IS NOT NULL)
);
CREATE INDEX ix_provider_tests_provider_created ON provider_tests(provider_id, created_at DESC);

CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.delete')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    topic TEXT NOT NULL CHECK (length(topic) BETWEEN 1 AND 128),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    payload_json TEXT NOT NULL CHECK (length(payload_json) BETWEEN 2 AND 65536),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'completed', 'failed', 'dead_letter')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000),
    available_at TEXT NOT NULL,
    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),
    lease_until TEXT,
    last_error TEXT CHECK (last_error IS NULL OR length(last_error) BETWEEN 1 AND 2000),
    created_at TEXT NOT NULL,
    completed_at TEXT,
    CHECK ((status = 'claimed') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK ((status IN ('completed', 'failed', 'dead_letter')) = (completed_at IS NOT NULL))
);
CREATE INDEX ix_outbox_claim ON outbox_events(status, available_at, lease_until);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.deleted')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);

CREATE TABLE credential_adoptions (
    credential_ref TEXT PRIMARY KEY CHECK (length(credential_ref) BETWEEN 1 AND 256),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 2048),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),
    receipt_id TEXT NOT NULL UNIQUE CHECK (length(receipt_id) BETWEEN 1 AND 64),
    adopted_at TEXT NOT NULL
);
CREATE INDEX ix_credential_adoptions_provider ON credential_adoptions(provider_id);

CREATE TRIGGER providers_credential_ref_insert
BEFORE INSERT ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256
BEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END;
CREATE TRIGGER providers_credential_ref_update
BEFORE UPDATE OF credential_ref ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256
BEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END;
