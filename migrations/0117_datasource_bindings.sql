-- 0117 datasource center: connection state + owner bindings.
ALTER TABLE db_connections ADD COLUMN state TEXT NOT NULL DEFAULT 'active'
    CHECK (state IN ('active','disabled'));
CREATE TABLE datasource_bindings (
    binding_id TEXT PRIMARY KEY CHECK (length(binding_id) = 26 AND substr(binding_id, 1, 1) GLOB '[0-7]' AND binding_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    owner_type TEXT NOT NULL CHECK (owner_type IN ('expert','mro')),
    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 64),
    connection_id TEXT NOT NULL REFERENCES db_connections(id),
    purpose TEXT NOT NULL CHECK (purpose IN ('stock','workorder','generic')),
    table_map_json TEXT NOT NULL CHECK (length(table_map_json) >= 2),
    created_at TEXT NOT NULL,
    UNIQUE (owner_type, owner_id, purpose)
);
