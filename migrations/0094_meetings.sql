-- 0094 This-PC meeting notes: live transcript plus generated documents.
-- Audio stays on this machine. Independent of people P2P and AI 对话 sessions.
CREATE TABLE meetings (
    meeting_id TEXT PRIMARY KEY CHECK (length(meeting_id) = 26 AND substr(meeting_id, 1, 1) GLOB '[0-7]' AND meeting_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 200),
    status TEXT NOT NULL CHECK (status IN ('recording','transcribed','summarizing','ready','needs_summary')),
    audio_source TEXT NOT NULL DEFAULT 'microphone' CHECK (audio_source IN ('microphone')),
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
CREATE INDEX ix_meetings_started ON meetings(started_at DESC, meeting_id DESC);
CREATE TABLE meeting_segments (
    segment_id TEXT PRIMARY KEY CHECK (length(segment_id) = 26 AND substr(segment_id, 1, 1) GLOB '[0-7]' AND segment_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    meeting_id TEXT NOT NULL REFERENCES meetings(meeting_id) ON DELETE CASCADE,
    seq INTEGER NOT NULL CHECK (seq >= 1),
    started_ms INTEGER NOT NULL DEFAULT 0 CHECK (started_ms >= 0),
    body TEXT NOT NULL CHECK (length(body) BETWEEN 1 AND 16384),
    created_at TEXT NOT NULL,
    UNIQUE(meeting_id, seq)
);
CREATE INDEX ix_meeting_segments_meeting ON meeting_segments(meeting_id, seq);
CREATE TABLE meeting_docs (
    doc_id TEXT PRIMARY KEY CHECK (length(doc_id) = 26 AND substr(doc_id, 1, 1) GLOB '[0-7]' AND doc_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    meeting_id TEXT NOT NULL REFERENCES meetings(meeting_id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('markdown','html')),
    body TEXT NOT NULL CHECK (length(body) <= 2097152),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_meeting_docs_meeting ON meeting_docs(meeting_id, created_at DESC);
