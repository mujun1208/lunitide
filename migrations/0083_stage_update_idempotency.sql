-- 0083: stage.update idempotency operation for PM stage status mutations.

DROP INDEX IF EXISTS ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0083_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'project.update', 'project.publish', 'project.close', 'project.reopen', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'stage.update', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle', 'delegation.create', 'delegation.settle', 'merge.submit', 'openapi.parse', 'complexity.decide', 'skill.import.discover', 'skill.import.inspect', 'skill.import.submit', 'skill.import.approve', 'skill.import.reject', 'skill.import.revoke')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0083_old;
DROP TABLE idempotency_records_0083_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);
