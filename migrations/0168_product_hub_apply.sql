-- Persist self-purify apply log and enrichment payload.
ALTER TABLE product_enrichments ADD COLUMN payload_json TEXT NOT NULL DEFAULT '{}';
CREATE TABLE product_apply_log (
  apply_id TEXT PRIMARY KEY CHECK (length(apply_id) BETWEEN 1 AND 64),
  error_code TEXT NOT NULL,
  stable_key TEXT NOT NULL,
  plan TEXT NOT NULL DEFAULT '',
  skill_id TEXT NOT NULL DEFAULT '',
  skill_name TEXT NOT NULL DEFAULT '',
  skill_output TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('planned','applied','failed','wont_fix')),
  created_at TEXT NOT NULL
);
