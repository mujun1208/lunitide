-- M4-E: change_set_operation stores the immutable per-path plan plus the
-- original-content snapshot (revert source) and the post-apply digest
-- (revert CAS). The change_set row stays the digest-bound aggregate; this
-- table is its ordered operation list.
CREATE TABLE change_set_operation (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    change_set_id TEXT NOT NULL REFERENCES change_set(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal > 0),
    op TEXT NOT NULL CHECK (op IN ('create','update','delete')),
    path TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 512),
    content TEXT,
    content_digest TEXT CHECK (content_digest IS NULL OR (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*')),
    original_content TEXT,
    original_digest TEXT CHECK (original_digest IS NULL OR (length(original_digest) = 64 AND original_digest NOT GLOB '*[^0-9a-f]*')),
    applied_digest TEXT CHECK (applied_digest IS NULL OR (length(applied_digest) = 64 AND applied_digest NOT GLOB '*[^0-9a-f]*')),
    UNIQUE (change_set_id, ordinal)
);
CREATE INDEX ix_change_set_operation_set ON change_set_operation(change_set_id, ordinal);

-- M4-E: rebuild idempotency_records to admit the changeset.* operations.
DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0032_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'session.update', 'message.append', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0032_old;
DROP TABLE idempotency_records_0032_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

-- M4-E: rebuild audit_events to admit the changeset.* audit actions.
DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0032_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'session.updated', 'message.appended', 'stage.created', 'stage.updated', 'message.assistant.appended', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0032_old;
DROP TABLE audit_events_0032_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
