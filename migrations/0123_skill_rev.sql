-- Q-03 skill optimistic concurrency: monotonically increasing rev column.
ALTER TABLE skills ADD COLUMN rev INTEGER NOT NULL DEFAULT 0;