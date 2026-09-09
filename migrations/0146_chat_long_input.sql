-- Widen typed input without deleting history, receipts, usage counters or FTS.
-- Store.initialize owns the transaction and disables foreign keys for rebuilds.
DROP TRIGGER IF EXISTS trg_message_fts_ai;
DROP TRIGGER IF EXISTS trg_message_fts_ad;
CREATE TABLE _message_parts_0146_new (
    message_id TEXT NOT NULL REFERENCES "messages"(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal = 1),
    type TEXT NOT NULL DEFAULT 'text' CHECK (type = 'text'),
    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 32768 AND length(CAST(text AS BLOB)) <= 131072),
    PRIMARY KEY (message_id, ordinal)
);
INSERT INTO _message_parts_0146_new SELECT * FROM message_parts;
DROP TABLE message_parts;
ALTER TABLE _message_parts_0146_new RENAME TO message_parts;
CREATE TABLE _queued_user_messages_0146_new (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id TEXT CHECK (run_id IS NULL OR (length(run_id) = 26 AND substr(run_id, 1, 1) GLOB '[0-7]' AND run_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    seq INTEGER NOT NULL CHECK (seq > 0),
    payload TEXT NOT NULL CHECK (length(payload) BETWEEN 1 AND 32768 AND length(CAST(payload AS BLOB)) <= 131072),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','injected','withdrawn')),
    mark TEXT NOT NULL DEFAULT 'turn_boundary' CHECK (mark IN ('turn_boundary','with_approval')),
    request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL, office_task_id TEXT
  CHECK (office_task_id IS NULL OR
    (length(office_task_id)=26 AND substr(office_task_id,1,1) GLOB '[0-7]'
     AND office_task_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    UNIQUE (session_id, seq),
    UNIQUE (session_id, request_id)
);
INSERT INTO _queued_user_messages_0146_new SELECT * FROM queued_user_messages;
DROP TABLE queued_user_messages;
ALTER TABLE _queued_user_messages_0146_new RENAME TO queued_user_messages;
CREATE TABLE _idempotency_records_0146_new (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'project.update', 'project.publish', 'project.close', 'project.reopen', 'project.advanceStatus', 'project.delete', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'stage.update', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle', 'delegation.create', 'delegation.settle', 'merge.submit', 'openapi.parse', 'complexity.decide', 'skill.import.discover', 'skill.import.inspect', 'skill.import.submit', 'skill.import.approve', 'skill.import.reject', 'skill.import.revoke', 'deliverable.upsert', 'deliverable.confirmGate', 'projectAttachment.ingest', 'release.buildPackage')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536 OR (operation = 'message.append' AND length(response_json) BETWEEN 2 AND 245760)),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO _idempotency_records_0146_new SELECT * FROM idempotency_records;
DROP TABLE idempotency_records;
ALTER TABLE _idempotency_records_0146_new RENAME TO idempotency_records;
CREATE INDEX ix_quem_session ON queued_user_messages(session_id, status, seq);
CREATE INDEX ix_quem_recent ON queued_user_messages(session_id, created_at);
CREATE INDEX ix_quem_office_scope ON queued_user_messages(session_id,office_task_id,status,seq);
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);
CREATE TRIGGER IF NOT EXISTS trg_message_fts_ai AFTER INSERT ON message_parts BEGIN
  INSERT INTO message_fts(session_id, message_id, role, sequence, text)
  SELECT m.session_id, m.id, m.role, m.sequence, new.text
  FROM messages m WHERE m.id = new.message_id;
END;
CREATE TRIGGER IF NOT EXISTS trg_message_fts_ad AFTER DELETE ON message_parts BEGIN
  DELETE FROM message_fts WHERE message_id = old.message_id;
END;
