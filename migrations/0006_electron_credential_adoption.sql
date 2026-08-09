ALTER TABLE provider_metadata_migration_items ADD COLUMN credential_migration_state TEXT NOT NULL DEFAULT 'none'
    CHECK (credential_migration_state IN ('pending', 'adopted', 'superseded', 'rejected', 'none'));
ALTER TABLE provider_metadata_migration_items ADD COLUMN credential_receipt_id TEXT
    CHECK (credential_receipt_id IS NULL OR length(credential_receipt_id) BETWEEN 1 AND 64);
ALTER TABLE provider_metadata_migration_items ADD COLUMN credential_updated_at TEXT;
CREATE INDEX ix_provider_metadata_migration_items_credential_state
    ON provider_metadata_migration_items(credential_migration_state, source_fingerprint);
