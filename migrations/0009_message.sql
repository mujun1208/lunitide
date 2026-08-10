CREATE TABLE messages (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role = 'user'),
    status TEXT NOT NULL DEFAULT 'completed' CHECK (status = 'completed'),
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 9007199254740991),
    created_at TEXT NOT NULL,
    UNIQUE (session_id, sequence)
);
CREATE INDEX ix_messages_session_sequence ON messages(session_id, sequence);
CREATE TABLE message_parts (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal = 1),
    type TEXT NOT NULL DEFAULT 'text' CHECK (type = 'text'),
    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 2048 AND length(CAST(text AS BLOB)) <= 8192),
    PRIMARY KEY (message_id, ordinal)
);

CREATE TABLE message_session_state (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE RESTRICT,
    last_sequence INTEGER NOT NULL CHECK (last_sequence BETWEEN 0 AND 9007199254740991),
    message_count INTEGER NOT NULL CHECK (message_count BETWEEN 0 AND 9007199254740991),
    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 268435456),
    CHECK (last_sequence = message_count)
);
INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes)
SELECT s.id,COALESCE(MAX(m.sequence),0),COUNT(m.id),COALESCE(SUM(length(CAST(p.text AS BLOB))),0) FROM sessions s LEFT JOIN messages m ON m.session_id=s.id LEFT JOIN message_parts p ON p.message_id=m.id GROUP BY s.id;
CREATE TABLE message_project_usage (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE RESTRICT,
    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 67108864)
);
INSERT INTO message_project_usage(project_id,text_bytes)
SELECT p.id,COALESCE(SUM(length(CAST(mp.text AS BLOB))),0) FROM projects p LEFT JOIN sessions s ON s.project_id=p.id LEFT JOIN messages m ON m.session_id=s.id LEFT JOIN message_parts mp ON mp.message_id=m.id GROUP BY p.id;
CREATE TABLE message_workspace_usage (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 268435456)
);
INSERT INTO message_workspace_usage(singleton,text_bytes) SELECT 1,COALESCE(SUM(length(CAST(text AS BLOB))),0) FROM message_parts;

DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0009_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'message.append')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0009_old;
DROP TABLE idempotency_records_0009_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0009_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'message.appended')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0009_old;
DROP TABLE audit_events_0009_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
