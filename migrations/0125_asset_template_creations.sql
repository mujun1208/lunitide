CREATE TABLE asset_template_creations (
    request_key TEXT PRIMARY KEY CHECK (length(request_key) BETWEEN 1 AND 128),
    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536 AND json_valid(response_json)),
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX ix_asset_template_creations_expires ON asset_template_creations(expires_at);
