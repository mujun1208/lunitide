-- 0115 session metadata bag for mroContext and other session keys.
ALTER TABLE sessions ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (length(metadata_json) BETWEEN 2 AND 16384);
