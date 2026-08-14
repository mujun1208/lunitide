-- Transactional, crash-visible resource reservations for the M4 durable run core.
CREATE TABLE run_usage_reservation (
    id TEXT PRIMARY KEY CHECK (length(id) = 26),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    reserved_json TEXT NOT NULL CHECK (json_valid(reserved_json)),
    committed_json TEXT CHECK (committed_json IS NULL OR json_valid(committed_json)),
    status TEXT NOT NULL CHECK (status IN ('reserved','committed','released')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_run_usage_reservation_active ON run_usage_reservation(run_id,status);
