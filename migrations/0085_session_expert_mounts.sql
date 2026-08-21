-- 0085: session-scoped expert collaboration pack (durable mounts).

CREATE TABLE session_expert_mounts (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    expert_id TEXT NOT NULL CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 0 AND 7),
    created_at TEXT NOT NULL,
    PRIMARY KEY (session_id, expert_id)
);
CREATE INDEX ix_session_expert_mounts_session ON session_expert_mounts(session_id, ordinal, expert_id);
