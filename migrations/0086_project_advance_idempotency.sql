-- 0086: project.advanceStatus / project.delete idempotency + audit actions.

DROP INDEX IF EXISTS ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0086_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'project.update', 'project.publish', 'project.close', 'project.reopen', 'project.advanceStatus', 'project.delete', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'stage.update', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle', 'delegation.create', 'delegation.settle', 'merge.submit', 'openapi.parse', 'complexity.decide', 'skill.import.discover', 'skill.import.inspect', 'skill.import.submit', 'skill.import.approve', 'skill.import.reject', 'skill.import.revoke', 'deliverable.upsert', 'deliverable.confirmGate', 'projectAttachment.ingest', 'release.buildPackage')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0086_old;
DROP TABLE idempotency_records_0086_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

DROP INDEX IF EXISTS ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0086_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'project.updated', 'project.published', 'project.closed', 'project.reopened', 'project.advanced', 'project.deleted', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'memory.created', 'memory.updated', 'memory.deleted', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.created', 'review.status_updated', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'plan.created', 'plan.status_updated', 'node.created', 'node.status_updated', 'ontology.node.created', 'ontology.node.updated', 'ontology.node.deleted', 'ontology.edge.created', 'ontology.edge.updated', 'ontology.edge.deleted', 'skill.created', 'skill.status_updated', 'skill.deleted', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked', 'policy.created', 'policy.updated', 'policy.deactivated', 'skill.updated', 'memory.settings.update', 'memory.fact.flag', 'memory.fact.unflag', 'memory.growth.enroll', 'memory.growth.decide', 'memory.purge', 'queue.input', 'queue.withdraw', 'queue.consume', 'skill.category_set', 'skill.category_seeded', 'browser.connected', 'browser.disconnected', 'browser.navigated', 'browser.data.cleared', 'browser.permission.granted', 'browser.permission.denied', 'cc.config.updated', 'cc.emergency.stopped', 'cc.operation.executed', 'cc.operation.blocked', 'cc.operation.confirmed', 'cc.tool.denied')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0086_old;
DROP TABLE audit_events_0086_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
