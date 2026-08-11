-- Attachments: secure file ingestion, parsing, and context injection.
-- Each attachment is owned by a project (required) and optionally linked to
-- a session. The file content is stored in a controlled data directory
-- (file_ref); only metadata and parsed text live in SQLite.
-- Parse status is tracked independently so a single attachment failure does
-- not block the conversation (ADR-005 §7: attachment isolation).

CREATE TABLE attachments (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    file_ref TEXT NOT NULL CHECK (length(file_ref) BETWEEN 1 AND 512),
    original_name TEXT NOT NULL CHECK (length(original_name) BETWEEN 1 AND 256),
    mime TEXT NOT NULL DEFAULT 'application/octet-stream' CHECK (length(mime) BETWEEN 1 AND 128),
    size INTEGER NOT NULL CHECK (size BETWEEN 0 AND 10485760),
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
    parse_status TEXT NOT NULL DEFAULT 'pending' CHECK (parse_status IN ('pending', 'parsing', 'succeeded', 'failed')),
    parse_error_code TEXT NOT NULL DEFAULT '' CHECK (length(parse_error_code) <= 64),
    parsed_text TEXT NOT NULL DEFAULT '' CHECK (length(parsed_text) <= 1048576),
    parsed_text_bytes INTEGER NOT NULL DEFAULT 0 CHECK (parsed_text_bytes BETWEEN 0 AND 1048576),
    created_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX ix_attachments_project ON attachments(project_id, created_at DESC);
CREATE INDEX ix_attachments_session ON attachments(session_id, created_at DESC) WHERE session_id IS NOT NULL;
CREATE INDEX ix_attachments_sha256 ON attachments(sha256);
CREATE INDEX ix_attachments_parse_status ON attachments(parse_status) WHERE parse_status != 'succeeded';
