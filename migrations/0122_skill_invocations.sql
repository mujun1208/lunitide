CREATE TABLE skill_invocations (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_id TEXT NOT NULL CHECK (length(skill_id) = 26 AND substr(skill_id, 1, 1) GLOB '[0-7]' AND skill_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    skill_version TEXT NOT NULL CHECK (length(skill_version) BETWEEN 1 AND 32),
    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 128),
    input TEXT NOT NULL CHECK (length(input) <= 65536),
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),
    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    risk TEXT NOT NULL CHECK (length(risk) BETWEEN 1 AND 32),
    mode TEXT NOT NULL DEFAULT '' CHECK (length(mode) <= 32),
    requires_approval INTEGER NOT NULL DEFAULT 0 CHECK (requires_approval IN (0, 1)),
    consumed INTEGER NOT NULL DEFAULT 0 CHECK (consumed IN (0, 1)),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX ix_skill_invocations_session ON skill_invocations(session_id);
CREATE INDEX ix_skill_invocations_expires ON skill_invocations(expires_at);