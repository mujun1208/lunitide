-- 0157: model profiles, target identity, and protocol ledger cipher (T02/T04).
CREATE TABLE model_profiles_v2 (
    profile_id TEXT PRIMARY KEY CHECK (length(profile_id) BETWEEN 1 AND 128),
    family TEXT NOT NULL CHECK (length(family) BETWEEN 1 AND 32),
    codec_version TEXT NOT NULL CHECK (length(codec_version) BETWEEN 1 AND 64),
    model_contract TEXT NOT NULL CHECK (length(model_contract) BETWEEN 1 AND 128),
    endpoint_purpose TEXT NOT NULL CHECK (endpoint_purpose IN ('standard','coding_plan','proxy','local')),
    body_json TEXT NOT NULL CHECK (json_valid(body_json) AND length(body_json)<=65536),
    profile_digest TEXT NOT NULL CHECK (length(profile_digest)=64)
);
CREATE TABLE model_targets_v2 (
    digest TEXT PRIMARY KEY CHECK (length(digest)=64),
    provider_id TEXT NOT NULL CHECK (length(provider_id) BETWEEN 1 AND 64),
    endpoint_url TEXT NOT NULL CHECK (length(endpoint_url) BETWEEN 1 AND 2048),
    purpose TEXT NOT NULL CHECK (purpose IN ('standard','coding_plan','proxy','local')),
    model_requested TEXT NOT NULL CHECK (length(model_requested) BETWEEN 1 AND 200),
    credential_binding_id TEXT NOT NULL CHECK (length(credential_binding_id) BETWEEN 1 AND 128)
);
CREATE TABLE protocol_epochs_v2 (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    target_json TEXT NOT NULL CHECK (json_valid(target_json) AND length(target_json)<=65536),
    target_digest TEXT NOT NULL CHECK (length(target_digest)=64),
    profile_json TEXT NOT NULL CHECK (json_valid(profile_json) AND length(profile_json)<=65536),
    profile_digest TEXT NOT NULL CHECK (length(profile_digest)=64),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    last_sequence INTEGER NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    state TEXT NOT NULL CHECK (state IN ('active','closed','degraded','legacy')),
    last_commit_id TEXT NOT NULL DEFAULT '' CHECK (length(last_commit_id) <= 64),
    observed_model TEXT,
    observed_revision TEXT,
    identity_integrity TEXT NOT NULL DEFAULT '' CHECK (length(identity_integrity) <= 64),
    observed_at TEXT NOT NULL DEFAULT '',
    cipher_bytes INTEGER NOT NULL DEFAULT 0 CHECK (cipher_bytes >= 0),
    prepared_bytes INTEGER NOT NULL DEFAULT 0 CHECK (prepared_bytes >= 0),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_protocol_epochs_v2_owner_session ON protocol_epochs_v2(owner_scope, session_id, created_at);
CREATE TABLE protocol_messages_v2 (
    id TEXT PRIMARY KEY CHECK (length(id)=26),
    epoch_id TEXT NOT NULL REFERENCES protocol_epochs_v2(id) ON DELETE CASCADE,
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    turn_id TEXT NOT NULL DEFAULT '' CHECK (length(turn_id) <= 64),
    call_id TEXT NOT NULL DEFAULT '' CHECK (length(call_id) <= 64),
    role TEXT NOT NULL CHECK (length(role) BETWEEN 1 AND 32),
    complete INTEGER NOT NULL CHECK (complete IN (0,1)),
    provenance TEXT NOT NULL CHECK (length(provenance) BETWEEN 1 AND 128),
    key_id TEXT NOT NULL CHECK (length(key_id) BETWEEN 1 AND 128),
    cipher_blob BLOB NOT NULL CHECK (length(cipher_blob) BETWEEN 1 AND 8388608),
    created_at TEXT NOT NULL,
    UNIQUE(epoch_id,sequence)
);
CREATE TABLE protocol_migration_progress (
    migration_id TEXT PRIMARY KEY CHECK (length(migration_id) BETWEEN 1 AND 64),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('message_group','protocol_private','checkpoint')),
    last_owner TEXT NOT NULL DEFAULT '' CHECK (length(last_owner) <= 128),
    last_source_key TEXT NOT NULL DEFAULT '' CHECK (length(last_source_key) <= 64),
    state TEXT NOT NULL CHECK (state IN ('running','completed')),
    updated_at TEXT NOT NULL
);
CREATE TABLE protocol_legacy_imports (
    source_kind TEXT NOT NULL CHECK (source_kind IN ('message_group','protocol_private','checkpoint')),
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    source_key TEXT NOT NULL CHECK (length(source_key) BETWEEN 1 AND 64),
    source_digest TEXT NOT NULL CHECK (length(source_digest)=64),
    epoch_id TEXT NOT NULL REFERENCES protocol_epochs_v2(id) ON DELETE CASCADE,
    message_refs_json TEXT NOT NULL CHECK (json_valid(message_refs_json) AND length(message_refs_json)<=65536),
    state TEXT NOT NULL CHECK (state IN ('imported','degraded')),
    PRIMARY KEY(source_kind, owner_scope, source_key)
);
