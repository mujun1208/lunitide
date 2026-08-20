-- 0082: Asset template library (TPL) and project deliverable artifacts.

CREATE TABLE asset_templates (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]'),
    template_code TEXT NOT NULL UNIQUE CHECK (length(template_code) BETWEEN 4 AND 16 AND template_code GLOB 'TPL[0-9]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    template_type TEXT NOT NULL CHECK (template_type IN ('document', 'scaffold')),
    document_type TEXT NOT NULL DEFAULT '' CHECK (length(document_type) <= 128),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    client TEXT NOT NULL DEFAULT '' CHECK (length(client) <= 200),
    mime_type TEXT NOT NULL DEFAULT '' CHECK (length(mime_type) <= 128),
    file_name TEXT NOT NULL DEFAULT '' CHECK (length(file_name) <= 260),
    file_path TEXT NOT NULL DEFAULT '' CHECK (length(file_path) <= 512),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'enabled', 'disabled', 'void')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)
);
CREATE INDEX ix_asset_templates_status ON asset_templates(status, updated_at DESC);

CREATE TABLE project_attachments (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    phase INTEGER NOT NULL CHECK (phase BETWEEN 1 AND 9),
    category TEXT NOT NULL DEFAULT 'phase_doc' CHECK (length(category) <= 64),
    file_name TEXT NOT NULL CHECK (length(file_name) BETWEEN 1 AND 260),
    mime_type TEXT NOT NULL DEFAULT '' CHECK (length(mime_type) <= 128),
    file_path TEXT NOT NULL CHECK (length(file_path) BETWEEN 1 AND 512),
    digest TEXT NOT NULL DEFAULT '' CHECK (length(digest) <= 128),
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_project_attachments_project ON project_attachments(project_id, phase, updated_at DESC);

CREATE TABLE project_deliverables (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    phase INTEGER NOT NULL CHECK (phase BETWEEN 1 AND 9),
    document_type TEXT NOT NULL CHECK (length(document_type) BETWEEN 1 AND 128),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    template_id TEXT REFERENCES asset_templates(id),
    attachment_id TEXT REFERENCES project_attachments(id),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'review', 'approved', 'immutable')),
    gate_confirmations INTEGER NOT NULL DEFAULT 0 CHECK (gate_confirmations BETWEEN 0 AND 3),
    digest TEXT NOT NULL DEFAULT '' CHECK (length(digest) <= 128),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    UNIQUE(project_id, phase, document_type)
);
CREATE INDEX ix_project_deliverables_project ON project_deliverables(project_id, phase, status);
