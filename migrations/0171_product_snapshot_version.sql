ALTER TABLE product_snapshots ADD COLUMN product_version TEXT NOT NULL DEFAULT '';
ALTER TABLE product_snapshots ADD COLUMN catalog_passed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE product_snapshots ADD COLUMN catalog_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE product_snapshots ADD COLUMN added_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE product_snapshots ADD COLUMN updated_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE product_snapshots ADD COLUMN removed_count INTEGER NOT NULL DEFAULT 0;
