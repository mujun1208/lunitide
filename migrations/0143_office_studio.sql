-- Extend shared immutable artifacts without renaming the referenced table.
-- initialize runs migrations with foreign_keys OFF inside one exclusive
-- transaction and validates every FK before reenabling enforcement.
CREATE TEMP TABLE office_artifact_backup AS SELECT * FROM artifact_versions;
DROP TRIGGER trg_art_immutable_update;
DROP TRIGGER trg_art_immutable_delete;
DROP TABLE artifact_versions;
CREATE TABLE artifact_versions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_id TEXT NOT NULL CHECK (length(artifact_id) BETWEEN 1 AND 256),
    version_no INTEGER NOT NULL CHECK (version_no >= 1),
    kind TEXT NOT NULL CHECK (kind IN ('document','patch','test_report','scan_report','package','sbom','other')),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('project','stage_run','dev_task','release','m6_root','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 256),
    content_ref TEXT NOT NULL CHECK (length(content_ref) BETWEEN 1 AND 1024),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size INTEGER NOT NULL CHECK (size >= 0),
    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 3 AND 256),
    state TEXT NOT NULL CHECK (state IN ('active','superseded')),
    created_by TEXT NOT NULL CHECK (length(created_by) BETWEEN 1 AND 128),
    created_at TEXT NOT NULL,
    UNIQUE (artifact_id, version_no)
);
INSERT INTO artifact_versions SELECT * FROM office_artifact_backup;
DROP TABLE office_artifact_backup;
CREATE INDEX ix_art_scope ON artifact_versions(scope_type, scope_id);
CREATE TRIGGER trg_art_immutable_update BEFORE UPDATE ON artifact_versions
    BEGIN SELECT RAISE(ABORT, 'M7-ART-001'); END;
CREATE TRIGGER trg_art_immutable_delete BEFORE DELETE ON artifact_versions
    BEGIN SELECT RAISE(ABORT, 'M7-ART-001'); END;

-- session_id is retained as provenance even if the chat is later deleted.
-- The create path checks session existence and all reads remain owner-scoped.
CREATE TABLE office_tasks (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    session_id TEXT NOT NULL CHECK (length(session_id)=26),
    owner_org_id TEXT NOT NULL DEFAULT '' CHECK (owner_org_id='' OR length(owner_org_id)=26),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    goal TEXT NOT NULL DEFAULT '' CHECK (length(goal)<=16000),
    status TEXT NOT NULL CHECK (status IN ('draft','queued','planning','running','validating','succeeded','waiting_input','waiting_approval','cancelling','cancelled','failed','interrupted')),
    revision INTEGER NOT NULL CHECK (revision>=1),
    run_id TEXT NOT NULL DEFAULT '' CHECK (run_id='' OR length(run_id)=26),
    start_message_id TEXT NOT NULL DEFAULT '' CHECK (start_message_id='' OR length(start_message_id)=26),
    checkpoint_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(checkpoint_json) AND length(checkpoint_json)<=1048576),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest)=64),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(session_id,idempotency_key)
);
CREATE INDEX ix_office_tasks_session ON office_tasks(session_id,updated_at DESC,id);
CREATE TRIGGER trg_office_task_scope BEFORE UPDATE OF session_id ON office_tasks
    WHEN NEW.session_id<>OLD.session_id BEGIN SELECT RAISE(ABORT,'OFFICE_SCOPE_MISMATCH'); END;
CREATE TRIGGER trg_office_task_owner BEFORE UPDATE OF owner_org_id ON office_tasks
    WHEN NEW.owner_org_id<>OLD.owner_org_id BEGIN SELECT RAISE(ABORT,'OFFICE_SCOPE_MISMATCH'); END;

CREATE TABLE office_task_artifacts (
    artifact_id TEXT PRIMARY KEY CHECK (length(artifact_id) BETWEEN 1 AND 256),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    role TEXT NOT NULL CHECK (role IN ('input','output','template')),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_office_artifacts_task ON office_task_artifacts(task_id,artifact_id);
CREATE TABLE office_version_metadata (
    version_id TEXT PRIMARY KEY REFERENCES artifact_versions(id),
    kind TEXT NOT NULL CHECK (kind IN ('pptx','docx','xlsx','pdf')),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),
    content_mode TEXT NOT NULL CHECK (content_mode IN ('managed','imported')),
    base_version_id TEXT REFERENCES artifact_versions(id),
    spec_json TEXT NOT NULL CHECK (json_valid(spec_json) AND length(spec_json)<=4194304),
    index_json TEXT NOT NULL CHECK (json_valid(index_json) AND length(index_json)<=4194304),
    quality TEXT NOT NULL CHECK (quality IN ('unverified','checking','partial','passed','blocked','stale')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest)=64)
);
CREATE TRIGGER trg_office_metadata_immutable BEFORE UPDATE ON office_version_metadata
    WHEN NEW.version_id<>OLD.version_id OR NEW.kind<>OLD.kind OR NEW.name<>OLD.name OR NEW.content_mode<>OLD.content_mode OR NEW.base_version_id IS NOT OLD.base_version_id OR NEW.spec_json<>OLD.spec_json OR NEW.index_json<>OLD.index_json OR NEW.idempotency_key<>OLD.idempotency_key OR NEW.request_digest<>OLD.request_digest
    BEGIN SELECT RAISE(ABORT,'OFFICE_VERSION_IMMUTABLE'); END;
CREATE TRIGGER trg_office_metadata_delete BEFORE DELETE ON office_version_metadata
    BEGIN SELECT RAISE(ABORT,'OFFICE_VERSION_IMMUTABLE'); END;
CREATE TABLE office_artifact_heads (
    artifact_id TEXT PRIMARY KEY REFERENCES office_task_artifacts(artifact_id),
    latest_version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    accepted_version_id TEXT REFERENCES artifact_versions(id),
    revision INTEGER NOT NULL CHECK (revision>=1)
);

CREATE TABLE office_validation_runs (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    sha256 TEXT NOT NULL CHECK (length(sha256)=64),
    validator TEXT NOT NULL CHECK (length(validator) BETWEEN 1 AND 128),
    quality TEXT NOT NULL CHECK (quality IN ('unverified','checking','partial','passed','blocked')),
    checks_json TEXT NOT NULL CHECK (json_valid(checks_json) AND length(checks_json)<=1048576),
    evidence_json TEXT NOT NULL CHECK (json_valid(evidence_json) AND length(evidence_json)<=1048576),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_office_validation_version ON office_validation_runs(version_id,created_at DESC,id DESC);
CREATE TRIGGER trg_office_validation_update BEFORE UPDATE ON office_validation_runs
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_validation_delete BEFORE DELETE ON office_validation_runs
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;

CREATE TABLE office_step_receipts (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    run_id TEXT NOT NULL CHECK (length(run_id)=26),
    step_key TEXT NOT NULL CHECK (length(step_key) BETWEEN 1 AND 128),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    input_digest TEXT NOT NULL CHECK (length(input_digest)=64),
    state TEXT NOT NULL CHECK (state IN ('started','succeeded','failed','cancelled','unknown')),
    result_json TEXT NOT NULL CHECK (json_valid(result_json) AND length(result_json)<=1048576),
    created_at TEXT NOT NULL,
    UNIQUE(task_id,run_id,step_key,idempotency_key,state)
);
CREATE TRIGGER trg_office_step_update BEFORE UPDATE ON office_step_receipts
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_step_delete BEFORE DELETE ON office_step_receipts
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;

CREATE TABLE office_evidence_edges (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    source_version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    target_version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    source_node TEXT NOT NULL CHECK (length(source_node)<=512),
    target_node TEXT NOT NULL CHECK (length(target_node)<=512),
    metric_json TEXT NOT NULL CHECK (json_valid(metric_json) AND length(metric_json)<=65536),
    created_at TEXT NOT NULL,
    CHECK(source_version_id<>target_version_id),
    UNIQUE(source_version_id,target_version_id,source_node,target_node)
);
CREATE INDEX ix_office_evidence_source ON office_evidence_edges(source_version_id,target_version_id);
CREATE TRIGGER trg_office_edge_update BEFORE UPDATE ON office_evidence_edges
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_office_edge_delete BEFORE DELETE ON office_evidence_edges
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;

CREATE TABLE artifact_lifecycle_events (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    task_id TEXT NOT NULL REFERENCES office_tasks(id),
    version_id TEXT REFERENCES artifact_versions(id),
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 128),
    detail_json TEXT NOT NULL CHECK (json_valid(detail_json) AND length(detail_json)<=1048576),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_artifact_lifecycle_task ON artifact_lifecycle_events(task_id,created_at DESC,id DESC);
CREATE TRIGGER trg_artifact_lifecycle_update BEFORE UPDATE ON artifact_lifecycle_events
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
CREATE TRIGGER trg_artifact_lifecycle_delete BEFORE DELETE ON artifact_lifecycle_events
    BEGIN SELECT RAISE(ABORT,'OFFICE_EVIDENCE_IMMUTABLE'); END;
