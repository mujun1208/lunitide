CREATE TABLE memories (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    layer TEXT NOT NULL CHECK (layer IN ('working', 'episodic', 'semantic', 'procedural')),
    scope TEXT NOT NULL CHECK (scope IN ('workspace', 'project', 'session')),
    key TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 256),
    content TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 65536),
    embedding_id TEXT,
    source_id TEXT,
    source_type TEXT CHECK (source_type IS NULL OR length(source_type) <= 64),
    confidence REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    access_count INTEGER NOT NULL DEFAULT 0 CHECK (access_count >= 0),
    last_accessed TEXT,
    expires_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX ix_memories_project_layer ON memories(project_id, layer);
CREATE INDEX ix_memories_key ON memories(project_id, key);