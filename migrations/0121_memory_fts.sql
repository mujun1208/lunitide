-- 0121 FTS5 over project memories for keyword search (F-02).
-- Shadow tables (memory_fts_*) and these triggers are skipped by the
-- sqlite_schema dump (same rule as 0101 message_fts). Do not add them to
-- expectedSchemaSQL.
-- Idempotent: Open() after a journal rewind (upgrade tests) re-applies this
-- file against a database that already has the virtual table.
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    memory_id UNINDEXED,
    project_id UNINDEXED,
    layer UNINDEXED,
    confidence UNINDEXED,
    key,
    content,
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS trg_memory_fts_ai AFTER INSERT ON memories BEGIN
  INSERT INTO memory_fts(memory_id, project_id, layer, confidence, key, content)
  VALUES(new.id, new.project_id, new.layer, new.confidence, new.key, new.content);
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fts_au AFTER UPDATE ON memories BEGIN
  DELETE FROM memory_fts WHERE memory_id = old.id;
  INSERT INTO memory_fts(memory_id, project_id, layer, confidence, key, content)
  VALUES(new.id, new.project_id, new.layer, new.confidence, new.key, new.content);
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fts_ad AFTER DELETE ON memories BEGIN
  DELETE FROM memory_fts WHERE memory_id = old.id;
END;

INSERT INTO memory_fts(memory_id, project_id, layer, confidence, key, content)
SELECT id, project_id, layer, confidence, key, content
FROM memories
WHERE (SELECT COUNT(*) FROM memory_fts) = 0;