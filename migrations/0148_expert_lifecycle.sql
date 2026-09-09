-- Existing local experts have ambiguous provenance. Do not infer manual ownership.
ALTER TABLE expert_catalog ADD COLUMN creation_origin TEXT NOT NULL DEFAULT 'legacy'
    CHECK (creation_origin IN ('manual','builtin','catalog','legacy'));
ALTER TABLE expert_catalog ADD COLUMN deleted_at TEXT NOT NULL DEFAULT '';
