-- M5 T-5.0.2: durable chat Root Run storage. Five m5_ tables per M5/02 DDL
-- (adapted to repo conventions: agent_run FK, TEXT RFC3339 timestamps, ULID and
-- 64-hex digest CHECKs). Design deltas recorded in docs/evidence/m5-day0.txt:
--   - m5_changeset/m5_changeset_item are the AdHocWorkspace ChangeSetService
--     store; M4 change_set/change_set_operation (trusted-review workspace)
--     remain untouched and keep serving the M4 changeset.* review flow.
--   - m5_adhoc_workspace gains used_bytes (quota projection), version
--     (optimistic lock, repo convention) and state 'cleaning_failed'
--     (frozen-params panel state; without it a crashed cleanup loses its
--     durable state). 'near_quota' stays derived, not stored.
--   - m5_workspace_conversion gains phase (crash-recovery journal: preview ->
--     copying -> publishing -> committed) and scope_json (the confirmed copy
--     scope must survive restart to continue or roll back the staging copy).
--   - command_job gains backgrounding columns for the 10s/1MiB threshold
--     (pid_token defeats PID reuse, log_cursor reconnects incremental logs,
--     cancel_deadline bounds tree-kill grace).

CREATE TABLE m5_adhoc_workspace (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    root_canonical TEXT NOT NULL CHECK (length(root_canonical) BETWEEN 1 AND 1024),
    display_path TEXT NOT NULL CHECK (length(display_path) BETWEEN 1 AND 1024),
    grant_json TEXT NOT NULL CHECK (json_valid(grant_json) AND length(grant_json) BETWEEN 2 AND 16384),
    lease_expiry TEXT NOT NULL,
    base_digest TEXT NOT NULL CHECK (length(base_digest) = 64 AND base_digest NOT GLOB '*[^0-9a-f]*'),
    quota_soft INTEGER NOT NULL DEFAULT 2147483648 CHECK (quota_soft > 0),
    quota_hard INTEGER NOT NULL DEFAULT 4294967296 CHECK (quota_hard >= quota_soft),
    used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    state TEXT NOT NULL CHECK (state IN ('active','readonly_full','expiring','cleaning','cleaning_failed','retained','deleted')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE UNIQUE INDEX ux_m5_adhoc_root ON m5_adhoc_workspace(root_canonical) WHERE state != 'deleted';
CREATE INDEX ix_m5_adhoc_run ON m5_adhoc_workspace(run_id);

CREATE TABLE m5_changeset (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES m5_adhoc_workspace(id) ON DELETE CASCADE,
    base_digest TEXT NOT NULL CHECK (length(base_digest) = 64 AND base_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('staged','applied','reverted','conflict')),
    source TEXT NOT NULL CHECK (length(source) BETWEEN 1 AND 128),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    applied_at TEXT,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_m5_changeset_run ON m5_changeset(run_id, created_at);

CREATE TABLE m5_changeset_item (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    changeset_id TEXT NOT NULL REFERENCES m5_changeset(id) ON DELETE CASCADE,
    path TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 512),
    change TEXT NOT NULL CHECK (change IN ('add','modify','delete')),
    patch_ref TEXT NOT NULL CHECK (length(patch_ref) = 64 AND patch_ref NOT GLOB '*[^0-9a-f]*'),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size INTEGER NOT NULL CHECK (size >= 0)
);
CREATE INDEX ix_m5_changeset_item_set ON m5_changeset_item(changeset_id);

CREATE TABLE m5_artifact (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    mime TEXT NOT NULL CHECK (length(mime) BETWEEN 1 AND 256),
    size INTEGER NOT NULL CHECK (size >= 0),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    generator TEXT NOT NULL CHECK (length(generator) BETWEEN 1 AND 128),
    download_state TEXT NOT NULL DEFAULT 'blocked' CHECK (download_state IN ('blocked','allowed','downloaded')),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_m5_artifact_run ON m5_artifact(run_id, created_at);

CREATE TABLE m5_workspace_conversion (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    source_workspace_id TEXT NOT NULL REFERENCES m5_adhoc_workspace(id),
    target_project_id TEXT NOT NULL REFERENCES projects(id),
    preview_digest TEXT NOT NULL CHECK (length(preview_digest) = 64 AND preview_digest NOT GLOB '*[^0-9a-f]*'),
    scope_json TEXT NOT NULL CHECK (json_valid(scope_json) AND length(scope_json) BETWEEN 2 AND 16384),
    phase TEXT NOT NULL DEFAULT 'preview' CHECK (phase IN ('preview','copying','publishing','committed','failed','abandoned')),
    committed INTEGER NOT NULL DEFAULT 0 CHECK (committed IN (0, 1)),
    committed_at TEXT,
    audit_event_id TEXT NOT NULL CHECK (length(audit_event_id) BETWEEN 1 AND 64),
    created_at TEXT NOT NULL,
    CHECK ((committed = 1) = (committed_at IS NOT NULL)),
    CHECK (committed = 0 OR phase = 'committed')
);
CREATE INDEX ix_m5_conversion_run ON m5_workspace_conversion(run_id, created_at);
CREATE INDEX ix_m5_conversion_source ON m5_workspace_conversion(source_workspace_id);

-- Backgrounding columns for the M5 foreground->background threshold (10s / 1MiB).
ALTER TABLE command_job ADD COLUMN backgrounded INTEGER NOT NULL DEFAULT 0 CHECK (backgrounded IN (0, 1));
ALTER TABLE command_job ADD COLUMN pid_token TEXT;
ALTER TABLE command_job ADD COLUMN log_cursor INTEGER NOT NULL DEFAULT 0 CHECK (log_cursor >= 0);
ALTER TABLE command_job ADD COLUMN cancel_deadline TEXT;

-- M5 wire operations: run.send, run.cancel, browser.act, mcp.invoke, workspace.convert.
DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0042_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0042_old;
DROP TABLE idempotency_records_0042_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

-- M5 audit actions. run.cancel reuses 'agent.run.cancelled' (same core service).
DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0042_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0042_old;
DROP TABLE audit_events_0042_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);
