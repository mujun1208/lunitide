-- 0160: openai_responses is the OpenAI Responses API protocol (OpenAI, Ark
-- standard/Agent Plan, Bailian compatible-mode). SQLite cannot ALTER a CHECK
-- enum, so rebuild the two tables that pin it, preserving the 0119 shape.
PRAGMA legacy_alter_table=ON;
DROP TRIGGER IF EXISTS providers_credential_ref_insert;
DROP TRIGGER IF EXISTS providers_credential_ref_update;
ALTER TABLE providers RENAME TO providers_0160_old;
CREATE TABLE providers (
    id TEXT PRIMARY KEY,
    legacy_id TEXT UNIQUE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic', 'volc_speech', 'openai_responses')),
    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),
    credential_ref TEXT CHECK (credential_ref IS NULL OR length(credential_ref) BETWEEN 1 AND 500),
    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable', 'requires_reentry')),
    status TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    deleted_at TEXT, origin_fingerprint TEXT NOT NULL
    DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'
    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*'), credential_ref_backups TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(credential_ref_backups)),
    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))
);
INSERT INTO providers(
    id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,
    created_at,updated_at,version,deleted_at,origin_fingerprint,credential_ref_backups
)
SELECT
    id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,
    created_at,updated_at,version,deleted_at,origin_fingerprint,credential_ref_backups
FROM providers_0160_old;
DROP TABLE providers_0160_old;
CREATE TRIGGER providers_credential_ref_insert
BEFORE INSERT ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256
BEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END;
CREATE TRIGGER providers_credential_ref_update
BEFORE UPDATE OF credential_ref ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256
BEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END;

ALTER TABLE credential_adoptions RENAME TO credential_adoptions_0160_old;
CREATE TABLE credential_adoptions (
    credential_ref TEXT PRIMARY KEY CHECK (length(credential_ref) BETWEEN 1 AND 256),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 2048),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic', 'volc_speech', 'openai_responses')),
    receipt_id TEXT NOT NULL UNIQUE CHECK (length(receipt_id) BETWEEN 1 AND 64),
    adopted_at TEXT NOT NULL
);
INSERT INTO credential_adoptions(
    credential_ref,provider_id,origin,protocol,receipt_id,adopted_at
)
SELECT
    credential_ref,provider_id,origin,protocol,receipt_id,adopted_at
FROM credential_adoptions_0160_old;
DROP TABLE credential_adoptions_0160_old;
CREATE INDEX ix_credential_adoptions_provider ON credential_adoptions(provider_id);
PRAGMA legacy_alter_table=OFF;
