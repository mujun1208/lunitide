-- 0096 Four model catalogs on the existing provider_models rows.
-- kind: llm (everyday chat/tools), vision (OCR/describe), image (generation), video (generation).
-- Existing rows stay llm. kind_default is the global default for that kind; backups are the rest.

ALTER TABLE provider_models ADD COLUMN kind TEXT NOT NULL DEFAULT 'llm';
ALTER TABLE provider_models ADD COLUMN supports_vision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE provider_models ADD COLUMN kind_default INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX ux_provider_kind_default ON provider_models(kind) WHERE kind_default = 1;
