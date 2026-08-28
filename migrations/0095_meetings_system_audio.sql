-- 0095 This-PC system-audio mix for meeting notes. Never another machine.
-- SQLite cannot ALTER a CHECK constraint, so meetings is rebuilt. Child
-- tables keep their rows; foreign_keys are off during migration.
CREATE TABLE meetings_0095 (
    meeting_id TEXT PRIMARY KEY CHECK (length(meeting_id) = 26 AND substr(meeting_id, 1, 1) GLOB '[0-7]' AND meeting_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 200),
    status TEXT NOT NULL CHECK (status IN ('recording','transcribed','summarizing','ready','needs_summary')),
    audio_source TEXT NOT NULL DEFAULT 'microphone' CHECK (audio_source IN ('microphone','microphone_and_system')),
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 65536),
    actions TEXT NOT NULL DEFAULT '' CHECK (length(actions) <= 32768),
    transcript TEXT NOT NULL DEFAULT '' CHECK (length(transcript) <= 1048576),
    summary_error TEXT NOT NULL DEFAULT '' CHECK (length(summary_error) <= 1024),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO meetings_0095 SELECT * FROM meetings;
DROP TABLE meetings;
ALTER TABLE meetings_0095 RENAME TO meetings;
CREATE INDEX ix_meetings_started ON meetings(started_at DESC, meeting_id DESC);
