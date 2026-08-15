-- M6 design-doc closure (M6/02 §06 Legacy S5 contract, blueprint-fusion
-- routing/synthesis tables and the cloud execution engineering contract):
--
--   - Legacy S5 governance: OpenAPI consumer Integration / ApiOperation /
--     FieldMapping / CredentialRef plus the append-only HealthSample and
--     CallLog telemetry streams (M6-OAS/INT/MAP/CRD/HLT codes).
--   - Legacy S8 skill entity chain (skill / skill_version / skill_dependency /
--     skill_install / skill_trigger) and the governed import_candidate
--     pipeline (discovered -> ... -> approved | rejected | revoked).
--   - Full-conversation complexity routing (m6_complexity_decision) with the
--     frozen ChildContextManifest / ResultBundle / SynthesisRecord contract.
--   - Cloud execution registry: CloudRunner registration + attestation,
--     RegionPolicySnapshot, WorkerLease fencing epochs, RemoteReceipt with
--     outcome_unknown and ReconcileDecision (receipt loss never re-dispatches).
--   - m5_workspace_conversion gains the publish crash-recovery journal
--     (the copying/publishing walk records undo steps in publish_journal so
--     H2 crash rollback can replay them).
--
-- audit_events and idempotency_records are rebuilt (0047-0050 pattern) to
-- carry the matching actions and bridge operations.

-- ---------------------------------------------------------------------------
-- 1. m5_workspace_conversion: publish_journal column (rebuild; ALTER ADD
--    COLUMN would leave an uncontrolled amended DDL text in sqlite_schema).
-- ---------------------------------------------------------------------------

DROP INDEX ix_m5_conversion_run;
DROP INDEX ix_m5_conversion_source;
ALTER TABLE m5_workspace_conversion RENAME TO m5_workspace_conversion_0053_old;
CREATE TABLE m5_workspace_conversion (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    source_workspace_id TEXT NOT NULL REFERENCES m5_adhoc_workspace(id),
    target_project_id TEXT NOT NULL REFERENCES projects(id),
    preview_digest TEXT NOT NULL CHECK (length(preview_digest) = 64 AND preview_digest NOT GLOB '*[^0-9a-f]*'),
    scope_json TEXT NOT NULL CHECK (json_valid(scope_json) AND length(scope_json) BETWEEN 2 AND 16384),
    phase TEXT NOT NULL DEFAULT 'preview' CHECK (phase IN ('preview','copying','publishing','committed','failed','abandoned')),
    publish_journal TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(publish_journal) AND length(publish_journal) BETWEEN 2 AND 16384),
    committed INTEGER NOT NULL DEFAULT 0 CHECK (committed IN (0, 1)),
    committed_at TEXT,
    audit_event_id TEXT NOT NULL CHECK (length(audit_event_id) BETWEEN 1 AND 64),
    created_at TEXT NOT NULL,
    CHECK ((committed = 1) = (committed_at IS NOT NULL)),
    CHECK (committed = 0 OR phase = 'committed')
);
INSERT INTO m5_workspace_conversion (id, run_id, source_workspace_id, target_project_id, preview_digest, scope_json, phase, committed, committed_at, audit_event_id, created_at)
    SELECT id, run_id, source_workspace_id, target_project_id, preview_digest, scope_json, phase, committed, committed_at, audit_event_id, created_at FROM m5_workspace_conversion_0053_old;
DROP TABLE m5_workspace_conversion_0053_old;
CREATE INDEX ix_m5_conversion_run ON m5_workspace_conversion(run_id, created_at);
CREATE INDEX ix_m5_conversion_source ON m5_workspace_conversion(source_workspace_id);

-- ---------------------------------------------------------------------------
-- 2. audit_events rebuild: S5C / routing / synthesis / cloud / skill-import
--    / conversion-publish actions appended to the 0050 catalog.
-- ---------------------------------------------------------------------------

DROP INDEX ix_audit_aggregate_created;
ALTER TABLE audit_events RENAME TO audit_events_0053_old;
CREATE TABLE audit_events (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked')),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);
INSERT INTO audit_events SELECT * FROM audit_events_0053_old;
DROP TABLE audit_events_0053_old;
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- 3. idempotency_records rebuild: openapi.parse / complexity.decide /
--    skill.import.* bridge operations.
-- ---------------------------------------------------------------------------

DROP INDEX ix_idempotency_expires;
ALTER TABLE idempotency_records RENAME TO idempotency_records_0053_old;
CREATE TABLE idempotency_records (
    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle', 'delegation.create', 'delegation.settle', 'merge.submit', 'openapi.parse', 'complexity.decide', 'skill.import.discover', 'skill.import.inspect', 'skill.import.submit', 'skill.import.approve', 'skill.import.reject', 'skill.import.revoke')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);
INSERT INTO idempotency_records SELECT * FROM idempotency_records_0053_old;
DROP TABLE idempotency_records_0053_old;
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);

-- ---------------------------------------------------------------------------
-- 4. Legacy S5 governance entities.
-- ---------------------------------------------------------------------------

CREATE TABLE m6_credential_ref (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    provider TEXT NOT NULL CHECK (length(provider) BETWEEN 1 AND 128),
    secret_handle TEXT NOT NULL CHECK (length(secret_handle) BETWEEN 1 AND 256),
    scopes TEXT NOT NULL CHECK (json_valid(scopes) AND length(scopes) BETWEEN 2 AND 8192),
    expires_at TEXT,
    revoked_at TEXT,
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (provider, secret_handle)
);

CREATE TABLE m6_integration (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    kind TEXT NOT NULL CHECK (kind IN ('openapi', 'database')),
    base_url TEXT CHECK (base_url IS NULL OR length(base_url) BETWEEN 1 AND 2048),
    spec_digest TEXT NOT NULL CHECK (length(spec_digest) = 64 AND spec_digest NOT GLOB '*[^0-9a-f]*'),
    spec_version TEXT NOT NULL CHECK (length(spec_version) BETWEEN 1 AND 64),
    auth_type TEXT NOT NULL CHECK (auth_type IN ('none', 'apiKeyHeader', 'apiKeyQuery', 'bearerToken', 'basic', 'oauth2ClientCredentials')),
    credential_ref_id TEXT REFERENCES m6_credential_ref(id),
    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound', 'bidirectional')),
    role TEXT NOT NULL CHECK (role IN ('client', 'server')),
    environment_bindings TEXT NOT NULL CHECK (json_valid(environment_bindings) AND length(environment_bindings) BETWEEN 2 AND 16384),
    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'validating', 'active', 'paused', 'revoked', 'failed')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (name, spec_version)
);
CREATE INDEX ix_m6_integration_state ON m6_integration(state, name);

CREATE TABLE m6_api_operation (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    integration_id TEXT NOT NULL REFERENCES m6_integration(id) ON DELETE CASCADE,
    operation_id TEXT NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 256),
    method TEXT NOT NULL CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS')),
    path_template TEXT NOT NULL CHECK (length(path_template) BETWEEN 1 AND 1024),
    input_schema TEXT NOT NULL CHECK (json_valid(input_schema) AND length(input_schema) BETWEEN 2 AND 65536),
    output_schema TEXT NOT NULL CHECK (json_valid(output_schema) AND length(output_schema) BETWEEN 2 AND 65536),
    risk TEXT NOT NULL DEFAULT 'low' CHECK (risk IN ('low', 'medium', 'high')),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    pagination_spec TEXT CHECK (pagination_spec IS NULL OR (json_valid(pagination_spec) AND length(pagination_spec) BETWEEN 2 AND 16384)),
    retry_spec TEXT CHECK (retry_spec IS NULL OR (json_valid(retry_spec) AND length(retry_spec) BETWEEN 2 AND 16384)),
    idempotency_spec TEXT CHECK (idempotency_spec IS NULL OR (json_valid(idempotency_spec) AND length(idempotency_spec) BETWEEN 2 AND 16384)),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (integration_id, operation_id)
);

CREATE TABLE m6_field_mapping (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    operation_id TEXT NOT NULL REFERENCES m6_api_operation(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (length(source) BETWEEN 1 AND 512),
    target TEXT NOT NULL CHECK (length(target) BETWEEN 1 AND 512),
    direction TEXT NOT NULL CHECK (direction IN ('request', 'response')),
    required INTEGER NOT NULL DEFAULT 0 CHECK (required IN (0, 1)),
    transform_id TEXT CHECK (transform_id IS NULL OR length(transform_id) BETWEEN 1 AND 128),
    default_value TEXT CHECK (default_value IS NULL OR (json_valid(default_value) AND length(default_value) <= 4096)),
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    created_at TEXT NOT NULL,
    UNIQUE (operation_id, source, target, direction)
);

CREATE TABLE m6_health_sample (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    integration_id TEXT NOT NULL REFERENCES m6_integration(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('unknown', 'healthy', 'degraded', 'unhealthy', 'paused')),
    success INTEGER NOT NULL CHECK (success IN (0, 1)),
    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),
    code_class TEXT CHECK (code_class IS NULL OR code_class IN ('1xx', '2xx', '3xx', '4xx', '5xx')),
    sampled_at TEXT NOT NULL
);
CREATE INDEX ix_m6_health_sample_integration ON m6_health_sample(integration_id, sampled_at);

CREATE TRIGGER trg_m6_hs_ro_u BEFORE UPDATE ON m6_health_sample
    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END;
CREATE TRIGGER trg_m6_hs_ro_d BEFORE DELETE ON m6_health_sample
    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END;

CREATE TABLE m6_call_log (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    integration_id TEXT NOT NULL REFERENCES m6_integration(id),
    operation_id TEXT NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 256),
    trace_id TEXT CHECK (trace_id IS NULL OR length(trace_id) BETWEEN 1 AND 128),
    actor_id TEXT CHECK (actor_id IS NULL OR length(actor_id) BETWEEN 1 AND 128),
    subject_id TEXT CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),
    environment TEXT NOT NULL CHECK (environment IN ('development', 'test', 'production')),
    grant_id TEXT CHECK (grant_id IS NULL OR length(grant_id) BETWEEN 1 AND 256),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    started_at TEXT NOT NULL,
    completed_at TEXT,
    request_bytes INTEGER CHECK (request_bytes IS NULL OR request_bytes >= 0),
    response_bytes INTEGER CHECK (response_bytes IS NULL OR response_bytes >= 0),
    status_class TEXT CHECK (status_class IS NULL OR status_class IN ('1xx', '2xx', '3xx', '4xx', '5xx')),
    request_digest TEXT CHECK (request_digest IS NULL OR (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*')),
    response_digest TEXT CHECK (response_digest IS NULL OR (length(response_digest) = 64 AND response_digest NOT GLOB '*[^0-9a-f]*')),
    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'cancelled', 'outcome_unknown')),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),
    latency_ms INTEGER CHECK (latency_ms IS NULL OR latency_ms >= 0),
    cost_micros INTEGER CHECK (cost_micros IS NULL OR cost_micros >= 0),
    retry_of_call_id TEXT,
    correction_of_call_id TEXT,
    idempotency_key_digest TEXT CHECK (idempotency_key_digest IS NULL OR (length(idempotency_key_digest) = 64 AND idempotency_key_digest NOT GLOB '*[^0-9a-f]*')),
    policy_decision_id TEXT CHECK (policy_decision_id IS NULL OR length(policy_decision_id) BETWEEN 1 AND 256),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_m6_call_log_integration ON m6_call_log(integration_id, started_at);
CREATE INDEX ix_m6_call_log_operation ON m6_call_log(operation_id, started_at);

CREATE TRIGGER trg_m6_cl_ro_u BEFORE UPDATE ON m6_call_log
    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END;
CREATE TRIGGER trg_m6_cl_ro_d BEFORE DELETE ON m6_call_log
    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END;

-- ---------------------------------------------------------------------------
-- 5. Legacy S8 skill entity chain and the import candidate pipeline.
--    current_version_id is intentionally loose-coupled (no FK): skill rows
--    reference skill_version by id only, matching the m6app convention.
-- ---------------------------------------------------------------------------

CREATE TABLE m6_skill (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),
    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 256),
    status TEXT NOT NULL DEFAULT 'discovered' CHECK (status IN ('discovered', 'verified', 'installed', 'enabled', 'paused', 'quarantined', 'blocked', 'uninstalled')),
    current_version_id TEXT CHECK (current_version_id IS NULL OR length(current_version_id) = 26),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);

CREATE TABLE m6_skill_version (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_id TEXT NOT NULL REFERENCES m6_skill(id) ON DELETE CASCADE,
    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 64),
    manifest_ref TEXT NOT NULL CHECK (length(manifest_ref) BETWEEN 1 AND 512),
    package_hash TEXT NOT NULL CHECK (length(package_hash) = 64 AND package_hash NOT GLOB '*[^0-9a-f]*'),
    signature_status TEXT NOT NULL CHECK (signature_status IN ('verified', 'unverified', 'invalid')),
    permissions_json TEXT NOT NULL CHECK (json_valid(permissions_json) AND length(permissions_json) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL,
    UNIQUE (skill_id, semver)
);

CREATE TABLE m6_skill_dependency (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_version_id TEXT NOT NULL REFERENCES m6_skill_version(id) ON DELETE CASCADE,
    dependency_type TEXT NOT NULL CHECK (dependency_type IN ('skill', 'library', 'runtime')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),
    version_constraint TEXT NOT NULL CHECK (length(version_constraint) BETWEEN 1 AND 128),
    locked_digest TEXT CHECK (locked_digest IS NULL OR (length(locked_digest) = 64 AND locked_digest NOT GLOB '*[^0-9a-f]*')),
    created_at TEXT NOT NULL,
    UNIQUE (skill_version_id, dependency_type, name)
);

CREATE TABLE m6_skill_install (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_version_id TEXT NOT NULL REFERENCES m6_skill_version(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL CHECK (length(workspace_id) BETWEEN 1 AND 256),
    status TEXT NOT NULL CHECK (status IN ('installed', 'enabled', 'disabled', 'quarantined')),
    installed_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (skill_version_id, workspace_id)
);
CREATE INDEX ix_m6_skill_install_workspace ON m6_skill_install(workspace_id);

CREATE TABLE m6_skill_trigger (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 256),
    skill_version_id TEXT NOT NULL REFERENCES m6_skill_version(id) ON DELETE CASCADE,
    score REAL NOT NULL CHECK (score >= 0 AND score <= 1),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    status TEXT NOT NULL CHECK (status IN ('matched', 'executed', 'skipped', 'denied')),
    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_m6_skill_trigger_session ON m6_skill_trigger(session_id, created_at);

CREATE TABLE m6_import_candidate (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    asset_type TEXT NOT NULL CHECK (asset_type IN ('skill', 'profile', 'prompt_bundle')),
    source_url TEXT NOT NULL CHECK (length(source_url) BETWEEN 1 AND 2048),
    immutable_commit TEXT NOT NULL CHECK (length(immutable_commit) BETWEEN 1 AND 256),
    archive_hash TEXT NOT NULL CHECK (length(archive_hash) = 64 AND archive_hash NOT GLOB '*[^0-9a-f]*'),
    license TEXT NOT NULL CHECK (length(license) BETWEEN 1 AND 128),
    notice_ref TEXT CHECK (notice_ref IS NULL OR length(notice_ref) BETWEEN 1 AND 512),
    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 256),
    signature TEXT CHECK (signature IS NULL OR length(signature) <= 8192),
    source_attestation TEXT CHECK (source_attestation IS NULL OR (json_valid(source_attestation) AND length(source_attestation) <= 16384)),
    scan_refs TEXT CHECK (scan_refs IS NULL OR (json_valid(scan_refs) AND length(scan_refs) <= 16384)),
    injection_scan TEXT CHECK (injection_scan IS NULL OR (json_valid(injection_scan) AND length(injection_scan) <= 16384)),
    evaluation_id TEXT CHECK (evaluation_id IS NULL OR length(evaluation_id) BETWEEN 1 AND 256),
    approval TEXT CHECK (approval IS NULL OR (json_valid(approval) AND length(approval) <= 4096)),
    state TEXT NOT NULL DEFAULT 'discovered' CHECK (state IN ('discovered', 'pinned', 'inspected', 'scanned', 'evaluated', 'awaiting_approval', 'approved', 'rejected', 'revoked')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (source_url, immutable_commit)
);
CREATE INDEX ix_m6_import_candidate_state ON m6_import_candidate(state, created_at);

-- ---------------------------------------------------------------------------
-- 6. Complexity routing and the frozen delegation synthesis contract.
-- ---------------------------------------------------------------------------

CREATE TABLE m6_complexity_decision (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 256),
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),
    router_version TEXT NOT NULL CHECK (length(router_version) BETWEEN 1 AND 32),
    tier TEXT NOT NULL CHECK (tier IN ('simple', 'moderate', 'complex', 'high-risk')),
    routed_path TEXT NOT NULL CHECK (routed_path IN ('single', 'planned-single', 'delegated')),
    reason_codes TEXT NOT NULL CHECK (json_valid(reason_codes) AND length(reason_codes) BETWEEN 2 AND 8192),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    created_at TEXT NOT NULL,
    UNIQUE (input_digest, router_version)
);
CREATE INDEX ix_m6_complexity_decision_session ON m6_complexity_decision(session_id, created_at);

CREATE TABLE m6_child_manifest (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    delegation_id TEXT NOT NULL UNIQUE REFERENCES m6_delegation(id) ON DELETE CASCADE,
    manifest_digest TEXT NOT NULL UNIQUE CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    task_scope TEXT NOT NULL CHECK (json_valid(task_scope) AND length(task_scope) BETWEEN 2 AND 16384),
    locked_inputs TEXT NOT NULL CHECK (json_valid(locked_inputs) AND length(locked_inputs) BETWEEN 2 AND 65536),
    budget_json TEXT NOT NULL CHECK (json_valid(budget_json) AND length(budget_json) BETWEEN 2 AND 8192),
    capabilities TEXT NOT NULL CHECK (json_valid(capabilities) AND length(capabilities) BETWEEN 2 AND 8192),
    created_at TEXT NOT NULL
);

CREATE TABLE m6_result_bundle (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    delegation_id TEXT NOT NULL REFERENCES m6_delegation(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL CHECK (length(child_id) BETWEEN 1 AND 256),
    attempt INTEGER NOT NULL CHECK (attempt >= 1),
    base_head TEXT NOT NULL CHECK (length(base_head) BETWEEN 1 AND 256),
    claims TEXT NOT NULL CHECK (json_valid(claims) AND length(claims) BETWEEN 2 AND 65536),
    patch_digest TEXT CHECK (patch_digest IS NULL OR (length(patch_digest) = 64 AND patch_digest NOT GLOB '*[^0-9a-f]*')),
    test_evidence TEXT NOT NULL CHECK (json_valid(test_evidence) AND length(test_evidence) BETWEEN 2 AND 65536),
    usage TEXT NOT NULL CHECK (json_valid(usage) AND length(usage) BETWEEN 2 AND 8192),
    risk_notes TEXT CHECK (risk_notes IS NULL OR (json_valid(risk_notes) AND length(risk_notes) <= 16384)),
    result_digest TEXT NOT NULL UNIQUE CHECK (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    UNIQUE (delegation_id, attempt)
);

CREATE TABLE m6_synthesis_record (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    barrier_id TEXT REFERENCES m6_barrier(id),
    synthesis_digest TEXT NOT NULL UNIQUE CHECK (length(synthesis_digest) = 64 AND synthesis_digest NOT GLOB '*[^0-9a-f]*'),
    consistent TEXT NOT NULL CHECK (json_valid(consistent) AND length(consistent) BETWEEN 2 AND 65536),
    conflicts TEXT NOT NULL CHECK (json_valid(conflicts) AND length(conflicts) BETWEEN 2 AND 65536),
    missing_evidence TEXT NOT NULL CHECK (json_valid(missing_evidence) AND length(missing_evidence) BETWEEN 2 AND 65536),
    adoption_reasons TEXT NOT NULL CHECK (json_valid(adoption_reasons) AND length(adoption_reasons) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_m6_synthesis_record_root ON m6_synthesis_record(root_id, created_at);

-- ---------------------------------------------------------------------------
-- 7. Cloud execution registry (m6-cloud-execution-canonical): runner
--    registration + attestation, region policy snapshots, fenced worker
--    leases, remote receipts (outcome_unknown never re-dispatches) and the
--    reconcile decisions drawn from them.
-- ---------------------------------------------------------------------------

CREATE TABLE m6_region_policy (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    version INTEGER NOT NULL UNIQUE CHECK (version > 0),
    allowed_regions TEXT NOT NULL CHECK (json_valid(allowed_regions) AND length(allowed_regions) BETWEEN 2 AND 16384),
    egress_policy TEXT NOT NULL CHECK (json_valid(egress_policy) AND length(egress_policy) BETWEEN 2 AND 16384),
    data_classification TEXT NOT NULL CHECK (json_valid(data_classification) AND length(data_classification) BETWEEN 2 AND 16384),
    created_at TEXT NOT NULL
);

CREATE TABLE m6_cloudrunner (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    region TEXT NOT NULL CHECK (length(region) BETWEEN 1 AND 64),
    workload_identity TEXT NOT NULL UNIQUE CHECK (length(workload_identity) BETWEEN 1 AND 256),
    attestation_digest TEXT NOT NULL CHECK (length(attestation_digest) = 64 AND attestation_digest NOT GLOB '*[^0-9a-f]*'),
    attestation_status TEXT NOT NULL CHECK (attestation_status IN ('verified', 'unverified', 'revoked')),
    mtls_fingerprint TEXT NOT NULL CHECK (length(mtls_fingerprint) BETWEEN 1 AND 256),
    capabilities TEXT NOT NULL CHECK (json_valid(capabilities) AND length(capabilities) BETWEEN 2 AND 8192),
    state TEXT NOT NULL DEFAULT 'registered' CHECK (state IN ('registered', 'active', 'suspended', 'revoked')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_m6_cloudrunner_state ON m6_cloudrunner(state, region);

CREATE TABLE m6_worker_lease (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    runner_id TEXT NOT NULL REFERENCES m6_cloudrunner(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL UNIQUE REFERENCES m6_cloud_task(id) ON DELETE CASCADE,
    epoch INTEGER NOT NULL CHECK (epoch > 0),
    expires_at TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active', 'expired', 'released', 'revoked')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_m6_worker_lease_state ON m6_worker_lease(state, expires_at);

CREATE TABLE m6_remote_receipt (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    task_id TEXT NOT NULL REFERENCES m6_cloud_task(id) ON DELETE CASCADE,
    runner_id TEXT NOT NULL REFERENCES m6_cloudrunner(id),
    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'cancelled', 'outcome_unknown')),
    result_digest TEXT CHECK (result_digest IS NULL OR (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*')),
    usage TEXT NOT NULL CHECK (json_valid(usage) AND length(usage) BETWEEN 2 AND 8192),
    received_at TEXT NOT NULL,
    reconcile_state TEXT NOT NULL DEFAULT 'pending' CHECK (reconcile_state IN ('pending', 'reconciled', 'disputed')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (task_id, runner_id, received_at)
);
CREATE INDEX ix_m6_remote_receipt_pending ON m6_remote_receipt(reconcile_state, received_at);

CREATE TABLE m6_reconcile_decision (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    receipt_id TEXT NOT NULL REFERENCES m6_remote_receipt(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES m6_cloud_task(id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK (decision IN ('accepted', 'rejected', 'requeued', 'manual_review')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    decided_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_m6_reconcile_decision_task ON m6_reconcile_decision(task_id, decided_at);
