-- M10 wave-2: queued user input (FR-28 / FR-34).
-- Session-scoped durable queue: rows survive restarts and are only ever
-- settled explicitly (withdraw by the user, consume into the next turn),
-- so nothing is dropped silently. run_id stays nullable for now — the
-- chat stream has no durable agent_run row yet; the column is the join
-- point once the M4 run kernel drives the main conversation.
CREATE TABLE queued_user_messages (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id TEXT CHECK (run_id IS NULL OR (length(run_id) = 26 AND substr(run_id, 1, 1) GLOB '[0-7]' AND run_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    seq INTEGER NOT NULL CHECK (seq > 0),
    payload TEXT NOT NULL CHECK (length(payload) BETWEEN 1 AND 8000),
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','injected','withdrawn')),
    mark TEXT NOT NULL DEFAULT 'turn_boundary' CHECK (mark IN ('turn_boundary','with_approval')),
    request_id TEXT NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    consumed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (session_id, seq),
    UNIQUE (session_id, request_id)
);
CREATE INDEX ix_quem_session ON queued_user_messages(session_id, status, seq);
CREATE INDEX ix_quem_recent ON queued_user_messages(session_id, created_at);
