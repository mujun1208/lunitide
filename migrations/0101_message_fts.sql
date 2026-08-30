-- 0101 Session message FTS5 index for Ctrl+K history search.
-- Shadow tables (message_fts_*) and these triggers are skipped by the
-- sqlite_schema dump; do not add them to expectedSchemaSQL.
-- Idempotent: Open() after a journal rewind (upgrade tests) re-applies this
-- file against a database that already has the virtual table.
CREATE VIRTUAL TABLE IF NOT EXISTS message_fts USING fts5(
    session_id UNINDEXED,
    message_id UNINDEXED,
    role UNINDEXED,
    sequence UNINDEXED,
    text,
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS trg_message_fts_ai AFTER INSERT ON message_parts BEGIN
  INSERT INTO message_fts(session_id, message_id, role, sequence, text)
  SELECT m.session_id, m.id, m.role, m.sequence, new.text
  FROM messages m WHERE m.id = new.message_id;
END;

CREATE TRIGGER IF NOT EXISTS trg_message_fts_ad AFTER DELETE ON message_parts BEGIN
  DELETE FROM message_fts WHERE message_id = old.message_id;
END;

CREATE TRIGGER IF NOT EXISTS trg_message_fts_am AFTER DELETE ON messages BEGIN
  DELETE FROM message_fts WHERE message_id = old.id;
END;

INSERT INTO message_fts(session_id, message_id, role, sequence, text)
SELECT m.session_id, m.id, m.role, m.sequence, p.text
FROM messages m
JOIN message_parts p ON p.message_id = m.id
WHERE (SELECT COUNT(*) FROM message_fts) = 0;
