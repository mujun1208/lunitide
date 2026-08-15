-- 0055: audit_actions for the governance policy and skill field-update
-- domains. The 2026-08-15 full-chain review found CreatePolicy/UpdatePolicy/
-- DeactivatePolicy/UpdateSkill/UpdateSkillFields writing outside the audited
-- path; wiring them into execWithAudit requires these four actions in the
-- audit_events CHECK catalog. Rebuild follows the 0047-0054 pattern.

DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0055_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'memory.created', 'memory.updated', 'memory.deleted', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.created', 'review.status_updated', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'plan.created', 'plan.status_updated', 'node.created', 'node.status_updated', 'ontology.node.created', 'ontology.node.updated', 'ontology.node.deleted', 'ontology.edge.created', 'ontology.edge.updated', 'ontology.edge.deleted', 'skill.created', 'skill.status_updated', 'skill.deleted', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked', 'policy.created', 'policy.updated', 'policy.deactivated', 'skill.updated')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0055_old;
DROP TABLE audit_events_0055_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
