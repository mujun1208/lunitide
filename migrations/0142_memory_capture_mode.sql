-- Preserve existing memory and the master off switch. User-directed capture is
-- independent from the older broad nomination/observation setting.
ALTER TABLE memory_settings ADD COLUMN capture_mode TEXT NOT NULL DEFAULT 'auto'
CHECK (capture_mode IN ('auto', 'manual', 'off'));
