CREATE TABLE skills (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),
    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 32),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'deprecated', 'disabled')),
    permissions_json TEXT NOT NULL CHECK (length(permissions_json) BETWEEN 2 AND 2048),
    entry_point TEXT NOT NULL CHECK (length(entry_point) BETWEEN 1 AND 512),
    manifest_json TEXT NOT NULL CHECK (length(manifest_json) BETWEEN 2 AND 65536),
    signature TEXT CHECK (signature IS NULL OR length(signature) <= 1024),
    publisher_id TEXT,
    min_engine_version TEXT CHECK (min_engine_version IS NULL OR length(min_engine_version) <= 32),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX ux_skills_name_version ON skills(name, version);