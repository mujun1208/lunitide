CREATE TABLE IF NOT EXISTS providers (
    id TEXT PRIMARY KEY,
    legacy_id TEXT UNIQUE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),
    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),
    credential_ref TEXT,
    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS provider_models (
    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 500),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 500),
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (provider_id, model_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_provider_default_model
ON provider_models(provider_id) WHERE is_default = 1;
