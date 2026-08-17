-- M10 wave4 T4-1: computer-control security tables. audit_events gains the
-- six wave-4 cc.* actions; cc_security_config is the single-row
-- configuration (enable flag, security level, process blocklist, rate and
-- confirm caps, emergency-stop latch); cc_audit_log is the append-only
-- operation ledger (UPDATE/DELETE aborted by triggers); cc_recent_audit is
-- the bounded newest-200 projection used by the settings panel.
DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0079_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'project.updated', 'project.published', 'project.closed', 'project.reopened', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'memory.created', 'memory.updated', 'memory.deleted', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.created', 'review.status_updated', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'plan.created', 'plan.status_updated', 'node.created', 'node.status_updated', 'ontology.node.created', 'ontology.node.updated', 'ontology.node.deleted', 'ontology.edge.created', 'ontology.edge.updated', 'ontology.edge.deleted', 'skill.created', 'skill.status_updated', 'skill.deleted', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked', 'policy.created', 'policy.updated', 'policy.deactivated', 'skill.updated', 'memory.settings.update', 'memory.fact.flag', 'memory.fact.unflag', 'memory.growth.enroll', 'memory.growth.decide', 'memory.purge', 'queue.input', 'queue.withdraw', 'queue.consume', 'skill.category_set', 'skill.category_seeded', 'browser.connected', 'browser.disconnected', 'browser.navigated', 'browser.data.cleared', 'browser.permission.granted', 'browser.permission.denied', 'cc.config.updated', 'cc.emergency.stopped', 'cc.operation.executed', 'cc.operation.blocked', 'cc.operation.confirmed', 'cc.tool.denied')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0079_old;
DROP TABLE audit_events_0079_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);

CREATE TABLE cc_security_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    security_level TEXT NOT NULL DEFAULT 'standard' CHECK (security_level IN ('standard','strict')),
    allow_critical INTEGER NOT NULL DEFAULT 0 CHECK (allow_critical IN (0,1)),
    process_blocklist_json TEXT NOT NULL DEFAULT '[]' CHECK (length(process_blocklist_json) <= 8192),
    max_actions_per_minute INTEGER NOT NULL DEFAULT 30 CHECK (max_actions_per_minute BETWEEN 1 AND 120),
    confirm_timeout_seconds INTEGER NOT NULL DEFAULT 60 CHECK (confirm_timeout_seconds BETWEEN 10 AND 600),
    emergency_stopped INTEGER NOT NULL DEFAULT 0 CHECK (emergency_stopped IN (0,1)),
    emergency_stopped_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE cc_audit_log (
    entry_id TEXT PRIMARY KEY CHECK (length(entry_id) BETWEEN 1 AND 64),
    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 64),
    tool TEXT NOT NULL CHECK (tool IN ('cc.mouse_move','cc.mouse_click','cc.keyboard_type','cc.keyboard_shortcut','cc.screen_capture','cc.get_active_window')),
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 512),
    risk_level TEXT NOT NULL CHECK (risk_level IN ('low','medium','high','critical')),
    status TEXT NOT NULL CHECK (status IN ('executed','blocked','denied','failed','stopped')),
    layer TEXT NOT NULL DEFAULT '' CHECK (layer IN ('','intent','input-filter','process-monitor')),
    detail_json TEXT NOT NULL CHECK (length(detail_json) BETWEEN 2 AND 4096),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_ccaudit_session ON cc_audit_log(session_id, created_at DESC);
CREATE INDEX ix_ccaudit_status ON cc_audit_log(status, created_at DESC);

CREATE TRIGGER trg_ccaudit_no_update BEFORE UPDATE ON cc_audit_log
    BEGIN SELECT RAISE(ABORT, 'M10-CC-003'); END;
CREATE TRIGGER trg_ccaudit_no_delete BEFORE DELETE ON cc_audit_log
    BEGIN SELECT RAISE(ABORT, 'M10-CC-003'); END;

CREATE VIEW cc_recent_audit AS SELECT entry_id, session_id, tool, action, risk_level, status, layer, detail_json, created_at FROM cc_audit_log ORDER BY created_at DESC, entry_id DESC LIMIT 200;
