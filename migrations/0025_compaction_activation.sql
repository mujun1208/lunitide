-- Session-level activation pointer for compaction checkpoints.
-- A succeeded checkpoint is only a draft until this pointer is advanced.
CREATE TABLE compaction_activations (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE RESTRICT,
    checkpoint_id TEXT REFERENCES compaction_checkpoints(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),
    updated_at TEXT NOT NULL
);

CREATE TABLE compaction_activation_bases (
    checkpoint_id TEXT PRIMARY KEY REFERENCES compaction_checkpoints(id) ON DELETE CASCADE,
    base_revision INTEGER NOT NULL CHECK (base_revision >= 0)
);

-- Preserve the pre-migration meaning of the latest succeeded checkpoint.
INSERT INTO compaction_activations(session_id, checkpoint_id, revision, updated_at)
SELECT s.id,
       (SELECT c.id FROM compaction_checkpoints c WHERE c.session_id=s.id AND c.status='succeeded' ORDER BY c.version DESC LIMIT 1),
       COALESCE((SELECT c.version FROM compaction_checkpoints c WHERE c.session_id=s.id AND c.status='succeeded' ORDER BY c.version DESC LIMIT 1), 0),
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM sessions s;

INSERT INTO compaction_activation_bases(checkpoint_id, base_revision)
SELECT c.id, COALESCE(a.revision, 0)
FROM compaction_checkpoints c
LEFT JOIN compaction_activations a ON a.session_id=c.session_id;
