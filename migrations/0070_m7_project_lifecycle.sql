-- M7 project lifecycle: extend projects with the frozen A-N creation contract
-- (06-完整UI界面设计 01/PROJECT MANAGEMENT) and the created->active->closed gate.
-- legacy_alter_table=ON keeps FOREIGN KEY clauses in dependent tables pointing
-- at "projects" while the table is renamed, rebuilt, and swapped back in.
PRAGMA legacy_alter_table=ON;
ALTER TABLE projects RENAME TO projects_0070_old;
CREATE TABLE projects (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200 AND name = trim(name)),
    project_code TEXT NOT NULL CHECK (length(project_code) BETWEEN 4 AND 16 AND project_code GLOB 'ITM[0-9]*'),
    project_type TEXT NOT NULL DEFAULT 'implementation' CHECK (project_type IN ('implementation', 'operations', 'enhancement')),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 500),
    objective TEXT NOT NULL DEFAULT '' CHECK (length(objective) <= 2000),
    client TEXT NOT NULL DEFAULT '' CHECK (length(client) <= 200),
    contract_no TEXT NOT NULL DEFAULT '' CHECK (length(contract_no) <= 100),
    amount REAL NOT NULL DEFAULT 0 CHECK (amount >= 0 AND amount <= 999999999999),
    budget REAL NOT NULL DEFAULT 0 CHECK (budget >= 0 AND budget <= 999999999999),
    plan_start TEXT NOT NULL DEFAULT '' CHECK (plan_start = '' OR (length(plan_start) = 10 AND plan_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')),
    plan_end TEXT NOT NULL DEFAULT '' CHECK (plan_end = '' OR (length(plan_end) = 10 AND plan_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')),
    remark TEXT NOT NULL DEFAULT '' CHECK (length(remark) <= 2000),
    close_reason TEXT NOT NULL DEFAULT '' CHECK (length(close_reason) <= 500),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('created', 'active', 'closed', 'archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);
INSERT INTO projects(id,name,project_code,project_type,description,summary,objective,client,contract_no,amount,budget,plan_start,plan_end,remark,close_reason,status,created_at,updated_at,version)
SELECT id,name,'ITM'||substr('0000'||rowid, -5, 5),'implementation','','','','','',0,0,'','','','',status,created_at,updated_at,version
FROM projects_0070_old ORDER BY created_at;
DROP TABLE projects_0070_old;
PRAGMA legacy_alter_table=OFF;
CREATE UNIQUE INDEX ix_projects_code ON projects(project_code);
CREATE INDEX ix_projects_status_created ON projects(status, created_at, id);

-- audit_actions + idempotency operations for the project lifecycle gate
DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0070_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'project.updated', 'project.published', 'project.closed', 'project.reopened', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'memory.created', 'memory.updated', 'memory.deleted', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.created', 'review.status_updated', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'plan.created', 'plan.status_updated', 'node.created', 'node.status_updated', 'ontology.node.created', 'ontology.node.updated', 'ontology.node.deleted', 'ontology.edge.created', 'ontology.edge.updated', 'ontology.edge.deleted', 'skill.created', 'skill.status_updated', 'skill.deleted', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked', 'policy.created', 'policy.updated', 'policy.deactivated', 'skill.updated')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0070_old;
DROP TABLE audit_events_0070_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);

DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0070_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'project.update', 'project.publish', 'project.close', 'project.reopen', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle', 'delegation.create', 'delegation.settle', 'merge.submit', 'openapi.parse', 'complexity.decide', 'skill.import.discover', 'skill.import.inspect', 'skill.import.submit', 'skill.import.approve', 'skill.import.reject', 'skill.import.revoke')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0070_old;
DROP TABLE idempotency_records_0070_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);
