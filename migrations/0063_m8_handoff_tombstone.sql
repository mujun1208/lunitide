-- 0063 M8 slice 3 (T-8.3.x): handoff, recursive tombstones, device replicas
-- and the sync conflict box.
--
-- Handoffs are redacted by the sender against the receiver policy before
-- offering; unread before accept (M8-015), expired refuse (M8-014), repeated
-- accepts are idempotent. Tombstones hide the read face immediately, then
-- propagate along the dependency graph with a resumable cursor; per-
-- projection ACKs are collected before compaction (FR-07, M8-016/017).
-- Device replicas carry vector clocks; revoked devices cannot download
-- (M8-019) and stale ACKs are refused (M8-020). Sync conflicts are explicit
-- (no silent last-write-wins, M8-018).
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE handoffs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    sender TEXT NOT NULL CHECK (length(sender) BETWEEN 1 AND 128),
    receiver TEXT NOT NULL CHECK (length(receiver) BETWEEN 1 AND 128),
    manifest TEXT NOT NULL CHECK (length(manifest) >= 2),
    redaction_log TEXT NOT NULL CHECK (length(redaction_log) >= 2),
    state TEXT NOT NULL CHECK (state IN ('sent','accepted','expired')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE memory_tombstones (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_ref TEXT NOT NULL CHECK (length(root_ref) BETWEEN 1 AND 128),
    cascade_cursor TEXT NOT NULL CHECK (length(cascade_cursor) >= 2),
    ack_set TEXT NOT NULL CHECK (length(ack_set) >= 2),
    proof_digest TEXT CHECK (proof_digest IS NULL OR (length(proof_digest) = 64 AND proof_digest NOT GLOB '*[^0-9a-f]*')),
    state TEXT NOT NULL CHECK (state IN ('pending','propagating','verified','compacted')),
    created_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE device_replicas (
    device_id TEXT PRIMARY KEY CHECK (length(device_id) = 26 AND substr(device_id, 1, 1) GLOB '[0-7]' AND device_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    vector_clock TEXT NOT NULL CHECK (length(vector_clock) >= 2),
    last_ack INTEGER NOT NULL CHECK (last_ack >= 0),
    trust_state TEXT NOT NULL CHECK (trust_state IN ('trusted','revoked')),
    created_at TEXT NOT NULL
);

CREATE TABLE sync_conflicts (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    json_pointer TEXT NOT NULL CHECK (length(json_pointer) BETWEEN 1 AND 512),
    variants TEXT NOT NULL CHECK (length(variants) >= 2),
    resolution TEXT,
    state TEXT NOT NULL CHECK (state IN ('open','resolved')),
    created_at TEXT NOT NULL
);
