-- M10 wave3 T3-B: browser multi-mode runtime tables. br_settings is the
-- single-row configuration (5 connection modes, executable paths,
-- extension port, navigation allowlist, data retention window).
-- br_sessions tracks the CDP connection state machine
-- (disconnected→connecting→connected→error). br_data_usage keeps the
-- latest storage snapshot per session profile. br_permissions is the
-- permission approval queue (ask/allow/deny policies).
CREATE TABLE br_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    mode TEXT NOT NULL DEFAULT 'builtin' CHECK (mode IN ('builtin','chrome','edge','extension','ask')),
    chrome_path TEXT NOT NULL DEFAULT '' CHECK (length(chrome_path) <= 1024),
    edge_path TEXT NOT NULL DEFAULT '' CHECK (length(edge_path) <= 1024),
    extension_port INTEGER NOT NULL DEFAULT 9222 CHECK (extension_port BETWEEN 1024 AND 65535),
    allowlist_json TEXT NOT NULL DEFAULT '[]' CHECK (length(allowlist_json) <= 16384),
    data_retention_days INTEGER NOT NULL DEFAULT 30 CHECK (data_retention_days BETWEEN 0 AND 365),
    block_private_networks INTEGER NOT NULL DEFAULT 1 CHECK (block_private_networks IN (0,1)),
    updated_at TEXT NOT NULL
);

CREATE TABLE br_sessions (
    session_id TEXT PRIMARY KEY CHECK (length(session_id) BETWEEN 1 AND 64),
    mode TEXT NOT NULL CHECK (mode IN ('builtin','chrome','edge','extension','ask')),
    state TEXT NOT NULL CHECK (state IN ('disconnected','connecting','connected','error')),
    ws_url TEXT NOT NULL DEFAULT '' CHECK (length(ws_url) <= 512),
    detail TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 512),
    connected_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE br_data_usage (
    session_id TEXT PRIMARY KEY REFERENCES br_sessions(session_id),
    profile_bytes INTEGER NOT NULL DEFAULT 0 CHECK (profile_bytes >= 0),
    cache_bytes INTEGER NOT NULL DEFAULT 0 CHECK (cache_bytes >= 0),
    cookies_bytes INTEGER NOT NULL DEFAULT 0 CHECK (cookies_bytes >= 0),
    computed_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE br_permissions (
    permission_id TEXT PRIMARY KEY CHECK (length(permission_id) BETWEEN 1 AND 64),
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 512),
    permission TEXT NOT NULL CHECK (permission IN ('geolocation','camera','microphone','notifications','clipboard-read','downloads')),
    policy TEXT NOT NULL DEFAULT 'ask' CHECK (policy IN ('ask','allow','deny')),
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','granted','denied')),
    session_id TEXT NOT NULL DEFAULT '' CHECK (length(session_id) <= 64),
    decided_at TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_brperm_state_created ON br_permissions(state, created_at DESC);
