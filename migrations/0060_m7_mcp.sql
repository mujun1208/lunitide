-- 0060 M7 slice 8 (T-7.8.x): the MCP client settings plane.
--
-- mcp_endpoint_settings is the canonical management surface for MCP
-- endpoints (FR-25): transport stdio|https, origin market|manual,
-- source_trust signed|verified|unknown and the five-state machine
-- probe/ready/degraded/revoked/quarantined. source_trust='unknown' forces
-- quarantined until the user explicitly confirms (M7-MCP-002); capability
-- drift quarantines fail-closed (M7-MCP-003). M6 mcp6 endpoint storage and
-- capability pins stay authoritative for invoke; this table only governs the
-- settings plane.
--
-- mcp_market_items is a read-only catalog cache: UPDATE trips M7-EVD-001
-- (per design DDL); refresh inserts new rows, entries never mutate in place.
-- The catalog never carries credentials. House adaptations as in
-- 0051-0059: TEXT RFC3339 timestamps, ULID CHECKs, 64-hex digest CHECKs.

CREATE TABLE mcp_endpoint_settings (
    endpoint_id TEXT PRIMARY KEY CHECK (length(endpoint_id) BETWEEN 1 AND 128),
    transport TEXT NOT NULL CHECK (transport IN ('stdio','https')),
    command TEXT CHECK (command IS NULL OR length(command) BETWEEN 1 AND 512),
    args_json TEXT CHECK (args_json IS NULL OR length(args_json) >= 2),
    url TEXT CHECK (url IS NULL OR (length(url) BETWEEN 8 AND 2048 AND url LIKE 'https://%')),
    origin TEXT NOT NULL CHECK (origin IN ('market','manual')),
    source_trust TEXT NOT NULL CHECK (source_trust IN ('signed','verified','unknown')),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    state TEXT NOT NULL DEFAULT 'probe' CHECK (state IN ('probe','ready','degraded','revoked','quarantined')),
    capability_digest TEXT CHECK (capability_digest IS NULL OR (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*')),
    pinned_digest TEXT CHECK (pinned_digest IS NULL OR (length(pinned_digest) = 64 AND pinned_digest NOT GLOB '*[^0-9a-f]*')),
    last_health_at TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX ix_mcpes_state ON mcp_endpoint_settings(state);

CREATE TABLE mcp_market_items (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),
    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 128),
    description TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 2000),
    transport_hint TEXT NOT NULL CHECK (transport_hint IN ('stdio','https')),
    install_config_json TEXT NOT NULL CHECK (length(install_config_json) >= 2),
    catalog_digest TEXT NOT NULL CHECK (length(catalog_digest) = 64 AND catalog_digest NOT GLOB '*[^0-9a-f]*'),
    signature TEXT NOT NULL CHECK (length(signature) BETWEEN 1 AND 512),
    fetched_at TEXT NOT NULL
);

-- Catalog cache rows are read-only (design DDL: UPDATE -> M7-EVD-001).
CREATE TRIGGER trg_mmi_readonly BEFORE UPDATE ON mcp_market_items
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;