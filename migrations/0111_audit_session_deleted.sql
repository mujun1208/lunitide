-- 0111: allow 'session.deleted' in the audit action enum.
-- session.delete was the one session mutation that hard-deleted rows (messages,
-- parts, token ledger, the session itself) with no audit trail, because the
-- action CHECK did not permit 'session.deleted' — an audit insert would have
-- aborted the delete transaction. SQLite cannot ALTER a CHECK, so rebuild the
-- table with the widened enum and re-seal it append-only. The two triggers are
-- dropped first and recreated byte-for-byte so the schema-drift guard still
-- matches expectedSchemaSQL.
DROP TRIGGER trg_audit_append_only;
DROP TRIGGER trg_audit_nodelete;
ALTER TABLE audit_events RENAME TO audit_events_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'project.updated', 'project.published', 'project.closed', 'project.reopened', 'project.advanced', 'project.deleted', 'session.created', 'session.updated', 'session.deleted', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'memory.created', 'memory.updated', 'memory.deleted', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.created', 'review.status_updated', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'plan.created', 'plan.status_updated', 'node.created', 'node.status_updated', 'ontology.node.created', 'ontology.node.updated', 'ontology.node.deleted', 'ontology.edge.created', 'ontology.edge.updated', 'ontology.edge.deleted', 'skill.created', 'skill.status_updated', 'skill.deleted', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked', 'policy.created', 'policy.updated', 'policy.deactivated', 'skill.updated', 'memory.settings.update', 'memory.fact.flag', 'memory.fact.unflag', 'memory.growth.enroll', 'memory.growth.decide', 'memory.purge', 'queue.input', 'queue.withdraw', 'queue.consume', 'skill.category_set', 'skill.category_seeded', 'browser.connected', 'browser.disconnected', 'browser.navigated', 'browser.data.cleared', 'browser.permission.granted', 'browser.permission.denied', 'cc.config.updated', 'cc.emergency.stopped', 'cc.operation.executed', 'cc.operation.blocked', 'cc.operation.confirmed', 'cc.tool.denied', 'asset_template.created', 'asset_template.status', 'asset_template.deleted', 'project_deliverable.upserted', 'project_deliverable.gate_confirmed', 'project_attachment.created')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events (id, action, aggregate_id, actor, metadata_json, created_at)
    SELECT id, action, aggregate_id, actor, metadata_json, created_at FROM audit_events_old;
DROP TABLE audit_events_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
CREATE TRIGGER trg_audit_append_only BEFORE UPDATE ON audit_events
    BEGIN SELECT RAISE(ABORT, 'M10-AUD-001'); END;
CREATE TRIGGER trg_audit_nodelete BEFORE DELETE ON audit_events
    BEGIN SELECT RAISE(ABORT, 'M10-AUD-001'); END;
