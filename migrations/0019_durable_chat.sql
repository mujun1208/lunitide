-- P2 Durable Chat: widen messages/message_parts CHECK constraints and extend
-- idempotency/audit enums to support assistant durable write.
-- foreign_keys is OFF during migration; integrity is verified after commit.

-- 1. Rebuild messages: role IN ('user','assistant'), status IN ('completed','failed')
CREATE TABLE _messages_new (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'assistant')),
    status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'failed')),
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 9007199254740991),
    created_at TEXT NOT NULL,
    UNIQUE (session_id, sequence)
);
INSERT INTO _messages_new SELECT * FROM messages;

-- 2. Rebuild message_parts: widen text to 16384 codepoints / 65536 bytes, FK → _messages_new
CREATE TABLE _message_parts_new (
    message_id TEXT NOT NULL REFERENCES _messages_new(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal = 1),
    type TEXT NOT NULL DEFAULT 'text' CHECK (type = 'text'),
    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 16384 AND length(CAST(text AS BLOB)) <= 65536),
    PRIMARY KEY (message_id, ordinal)
);
INSERT INTO _message_parts_new SELECT * FROM message_parts;

-- 3. Drop old tables (foreign_keys=OFF, no CASCADE triggered)
DROP TABLE message_parts;
DROP TABLE messages;

-- 4. Rename new tables; SQLite updates FK references automatically
ALTER TABLE _message_parts_new RENAME TO message_parts;
ALTER TABLE _messages_new RENAME TO messages;

-- 5. Recreate index
CREATE INDEX ix_messages_session_sequence ON messages(session_id, sequence);

-- 6. Rebuild idempotency_records: add 'message.append-assistant' operation
DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO _idempotency_0019_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'message.append', 'stage.create', 'message.append-assistant')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM _idempotency_0019_old;
DROP TABLE _idempotency_0019_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

-- 7. Rebuild audit_events: add 'message.assistant.appended' action
DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO _audit_0019_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'message.appended', 'stage.created', 'stage.updated', 'message.assistant.appended')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM _audit_0019_old;
DROP TABLE _audit_0019_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
