-- 0114 Expert knowledge foundation: FTS over chunk body, growth paths,
-- unique collection scope. FTS objects are skipped by expectedSchemaSQL
-- (same rule as 0107).
CREATE UNIQUE INDEX IF NOT EXISTS ux_kb_collections_scope
    ON kb_collections(scope_id);

CREATE TABLE expert_growth_paths (
    expert_id TEXT PRIMARY KEY CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    mission_snapshot TEXT NOT NULL CHECK (length(mission_snapshot) BETWEEN 1 AND 4096),
    ladder_json TEXT NOT NULL CHECK (length(ladder_json) >= 2),
    coverage_json TEXT NOT NULL CHECK (length(coverage_json) >= 2),
    updated_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS kb_chunk_fts USING fts5(
    chunk_id UNINDEXED,
    body,
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS trg_kb_chunk_fts_ai AFTER INSERT ON kb_chunks BEGIN
  INSERT INTO kb_chunk_fts(chunk_id, body)
  SELECT new.chunk_id, new.body WHERE length(new.body) > 0;
END;

CREATE TRIGGER IF NOT EXISTS trg_kb_chunk_fts_au AFTER UPDATE OF body ON kb_chunks BEGIN
  DELETE FROM kb_chunk_fts WHERE chunk_id = old.chunk_id;
  INSERT INTO kb_chunk_fts(chunk_id, body)
  SELECT new.chunk_id, new.body WHERE length(new.body) > 0;
END;

CREATE TRIGGER IF NOT EXISTS trg_kb_chunk_fts_ad AFTER DELETE ON kb_chunks BEGIN
  DELETE FROM kb_chunk_fts WHERE chunk_id = old.chunk_id;
END;
