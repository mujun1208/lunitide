-- Retires the Electron prototype's provider-metadata import and credential
-- adoption bookkeeping. The prototype was never released, the Go code that
-- read these tables was removed in 0.4.01, and any credential they carried
-- now lives in the DPAPI store, so the rows have no remaining reader.
--
-- 0005 and 0006, which created these objects, stay embedded and journalled:
-- an applied migration is history, and rewriting it would fail the checksum
-- on every database that already ran it.
--
-- The child table goes first so the foreign key holds on databases where the
-- import actually ran and left rows behind.
DROP INDEX IF EXISTS ix_provider_metadata_migration_items_credential_state;
DROP INDEX IF EXISTS ix_provider_metadata_migration_items_legacy;
DROP TABLE IF EXISTS provider_metadata_migration_items;
DROP TABLE IF EXISTS provider_metadata_migrations;
