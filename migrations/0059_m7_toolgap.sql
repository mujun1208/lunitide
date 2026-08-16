-- 0059 M7 slice 7 (T-7.7.x): the tool-gap runtime registry.
--
-- db_connections registers external databases for db.query: credentials live
-- in Secret Lease references only (dsn_secret_ref, never plaintext) and a
-- connection is unusable until the read-only probe passes
-- (readonly_verified_at IS NULL -> M7-TOOL-004).
--
-- tool_manifest_v2 freezes the 23-tool manifest (16 imported read-only from
-- the M4 registry + the 7 gap tools). descriptor_version is bound by this
-- migration; any tool outside the manifest is refused registration
-- (scenario 44).
--
-- tool_call_quota tracks per-(run, tool) in-flight/call/byte budgets so
-- over-quota calls fail closed without amplifying limits (M7-TOOL-006
-- family, scenario 45). House adaptations as in 0051-0058: TEXT RFC3339
-- timestamps, ULID CHECKs, 64-hex digest CHECKs.

CREATE TABLE db_connections (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),
    kind TEXT NOT NULL CHECK (kind IN ('postgres','mysql')),
    dsn_secret_ref TEXT NOT NULL CHECK (length(dsn_secret_ref) BETWEEN 1 AND 256),
    readonly_verified_at TEXT,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (length(created_by) BETWEEN 1 AND 128)
);

CREATE INDEX ix_dbconn_kind ON db_connections(kind);

CREATE TABLE tool_manifest_v2 (
    tool_name TEXT PRIMARY KEY CHECK (length(tool_name) BETWEEN 1 AND 64),
    descriptor_version TEXT NOT NULL CHECK (length(descriptor_version) BETWEEN 1 AND 32),
    manifest_json TEXT NOT NULL CHECK (length(manifest_json) >= 2),
    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    io_semantics TEXT NOT NULL CHECK (io_semantics IN ('readonly','workspace_write')),
    timeout_ms INTEGER NOT NULL CHECK (timeout_ms >= 1),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
    imported_at TEXT NOT NULL
);

CREATE TABLE tool_call_quota (
    run_id TEXT NOT NULL CHECK (length(run_id) BETWEEN 1 AND 128),
    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 64),
    in_flight INTEGER NOT NULL DEFAULT 0 CHECK (in_flight >= 0),
    calls_total INTEGER NOT NULL DEFAULT 0 CHECK (calls_total >= 0),
    bytes_total INTEGER NOT NULL DEFAULT 0 CHECK (bytes_total >= 0),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, tool_name)
);
CREATE TABLE tool_results (
    run_id TEXT NOT NULL CHECK (length(run_id) BETWEEN 1 AND 128),
    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 64),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    result_json TEXT NOT NULL CHECK (length(result_json) >= 2),
    result_digest TEXT NOT NULL CHECK (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    PRIMARY KEY (run_id, idempotency_key)
);