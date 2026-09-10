ALTER TABLE model_call_attempts ADD COLUMN bytes_before INTEGER NOT NULL DEFAULT 0 CHECK (bytes_before >= 0);
ALTER TABLE model_call_attempts ADD COLUMN bytes_after INTEGER NOT NULL DEFAULT 0 CHECK (bytes_after >= 0);
