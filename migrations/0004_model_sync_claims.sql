ALTER TABLE idempotency_records RENAME TO idempotency_records_v3;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at)
SELECT operation,idempotency_key,request_digest,response_json,created_at,expires_at FROM idempotency_records_v3;
DROP TABLE idempotency_records_v3;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

ALTER TABLE audit_events RENAME TO audit_events_v3;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at)
SELECT id,action,aggregate_id,actor,metadata_json,created_at FROM audit_events_v3;
DROP TABLE audit_events_v3;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);

CREATE TABLE idempotency_claims (
    operation TEXT NOT NULL CHECK (operation = 'provider.model.sync'),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
CREATE INDEX ix_idempotency_claims_expires ON idempotency_claims(expires_at);
