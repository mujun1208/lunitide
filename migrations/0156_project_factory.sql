-- 0156: project factory (rules + sqlite db status).

ALTER TABLE projects ADD COLUMN rules_digest TEXT NOT NULL DEFAULT '' CHECK (rules_digest = '' OR (length(rules_digest) = 64 AND rules_digest NOT GLOB '*[^0-9a-f]*'));
ALTER TABLE projects ADD COLUMN rules_materialized_at TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN db_status TEXT NOT NULL DEFAULT 'none' CHECK (db_status IN ('none','pending','ready','failed'));
ALTER TABLE projects ADD COLUMN db_path TEXT NOT NULL DEFAULT '' CHECK (length(db_path) <= 1024);
ALTER TABLE projects ADD COLUMN db_digest TEXT NOT NULL DEFAULT '' CHECK (db_digest = '' OR (length(db_digest) = 64 AND db_digest NOT GLOB '*[^0-9a-f]*'));
ALTER TABLE projects ADD COLUMN db_verified_at TEXT NOT NULL DEFAULT '';
