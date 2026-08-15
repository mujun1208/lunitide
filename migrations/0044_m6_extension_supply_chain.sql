-- M6 T-6.1.2/T-6.1.4: extension supply chain + HTTP MCP endpoint registry.
-- Per M6/02 DDL adapted to repo conventions (agent_run FK, TEXT RFC3339
-- timestamps, ULID and 64-hex digest CHECKs). Design deltas recorded in
-- docs/evidence/m6-day0.txt:
--   - "root_run" in the M6 design DDL is the agent_run table here.
--   - m6_mcp_endpoint lives beside the extension tables because both share
--     the M6 supply-chain lifecycle audit path; transport stdio rows are
--     allowed by the CHECK but the bridge layer refuses creation while the
--     stdio gate is DISABLED (M6-MCP-004).
--   - auth_ref is a SecretRef handle, never a credential value.

CREATE TABLE m6_extension_artifact (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 200),
    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 64),
    digest TEXT NOT NULL UNIQUE CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    signature_state TEXT NOT NULL CHECK (signature_state IN ('verified','failed','revoked')),
    sbom_ref TEXT NOT NULL CHECK (length(sbom_ref) BETWEEN 1 AND 512),
    manifest_json TEXT NOT NULL CHECK (json_valid(manifest_json) AND length(manifest_json) BETWEEN 2 AND 262144),
    risk TEXT NOT NULL CHECK (risk IN ('low','medium','high')),
    created_at TEXT NOT NULL,
    UNIQUE (publisher, name, version, digest)
);

CREATE TABLE m6_extension_install (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_id TEXT NOT NULL REFERENCES m6_extension_artifact(id) ON DELETE CASCADE,
    subject TEXT NOT NULL CHECK (length(subject) BETWEEN 1 AND 256),
    scope TEXT NOT NULL CHECK (scope IN ('personal','ad_hoc','project')),
    project_id TEXT,
    state TEXT NOT NULL CHECK (state IN ('discovered','verifying','installed','enabled','paused','blocked',
                                          'quarantined','uninstalled','rolling_back')),
    permission_grant TEXT NOT NULL CHECK (json_valid(permission_grant) AND length(permission_grant) BETWEEN 2 AND 65536),
    version INTEGER NOT NULL CHECK (version > 0),
    installed_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= installed_at)
);
CREATE INDEX ix_extinst_subject ON m6_extension_install(subject, scope);
CREATE INDEX ix_extinst_artifact ON m6_extension_install(artifact_id);

CREATE TABLE m6_mcp_endpoint (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    transport TEXT NOT NULL CHECK (transport IN ('https','stdio')),
    url TEXT NOT NULL CHECK (length(url) BETWEEN 8 AND 2048),
    auth_ref TEXT NOT NULL CHECK (length(auth_ref) BETWEEN 1 AND 256),
    capability_pin TEXT NOT NULL CHECK (json_valid(capability_pin) AND length(capability_pin) BETWEEN 2 AND 262144),
    state TEXT NOT NULL CHECK (state IN ('registered','probe','ready','degraded','revoked','disabled')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_m6_mcp_endpoint_state ON m6_mcp_endpoint(state);
