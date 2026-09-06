CREATE TABLE datasource_write_operations (
    id TEXT PRIMARY KEY,
    request_key TEXT NOT NULL UNIQUE,
    connection_id TEXT NOT NULL,
    connection_name TEXT NOT NULL,
    sql_text TEXT NOT NULL CHECK (length(sql_text) BETWEEN 1 AND 16384),
    digest TEXT NOT NULL CHECK (length(digest) = 64),
    target_digest TEXT NOT NULL CHECK (length(target_digest) = 64),
    state TEXT NOT NULL CHECK (state IN ('prepared','executing','completed','unknown')),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    result_json TEXT CHECK (result_json IS NULL OR json_valid(result_json))
);
CREATE INDEX ix_datasource_write_connection ON datasource_write_operations(connection_id, id);
