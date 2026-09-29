-- Persist collected landscape evidence drafts (stage 2: competitor auto-collect).
-- A draft stays out of the report until a human confirms it; confirming keeps
-- the same row and flips the status so the report can quote it with its
-- source URL and collection date.
CREATE TABLE product_landscape_drafts (
  draft_id TEXT PRIMARY KEY CHECK (length(draft_id) BETWEEN 1 AND 64),
  name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
  axis TEXT NOT NULL CHECK (axis IN ('local','media','assets','hub')),
  quote TEXT NOT NULL CHECK (length(quote) BETWEEN 1 AND 600),
  url TEXT NOT NULL CHECK (length(url) BETWEEN 8 AND 512),
  date TEXT NOT NULL CHECK (length(date) = 10),
  status TEXT NOT NULL CHECK (status IN ('draft','confirmed')),
  created_at TEXT NOT NULL
);
