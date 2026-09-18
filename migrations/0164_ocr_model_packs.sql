-- OCR pack state, gates, scoped runs, retained NOTICE, and leases.
-- Gate defaults stay closed without a verified runtime profile.

CREATE TABLE ocr_pack_state (
    pack_id TEXT PRIMARY KEY CHECK (length(pack_id) BETWEEN 1 AND 64),
    availability TEXT NOT NULL CHECK (availability IN ('not_installed','ready','quarantined')),
    current_version TEXT CHECK (current_version IS NULL OR length(current_version) BETWEEN 1 AND 64),
    previous_version TEXT CHECK (previous_version IS NULL OR length(previous_version) BETWEEN 1 AND 64),
    active_operation_id TEXT CHECK (active_operation_id IS NULL OR (length(active_operation_id) = 26 AND substr(active_operation_id, 1, 1) GLOB '[0-7]' AND active_operation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    last_health_at TEXT,
    last_error_code TEXT NOT NULL DEFAULT '' CHECK (length(last_error_code) <= 128),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    updated_at TEXT NOT NULL
);

CREATE TABLE ocr_pack_versions (
    pack_id TEXT NOT NULL CHECK (length(pack_id) BETWEEN 1 AND 64),
    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 64),
    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    install_root_ref TEXT NOT NULL CHECK (length(install_root_ref) BETWEEN 1 AND 512),
    engine_version TEXT NOT NULL DEFAULT '' CHECK (length(engine_version) <= 128),
    device_kind TEXT CHECK (device_kind IS NULL OR device_kind IN ('cpu','gpu')),
    state TEXT NOT NULL CHECK (state IN ('verified','quarantined')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (pack_id, version)
);

CREATE TABLE ocr_pack_operations (
    operation_id TEXT PRIMARY KEY CHECK (length(operation_id) = 26 AND substr(operation_id, 1, 1) GLOB '[0-7]' AND operation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    pack_id TEXT NOT NULL CHECK (length(pack_id) BETWEEN 1 AND 64),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    action TEXT NOT NULL CHECK (action IN ('install','uninstall')),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    catalog_revision TEXT NOT NULL DEFAULT '' CHECK (length(catalog_revision) <= 128),
    accepted_manifest_digest TEXT CHECK (accepted_manifest_digest IS NULL OR (length(accepted_manifest_digest) = 64 AND accepted_manifest_digest NOT GLOB '*[^0-9a-f]*')),
    phase TEXT NOT NULL CHECK (phase IN ('requested','preflighting','downloading','verifying','installing','self_testing','succeeded','failed','cancelled')),
    progress INTEGER NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
    cancel_requested INTEGER NOT NULL DEFAULT 0 CHECK (cancel_requested IN (0,1)),
    result_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(result_json) AND length(result_json) <= 65536),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 128),
    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),
    lease_until TEXT,
    heartbeat_at TEXT,
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    fence INTEGER NOT NULL DEFAULT 0 CHECK (fence >= 0),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (pack_id, idempotency_key),
    UNIQUE (pack_id, request_digest)
);

CREATE UNIQUE INDEX ux_ocr_pack_ops_active ON ocr_pack_operations(pack_id) WHERE phase NOT IN ('succeeded','failed','cancelled');

CREATE TABLE ocr_settings (
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    policy_json TEXT NOT NULL CHECK (json_valid(policy_json) AND length(policy_json) <= 65536),
    policy_revision TEXT NOT NULL CHECK (length(policy_revision) = 64 AND policy_revision NOT GLOB '*[^0-9a-f]*'),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    legacy_imported_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_subject_id, scope_kind, scope_id),
    CHECK ((scope_kind = 'user' AND scope_id = owner_subject_id) OR scope_kind = 'project')
);

CREATE TABLE ocr_pack_gates (
    pack_id TEXT PRIMARY KEY CHECK (length(pack_id) BETWEEN 1 AND 64),
    install_enabled INTEGER NOT NULL DEFAULT 0 CHECK (install_enabled IN (0,1)),
    auto_route_enabled INTEGER NOT NULL DEFAULT 0 CHECK (auto_route_enabled IN (0,1)),
    verified_runtime_profile_digest TEXT CHECK (verified_runtime_profile_digest IS NULL OR (length(verified_runtime_profile_digest) = 64 AND verified_runtime_profile_digest NOT GLOB '*[^0-9a-f]*')),
    disabled_reason TEXT NOT NULL DEFAULT 'NO_VERIFIED_RUNTIME_PROFILE' CHECK (length(disabled_reason) BETWEEN 1 AND 128),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    updated_at TEXT NOT NULL
);

CREATE TABLE ocr_legacy_registrations (
    registration_id TEXT PRIMARY KEY CHECK (length(registration_id) = 26 AND substr(registration_id, 1, 1) GLOB '[0-7]' AND registration_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    engine_id TEXT NOT NULL CHECK (engine_id = 'ppocr'),
    root_ref TEXT NOT NULL CHECK (length(root_ref) BETWEEN 1 AND 512),
    marker_detected INTEGER NOT NULL DEFAULT 0 CHECK (marker_detected IN (0,1)),
    state TEXT NOT NULL CHECK (state = 'registered_unwired'),
    available INTEGER NOT NULL DEFAULT 0 CHECK (available = 0),
    imported_at TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    UNIQUE (owner_subject_id, scope_kind, scope_id, engine_id),
    CHECK ((scope_kind = 'user' AND scope_id = owner_subject_id) OR scope_kind = 'project')
);

CREATE TABLE ocr_document_runs (
    run_id TEXT PRIMARY KEY CHECK (length(run_id) = 26 AND substr(run_id, 1, 1) GLOB '[0-7]' AND run_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    request_id TEXT NOT NULL UNIQUE CHECK (length(request_id) BETWEEN 1 AND 128),
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    document_digest TEXT NOT NULL CHECK (length(document_digest) = 64 AND document_digest NOT GLOB '*[^0-9a-f]*'),
    policy_json TEXT NOT NULL CHECK (json_valid(policy_json) AND length(policy_json) <= 65536),
    gate_json TEXT NOT NULL CHECK (json_valid(gate_json) AND length(gate_json) <= 65536),
    mode TEXT NOT NULL CHECK (length(mode) BETWEEN 1 AND 64),
    provider_id TEXT NOT NULL DEFAULT '' CHECK (length(provider_id) <= 128),
    model_id TEXT NOT NULL DEFAULT '' CHECK (length(model_id) <= 128),
    pack_manifest_digest TEXT CHECK (pack_manifest_digest IS NULL OR (length(pack_manifest_digest) = 64 AND pack_manifest_digest NOT GLOB '*[^0-9a-f]*')),
    runtime_profile_digest TEXT CHECK (runtime_profile_digest IS NULL OR (length(runtime_profile_digest) = 64 AND runtime_profile_digest NOT GLOB '*[^0-9a-f]*')),
    pipeline_kind TEXT NOT NULL DEFAULT '' CHECK (length(pipeline_kind) <= 64),
    allow_remote INTEGER NOT NULL DEFAULT 0 CHECK (allow_remote = 0),
    credential_ref TEXT NOT NULL DEFAULT '' CHECK (credential_ref = ''),
    request_snapshot_digest TEXT NOT NULL CHECK (length(request_snapshot_digest) = 64 AND request_snapshot_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('queued','running','succeeded','failed','cancelled')),
    page_count INTEGER NOT NULL DEFAULT 0 CHECK (page_count >= 0),
    actual_engine TEXT NOT NULL DEFAULT '' CHECK (length(actual_engine) <= 128),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (run_id, request_snapshot_digest),
    CHECK ((scope_kind = 'user' AND scope_id = owner_subject_id) OR scope_kind = 'project')
);

CREATE INDEX ix_ocr_document_runs_scope ON ocr_document_runs(owner_subject_id, scope_kind, scope_id, created_at);

CREATE TABLE ocr_page_results (
    run_id TEXT NOT NULL CHECK (length(run_id) = 26 AND substr(run_id, 1, 1) GLOB '[0-7]' AND run_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    page INTEGER NOT NULL CHECK (page >= 1),
    request_snapshot_digest TEXT NOT NULL CHECK (length(request_snapshot_digest) = 64 AND request_snapshot_digest NOT GLOB '*[^0-9a-f]*'),
    actual_engine TEXT NOT NULL CHECK (length(actual_engine) BETWEEN 1 AND 128),
    engine_version TEXT NOT NULL DEFAULT '' CHECK (length(engine_version) <= 128),
    pack_version TEXT NOT NULL DEFAULT '' CHECK (length(pack_version) <= 64),
    pipeline_kind TEXT NOT NULL DEFAULT '' CHECK (length(pipeline_kind) <= 64),
    source_digest TEXT NOT NULL CHECK (length(source_digest) = 64 AND source_digest NOT GLOB '*[^0-9a-f]*'),
    complete INTEGER NOT NULL CHECK (complete IN (0,1)),
    uncertain INTEGER NOT NULL CHECK (uncertain IN (0,1)),
    layout_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(layout_json) AND length(layout_json) <= 65536),
    text_ref TEXT NOT NULL DEFAULT '' CHECK (length(text_ref) <= 512),
    warnings_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(warnings_json) AND length(warnings_json) <= 8192),
    PRIMARY KEY (run_id, page),
    FOREIGN KEY (run_id, request_snapshot_digest) REFERENCES ocr_document_runs(run_id, request_snapshot_digest)
);

CREATE TABLE ocr_artifacts (
    artifact_id TEXT PRIMARY KEY CHECK (length(artifact_id) = 26 AND substr(artifact_id, 1, 1) GLOB '[0-7]' AND artifact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_subject_id TEXT NOT NULL CHECK (length(owner_subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','project')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    run_id TEXT NOT NULL CHECK (length(run_id) = 26 AND substr(run_id, 1, 1) GLOB '[0-7]' AND run_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    cas_ref TEXT NOT NULL CHECK (length(cas_ref) BETWEEN 1 AND 512),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    size INTEGER NOT NULL CHECK (size >= 0),
    mime TEXT NOT NULL CHECK (length(mime) BETWEEN 1 AND 128),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    CHECK ((scope_kind = 'user' AND scope_id = owner_subject_id) OR scope_kind = 'project')
);

CREATE TABLE ocr_version_leases (
    lease_id TEXT PRIMARY KEY CHECK (length(lease_id) = 26 AND substr(lease_id, 1, 1) GLOB '[0-7]' AND lease_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    pack_id TEXT NOT NULL CHECK (length(pack_id) BETWEEN 1 AND 64),
    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 64),
    run_id TEXT CHECK (run_id IS NULL OR (length(run_id) = 26 AND substr(run_id, 1, 1) GLOB '[0-7]' AND run_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),
    fence INTEGER NOT NULL DEFAULT 0 CHECK (fence >= 0),
    expires_at TEXT NOT NULL
);

CREATE TABLE ocr_artifact_leases (
    lease_id TEXT PRIMARY KEY CHECK (length(lease_id) = 26 AND substr(lease_id, 1, 1) GLOB '[0-7]' AND lease_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_id TEXT NOT NULL REFERENCES ocr_artifacts(artifact_id),
    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),
    expires_at TEXT NOT NULL
);

CREATE TABLE ocr_pack_notices (
    pack_id TEXT NOT NULL CHECK (length(pack_id) BETWEEN 1 AND 64),
    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 64),
    notice_digest TEXT NOT NULL CHECK (length(notice_digest) = 64 AND notice_digest NOT GLOB '*[^0-9a-f]*'),
    notice_bytes BLOB NOT NULL,
    notice_ref TEXT NOT NULL CHECK (length(notice_ref) BETWEEN 1 AND 512),
    verified_at TEXT NOT NULL,
    installed_at TEXT,
    uninstalled_at TEXT,
    PRIMARY KEY (pack_id, manifest_digest)
);

CREATE INDEX ix_ocr_pack_notices_verified ON ocr_pack_notices(verified_at DESC, manifest_digest ASC);

INSERT INTO ocr_pack_gates(pack_id, install_enabled, auto_route_enabled, verified_runtime_profile_digest, disabled_reason, revision, updated_at)
VALUES ('paddleocr-vl-1.6', 0, 0, NULL, 'NO_VERIFIED_RUNTIME_PROFILE', 1, '1970-01-01T00:00:00Z');

INSERT INTO ocr_pack_state(pack_id, availability, current_version, previous_version, active_operation_id, last_health_at, last_error_code, revision, updated_at)
VALUES ('paddleocr-vl-1.6', 'not_installed', NULL, NULL, NULL, NULL, '', 1, '1970-01-01T00:00:00Z');
