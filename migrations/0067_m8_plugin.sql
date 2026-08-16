-- 0067 M8 FR-18 (T-8.9.x): the unified plugin bundle runtime. Plugins are
-- not a second executor: after the verification chain passes, capabilities
-- hot-register into the EXISTING registries (mcp_endpoint_settings / M6
-- Registry / ToolRegistry / persona read-only directory) - no new registry
-- is created here.
--
-- PluginBundle is an append-only version chain: UNIQUE(plugin_id, semver),
-- package_hash is the full-package SHA-256; tamper/signature failure/
-- permission expansion moves the bundle to quarantined. PluginInstall is
-- per-subject: UNIQUE(subject_id, plugin_id), origin market|local|dev, the
-- safe path installed/enabled<->disabled->uninstalled, the security path
-- ->quarantined->uninstalled. PluginCapabilityBinding active->revoked: any
-- disable/upgrade/uninstall/drift revokes immediately, and every capability
-- call pre-checks an active binding (M8-040). Binding revocation commits in
-- the same transaction as the m6_outbox audit event.
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE plugin_bundles (
    bundle_id TEXT PRIMARY KEY CHECK (length(bundle_id) = 26 AND substr(bundle_id, 1, 1) GLOB '[0-7]' AND bundle_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    plugin_id TEXT NOT NULL CHECK (length(plugin_id) BETWEEN 1 AND 128),
    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 32),
    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 128),
    kind TEXT NOT NULL CHECK (kind IN ('mcp','skill','workflow','template','tool','agent-pack')),
    manifest_ref TEXT NOT NULL CHECK (length(manifest_ref) BETWEEN 1 AND 512),
    entrypoint TEXT NOT NULL CHECK (length(entrypoint) BETWEEN 1 AND 512),
    capabilities_json TEXT NOT NULL CHECK (length(capabilities_json) >= 2),
    permissions_json TEXT NOT NULL CHECK (length(permissions_json) >= 2),
    requires_json TEXT NOT NULL CHECK (length(requires_json) >= 2),
    package_hash TEXT NOT NULL UNIQUE CHECK (length(package_hash) = 64 AND package_hash NOT GLOB '*[^0-9a-f]*'),
    signature_status TEXT NOT NULL CHECK (signature_status IN ('verified','unverified','invalid')),
    state TEXT NOT NULL CHECK (state IN ('verified','quarantined')),
    created_at TEXT NOT NULL,
    UNIQUE (plugin_id, semver)
);

CREATE TABLE plugin_installs (
    install_id TEXT PRIMARY KEY CHECK (length(install_id) = 26 AND substr(install_id, 1, 1) GLOB '[0-7]' AND install_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    bundle_id TEXT NOT NULL REFERENCES plugin_bundles(bundle_id),
    plugin_id TEXT NOT NULL CHECK (length(plugin_id) BETWEEN 1 AND 128),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    origin TEXT NOT NULL CHECK (origin IN ('market','local','dev')),
    state TEXT NOT NULL CHECK (state IN ('installed','enabled','disabled','quarantined','uninstalled')),
    permission_grant_digest TEXT NOT NULL CHECK (length(permission_grant_digest) = 64 AND permission_grant_digest NOT GLOB '*[^0-9a-f]*'),
    installed_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (subject_id, plugin_id)
);

CREATE TABLE plugin_capability_bindings (
    binding_id TEXT PRIMARY KEY CHECK (length(binding_id) = 26 AND substr(binding_id, 1, 1) GLOB '[0-7]' AND binding_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    install_id TEXT NOT NULL REFERENCES plugin_installs(install_id),
    target_type TEXT NOT NULL CHECK (target_type IN ('mcp_endpoint','m6_registry','tool_registry','template','persona_directory')),
    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 128),
    capability_digest TEXT NOT NULL CHECK (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('active','revoked')),
    created_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE INDEX idx_pb_plugin ON plugin_bundles(plugin_id, state);
CREATE INDEX idx_pi_subject ON plugin_installs(subject_id, state);
CREATE INDEX idx_pcb_install ON plugin_capability_bindings(install_id, state);
