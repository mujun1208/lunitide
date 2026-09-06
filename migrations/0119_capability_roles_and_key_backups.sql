-- 0119 capability roles + provider key backups
CREATE TABLE capability_role_bindings (
    role TEXT PRIMARY KEY CHECK (role IN ('chat','flash','vision','embed','judge','gui')),
    provider_id TEXT,
    model_id TEXT,
    allow_judge_eq_chat INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);
ALTER TABLE providers ADD COLUMN credential_ref_backups TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(credential_ref_backups));
