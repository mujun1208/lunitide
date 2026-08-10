-- Compaction checkpoint: versioned summaries of source message ranges.
-- Checkpoints are derived data, never substitutes for source messages.
-- Only accepted checkpoints are used in prompt assembly.
CREATE TABLE compaction_checkpoints (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    version INTEGER NOT NULL CHECK (version > 0),
    source_start_id TEXT NOT NULL REFERENCES messages(id) ON DELETE RESTRICT,
    source_end_id TEXT NOT NULL REFERENCES messages(id) ON DELETE RESTRICT,
    source_start_seq INTEGER NOT NULL CHECK (source_start_seq BETWEEN 1 AND 9007199254740991),
    source_end_seq INTEGER NOT NULL CHECK (source_end_seq BETWEEN 1 AND 9007199254740991),
    source_digest TEXT NOT NULL CHECK (length(source_digest) = 64 AND source_digest NOT GLOB '*[^0-9a-f]*'),
    prev_checkpoint_id TEXT REFERENCES compaction_checkpoints(id),
    prev_checkpoint_digest TEXT CHECK (prev_checkpoint_digest IS NULL OR (length(prev_checkpoint_digest) = 64 AND prev_checkpoint_digest NOT GLOB '*[^0-9a-f]*')),
    summary_schema_version TEXT NOT NULL DEFAULT '1.0' CHECK (length(summary_schema_version) <= 16),
    trigger TEXT NOT NULL CHECK (trigger IN ('automatic', 'manual', 'handoff')),
    trigger_reason TEXT NOT NULL DEFAULT '' CHECK (length(trigger_reason) <= 1024),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'superseded')),
    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),
    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),
    summary_json TEXT NOT NULL DEFAULT '{}' CHECK (length(summary_json) BETWEEN 2 AND 65536),
    human_summary TEXT NOT NULL DEFAULT '' CHECK (length(human_summary) <= 32768),
    failure_code TEXT CHECK (failure_code IS NULL OR length(failure_code) <= 64),
    created_at TEXT NOT NULL,
    completed_at TEXT,
    UNIQUE (session_id, version),
    CHECK (source_start_seq <= source_end_seq),
    CHECK ((status IN ('succeeded', 'failed', 'superseded')) = (completed_at IS NOT NULL))
);

CREATE INDEX ix_compaction_checkpoints_session ON compaction_checkpoints(session_id, version DESC);
CREATE INDEX ix_compaction_checkpoints_status ON compaction_checkpoints(session_id, status);

-- Handoff capsule: cross-window/session context transfer.
-- Each capsule binds a source session to a destination session.
CREATE TABLE handoff_capsules (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    source_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    dest_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    checkpoint_id TEXT NOT NULL REFERENCES compaction_checkpoints(id) ON DELETE RESTRICT,
    active_tasks_json TEXT NOT NULL DEFAULT '[]' CHECK (length(active_tasks_json) <= 65536),
    recent_message_ids TEXT NOT NULL DEFAULT '[]' CHECK (length(recent_message_ids) <= 65536),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'activated', 'expired', 'revoked')),
    created_at TEXT NOT NULL,
    activated_at TEXT,
    expires_at TEXT
);

CREATE INDEX ix_handoff_capsules_source ON handoff_capsules(source_session_id, created_at DESC);
CREATE INDEX ix_handoff_capsules_dest ON handoff_capsules(dest_session_id);