CREATE TABLE chat_turn_checkpoint_parts (
    turn_id TEXT NOT NULL REFERENCES chat_turn_journal(turn_id) ON DELETE CASCADE,
    part INTEGER NOT NULL CHECK (part >= 0),
    content BLOB NOT NULL CHECK (length(content) BETWEEN 1 AND 262144),
    PRIMARY KEY (turn_id, part)
);
