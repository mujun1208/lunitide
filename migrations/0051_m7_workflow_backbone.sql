-- M7 slice 1 (T-7.1.2): nine-stage versioned workflow backbone.
-- Design deltas vs the M7 tech-design DDL (recorded in docs/evidence/m7-day0.txt):
--   - Timestamps are TEXT RFC3339 like every other lunitide table (the design
--     sketch used INTEGER); ULID ids and hex-digest CHECKs follow house style.
--   - dependency_keys/gate_policy are canonical JSON arrays/objects; the
--     workflow service re-validates the nine fixed keys and DAG on write.
--   - stage_input_snapshots is append-only by trigger (M7-EVD-001 semantics
--     arrive in 0052; the trigger name keeps the same code).

CREATE TABLE workflow_versions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft','published')),
    definition_digest TEXT NOT NULL CHECK (length(definition_digest) = 64 AND definition_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    published_at TEXT,
    UNIQUE (project_id, version)
);

CREATE TRIGGER trg_wv_published_readonly BEFORE UPDATE ON workflow_versions
    WHEN OLD.status = 'published' AND NEW.definition_digest <> OLD.definition_digest
    BEGIN SELECT RAISE(ABORT, 'M7-WF-001'); END;

CREATE TABLE workflow_instances (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_version_id TEXT NOT NULL REFERENCES workflow_versions(id),
    state TEXT NOT NULL CHECK (state IN ('running','completed','cancelled')),
    created_at TEXT NOT NULL,
    completed_at TEXT,
    CHECK (completed_at IS NULL OR completed_at >= created_at)
);
CREATE INDEX ix_wfi_project ON workflow_instances(project_id);

CREATE TABLE stage_definitions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    workflow_version_id TEXT NOT NULL REFERENCES workflow_versions(id) ON DELETE CASCADE,
    stage_key TEXT NOT NULL CHECK (stage_key IN ('INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION',
                                                  'SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE',
                                                  'VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY','OPERATIONS_RETROSPECTIVE')),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 1 AND 9),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    dependency_keys TEXT NOT NULL CHECK (json_valid(dependency_keys)),
    gate_policy TEXT NOT NULL CHECK (json_valid(gate_policy)),
    UNIQUE (workflow_version_id, stage_key),
    UNIQUE (workflow_version_id, ordinal)
);

CREATE TABLE stage_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_workflow_instance_id TEXT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,
    stage_definition_id TEXT NOT NULL REFERENCES stage_definitions(id),
    attempt_no INTEGER NOT NULL CHECK (attempt_no >= 1),
    state TEXT NOT NULL CHECK (state IN ('draft','ready','running','waiting_review','approved','completed',
                                          'blocked','paused','cancelled')),
    lock_version INTEGER NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    started_at TEXT,
    completed_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (project_workflow_instance_id, stage_definition_id, attempt_no)
);
-- At most one unterminated attempt per (instance, stage); cancelling closes
-- the attempt so a fresh one can start without overwriting history.
CREATE UNIQUE INDEX idx_sr_active_attempt ON stage_runs(project_workflow_instance_id, stage_definition_id)
    WHERE state NOT IN ('completed','cancelled');

CREATE TABLE stage_input_snapshots (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id) ON DELETE CASCADE,
    inputs_json TEXT NOT NULL CHECK (json_valid(inputs_json) AND length(inputs_json) BETWEEN 2 AND 1048576),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    captured_at TEXT NOT NULL
);
CREATE INDEX idx_sis_run ON stage_input_snapshots(stage_run_id, captured_at);

CREATE TABLE artifact_versions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_id TEXT NOT NULL CHECK (length(artifact_id) BETWEEN 1 AND 256),
    version_no INTEGER NOT NULL CHECK (version_no >= 1),
    kind TEXT NOT NULL CHECK (kind IN ('document','patch','test_report','scan_report','package','sbom','other')),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('project','stage_run','dev_task','release','m6_root')),
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
CREATE INDEX ix_art_scope ON artifact_versions(scope_type, scope_id);

CREATE TRIGGER trg_art_immutable_update BEFORE UPDATE ON artifact_versions
    BEGIN SELECT RAISE(ABORT, 'M7-ART-001'); END;

CREATE TRIGGER trg_art_immutable_delete BEFORE DELETE ON artifact_versions
    BEGIN SELECT RAISE(ABORT, 'M7-ART-001'); END;
