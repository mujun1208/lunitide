-- M6 audit actions (T-6.1.x): extension supply-chain lifecycle and MCP
-- endpoint governance. Rebuild-only migration mirroring 0042's pattern.

DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0047_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0047_old;
DROP TABLE audit_events_0047_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);

-- M6 mutations write idempotency records under their own operation names, so
-- the idempotency_records operation CHECK gains extension.install and
-- extension.lifecycle (same rebuild pattern, no data changes).

DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0047_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0047_old;
DROP TABLE idempotency_records_0047_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);