ALTER TABLE cc_security_config ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0);
