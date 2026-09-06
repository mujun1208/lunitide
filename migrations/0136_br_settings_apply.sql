ALTER TABLE br_settings ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1);
ALTER TABLE br_settings ADD COLUMN apply_status TEXT NOT NULL DEFAULT 'applied' CHECK (apply_status IN ('applied','applying','failed'));
ALTER TABLE br_settings ADD COLUMN apply_error TEXT NOT NULL DEFAULT '' CHECK (length(apply_error) <= 1024);
