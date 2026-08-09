DROP INDEX IF EXISTS ux_provider_default_model;
ALTER TABLE providers RENAME TO providers_v1;
ALTER TABLE provider_models RENAME TO provider_models_v1;

CREATE TABLE providers (
    id TEXT PRIMARY KEY,
    legacy_id TEXT UNIQUE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),
    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),
    credential_ref TEXT CHECK (credential_ref IS NULL OR length(credential_ref) BETWEEN 1 AND 500),
    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable', 'requires_reentry')),
    status TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    deleted_at TEXT,
    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))
);

CREATE TABLE provider_models (
    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 200),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    position INTEGER NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 49),
    PRIMARY KEY (provider_id, model_id),
    UNIQUE (provider_id, position)
);

CREATE UNIQUE INDEX ux_provider_default_model
ON provider_models(provider_id) WHERE is_default = 1;

-- Data is deliberately copied by the Go migrator in the same transaction.
-- Provider IDs/FKs, credential reference state, and stable model positions
-- cannot be transformed safely by schema-only SQL.
