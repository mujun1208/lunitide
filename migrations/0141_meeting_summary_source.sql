ALTER TABLE meetings ADD COLUMN transcript_revision INTEGER NOT NULL DEFAULT 0 CHECK (transcript_revision >= 0);
ALTER TABLE meetings ADD COLUMN summary_source_revision INTEGER NOT NULL DEFAULT 0 CHECK (summary_source_revision >= 0);
ALTER TABLE meetings ADD COLUMN summary_source_digest TEXT NOT NULL DEFAULT '' CHECK (summary_source_digest = '' OR (length(summary_source_digest) = 64 AND summary_source_digest NOT GLOB '*[^0-9a-f]*'));
ALTER TABLE meetings ADD COLUMN summary_source_title TEXT NOT NULL DEFAULT '' CHECK (length(summary_source_title) <= 200);
ALTER TABLE meetings ADD COLUMN summary_source_transcript TEXT NOT NULL DEFAULT '' CHECK (length(summary_source_transcript) <= 1048576);
ALTER TABLE meetings ADD COLUMN summary_edited INTEGER NOT NULL DEFAULT 0 CHECK (summary_edited IN (0,1));

-- The current legacy transcript is known, but its relationship to an existing
-- model output is not. Never invent an input snapshot for a legacy summary.
UPDATE meetings SET transcript_revision = 1 WHERE transcript <> '';
UPDATE meetings SET status = 'needs_summary', summary_error = '旧摘要没有保存输入版本，原文和摘要已保留；请核对或重新生成。'
WHERE status = 'ready' AND (summary <> '' OR actions <> '');
