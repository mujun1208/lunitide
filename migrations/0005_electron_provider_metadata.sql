CREATE TABLE provider_metadata_migrations (
    source_fingerprint TEXT PRIMARY KEY CHECK (length(source_fingerprint) = 64 AND source_fingerprint NOT GLOB '*[^0-9a-f]*'),
    source_path_hash TEXT NOT NULL CHECK (length(source_path_hash) = 64 AND source_path_hash NOT GLOB '*[^0-9a-f]*'),
    source_version TEXT NOT NULL CHECK (source_version IN ('0.1', '0.2', '0.2.1')),
    state TEXT NOT NULL CHECK (state IN ('running', 'completed', 'failed')),
    processed INTEGER NOT NULL DEFAULT 0 CHECK (processed >= 0),
    total INTEGER NOT NULL DEFAULT 0 CHECK (total BETWEEN 0 AND 100),
    imported INTEGER NOT NULL DEFAULT 0 CHECK (imported >= 0),
    duplicates INTEGER NOT NULL DEFAULT 0 CHECK (duplicates >= 0),
    conflicts INTEGER NOT NULL DEFAULT 0 CHECK (conflicts >= 0),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (processed <= total),
    CHECK (imported + duplicates + conflicts <= processed)
);

CREATE TABLE provider_metadata_migration_items (
    source_fingerprint TEXT NOT NULL REFERENCES provider_metadata_migrations(source_fingerprint) ON DELETE CASCADE,
    item_fingerprint TEXT NOT NULL CHECK (length(item_fingerprint) = 64 AND item_fingerprint NOT GLOB '*[^0-9a-f]*'),
    legacy_id TEXT NOT NULL CHECK (length(legacy_id) BETWEEN 1 AND 128),
    result TEXT NOT NULL CHECK (result IN ('imported', 'duplicate', 'conflict')),
    provider_id TEXT,
    detail_code TEXT NOT NULL CHECK (length(detail_code) BETWEEN 1 AND 64),
    PRIMARY KEY (source_fingerprint, item_fingerprint)
);
CREATE INDEX ix_provider_metadata_migration_items_legacy ON provider_metadata_migration_items(legacy_id);
