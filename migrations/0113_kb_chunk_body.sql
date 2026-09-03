-- 0113 KB chunk body: searchable text. embedding stays NULL (vector off).
ALTER TABLE kb_chunks ADD COLUMN body TEXT NOT NULL DEFAULT '';
