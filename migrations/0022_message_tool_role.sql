-- P2 Tool Call Support: widen messages.role CHECK to include 'tool' role.
-- Tool messages carry tool/function results that follow an assistant tool_call.
-- foreign_keys is OFF during migration; integrity is verified after commit.

-- 1. Rebuild messages: role IN ('user','assistant','tool')
CREATE TABLE _messages_new (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'assistant', 'tool')),
    status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'failed')),
    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 9007199254740991),
    created_at TEXT NOT NULL,
    UNIQUE (session_id, sequence)
);
INSERT INTO _messages_new SELECT * FROM messages;

-- 2. Drop old table and rename (foreign_keys=OFF, no CASCADE triggered)
DROP TABLE messages;
ALTER TABLE _messages_new RENAME TO messages;

-- 3. Recreate index
CREATE INDEX ix_messages_session_sequence ON messages(session_id, sequence);
