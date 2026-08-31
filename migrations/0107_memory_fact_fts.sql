-- 0107 FTS5 over confirmed memory candidates and compaction summaries.
-- Shadow tables and these triggers are skipped by the sqlite_schema dump
-- (same rule as 0101 message_fts). Do not add them to expectedSchemaSQL.
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fact_fts USING fts5(
    source_id UNINDEXED,
    kind UNINDEXED,
    body,
    tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS trg_memory_fact_fts_ai AFTER INSERT ON memory_candidates BEGIN
  INSERT INTO memory_fact_fts(source_id, kind, body)
  SELECT new.candidate_id, 'candidate', new.payload
  WHERE new.state = 'confirmed';
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fact_fts_au AFTER UPDATE OF payload, state ON memory_candidates BEGIN
  DELETE FROM memory_fact_fts WHERE source_id = old.candidate_id AND kind = 'candidate';
  INSERT INTO memory_fact_fts(source_id, kind, body)
  SELECT new.candidate_id, 'candidate', new.payload
  WHERE new.state = 'confirmed';
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fact_fts_ad AFTER DELETE ON memory_candidates BEGIN
  DELETE FROM memory_fact_fts WHERE source_id = old.candidate_id AND kind = 'candidate';
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fact_fts_sum_ai AFTER INSERT ON compaction_checkpoints BEGIN
  INSERT INTO memory_fact_fts(source_id, kind, body)
  SELECT new.id, 'summary', trim(new.human_summary || ' ' || new.summary_json)
  WHERE length(trim(new.human_summary || new.summary_json)) > 2;
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fact_fts_sum_au AFTER UPDATE OF human_summary, summary_json ON compaction_checkpoints BEGIN
  DELETE FROM memory_fact_fts WHERE source_id = old.id AND kind = 'summary';
  INSERT INTO memory_fact_fts(source_id, kind, body)
  SELECT new.id, 'summary', trim(new.human_summary || ' ' || new.summary_json)
  WHERE length(trim(new.human_summary || new.summary_json)) > 2;
END;

CREATE TRIGGER IF NOT EXISTS trg_memory_fact_fts_sum_ad AFTER DELETE ON compaction_checkpoints BEGIN
  DELETE FROM memory_fact_fts WHERE source_id = old.id AND kind = 'summary';
END;

INSERT INTO memory_fact_fts(source_id, kind, body)
SELECT candidate_id, 'candidate', payload
FROM memory_candidates
WHERE state = 'confirmed'
  AND (SELECT COUNT(*) FROM memory_fact_fts WHERE kind = 'candidate') = 0;

INSERT INTO memory_fact_fts(source_id, kind, body)
SELECT id, 'summary', trim(human_summary || ' ' || summary_json)
FROM compaction_checkpoints
WHERE length(trim(human_summary || summary_json)) > 2
  AND (SELECT COUNT(*) FROM memory_fact_fts WHERE kind = 'summary') = 0;
