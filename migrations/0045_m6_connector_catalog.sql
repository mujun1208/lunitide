-- M6 T-6.2.1: connector metadata snapshot storage. Read-only catalog /
-- schema / table / column / index / constraint metadata only; the adapter
-- layer enforces the statement + driver-method double allowlist (M6-DB-001)
-- and the metadata scope bound (M6-DB-002).

CREATE TABLE m6_connector_catalog (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    connector_id TEXT NOT NULL CHECK (length(connector_id) BETWEEN 1 AND 128),
    scope TEXT NOT NULL CHECK (scope IN ('personal','ad_hoc','project')),
    snapshot_version INTEGER NOT NULL CHECK (snapshot_version > 0),
    metadata_scope TEXT NOT NULL CHECK (metadata_scope IN ('catalog','schema','table','column','index','constraint')),
    objects_json TEXT NOT NULL CHECK (json_valid(objects_json) AND length(objects_json) BETWEEN 2 AND 16777216),
    fetched_at TEXT NOT NULL,
    UNIQUE (connector_id, snapshot_version)
);
CREATE INDEX ix_m6_connector_catalog_connector ON m6_connector_catalog(connector_id, snapshot_version DESC);
