CREATE TABLE chat_turn_journal (
    turn_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    checkpoint_json TEXT NOT NULL,
    pending INTEGER NOT NULL CHECK (pending IN (0,1)),
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_chat_turn_journal_session ON chat_turn_journal(session_id, turn_id);
CREATE INDEX ix_chat_turn_journal_pending ON chat_turn_journal(session_id, pending, turn_id);
