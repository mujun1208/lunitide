-- M10 wave3 T3-A: MCP-market confirm tokens and per-endpoint usage stats.
-- mc_confirm_tokens backs the anti-bypass confirmation flow: lifecycle
-- operations (install/uninstall/update) must present the raw token whose
-- SHA-256 hash is stored here, bound to method+target+digest, single-use
-- (consumed_at) and short-lived (expires_at). mc_endpoint_usage aggregates
-- lifecycle usage per settings-plane endpoint; rows are never deleted, so
-- statistics survive uninstalls.
CREATE TABLE mc_confirm_tokens (
    token_hash TEXT PRIMARY KEY CHECK (length(token_hash) = 64 AND token_hash NOT GLOB '*[^0-9a-f]*'),
    method TEXT NOT NULL CHECK (method IN ('mc.connector.install', 'mc.connector.uninstall', 'mc.connector.update')),
    target TEXT NOT NULL CHECK (length(target) BETWEEN 1 AND 256),
    digest TEXT NOT NULL DEFAULT '' CHECK (digest = '' OR (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*')),
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT
);
CREATE INDEX ix_mct_expires ON mc_confirm_tokens(expires_at);

CREATE TABLE mc_endpoint_usage (
    endpoint_id TEXT PRIMARY KEY REFERENCES mcp_endpoint_settings(endpoint_id),
    installs INTEGER NOT NULL DEFAULT 0 CHECK (installs >= 0),
    updates INTEGER NOT NULL DEFAULT 0 CHECK (updates >= 0),
    uninstalls INTEGER NOT NULL DEFAULT 0 CHECK (uninstalls >= 0),
    last_used_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
