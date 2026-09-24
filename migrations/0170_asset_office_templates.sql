-- Office templates: PPT, Word, and Excel files that the office studio can import as a draft.

CREATE TABLE asset_templates_0170 (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]'),
    template_code TEXT NOT NULL UNIQUE CHECK (length(template_code) BETWEEN 4 AND 16 AND template_code GLOB 'TPL[0-9]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    template_type TEXT NOT NULL CHECK (template_type IN ('document', 'scaffold', 'ppt', 'word', 'excel')),
    document_type TEXT NOT NULL DEFAULT '' CHECK (length(document_type) <= 128),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    client TEXT NOT NULL DEFAULT '' CHECK (length(client) <= 200),
    mime_type TEXT NOT NULL DEFAULT '' CHECK (length(mime_type) <= 128),
    file_name TEXT NOT NULL DEFAULT '' CHECK (length(file_name) <= 260),
    file_path TEXT NOT NULL DEFAULT '' CHECK (length(file_path) <= 512),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'enabled', 'disabled', 'void')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    org_id TEXT REFERENCES organizations(org_id)
);

INSERT INTO asset_templates_0170(
    id, template_code, name, template_type, document_type, description, client,
    mime_type, file_name, file_path, status, created_at, updated_at, version, org_id
)
SELECT
    id, template_code, name, template_type, document_type, description, client,
    mime_type, file_name, file_path, status, created_at, updated_at, version, org_id
FROM asset_templates;

DROP TABLE asset_templates;
ALTER TABLE asset_templates_0170 RENAME TO asset_templates;

CREATE INDEX ix_asset_templates_status ON asset_templates(status, updated_at DESC);
CREATE INDEX ix_asset_templates_scope_created ON asset_templates(org_id, created_at DESC, id DESC);
