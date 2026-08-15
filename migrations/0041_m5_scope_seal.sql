-- M5 T-5.0.3 Scope Seal (DB-side): agent_plan_runs must never carry child runs
-- (parent_run_id NOT NULL) again. M6 is the only milestone allowed to lift this.
-- DR-20260814-01: seal is an M5 release gate; handler/wire removal ships in app code.
-- Migration runs with PRAGMA foreign_keys=OFF; integrity is re-checked after apply.

-- Fold any legacy child rows into cancelled orphans before the NOT-NULL seal,
-- so the rebuild below cannot silently drop data.
UPDATE agent_plan_runs
SET status = 'cancelled',
    failure = 'scope-seal: child run disabled by 0041_m5_scope_seal',
    parent_run_id = NULL,
    terminal_at = COALESCE(terminal_at, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE parent_run_id IS NOT NULL;

-- Rebuild path: rename the referencing table first so the FK text survives.
ALTER TABLE agent_plan_run_events RENAME TO agent_plan_run_events_0041_old;
-- The renamed table keeps its index; drop it before the new table reuses the name.
DROP INDEX ix_agent_plan_run_events_run_sequence;

DROP INDEX ix_agent_plan_runs_parent;
DROP INDEX ix_agent_plan_runs_plan_created;
DROP INDEX ix_agent_plan_runs_status;

ALTER TABLE agent_plan_runs RENAME TO agent_plan_runs_0041_old;

CREATE TABLE agent_plan_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    parent_run_id TEXT REFERENCES agent_plan_runs(id) ON DELETE CASCADE CHECK (parent_run_id IS NULL),
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (length(role) BETWEEN 1 AND 128),
    todo_id TEXT NOT NULL CHECK (length(todo_id) = 26 AND substr(todo_id, 1, 1) GLOB '[0-7]' AND todo_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    todo_title TEXT NOT NULL CHECK (length(todo_title) BETWEEN 1 AND 200),
    todo_description TEXT NOT NULL DEFAULT '' CHECK (length(todo_description) <= 4096),
    todo_metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (length(todo_metadata_json) BETWEEN 2 AND 8192),
    status TEXT NOT NULL CHECK (status IN ('queued','running','joining','succeeded','failed','cancel_requested','cancelled','timed_out')),
    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 8),
    failure TEXT NOT NULL DEFAULT '' CHECK (length(failure) <= 2048),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    terminal_at TEXT,
    version INTEGER NOT NULL CHECK (version > 0),
    CHECK ((status IN ('succeeded','failed','cancelled','timed_out')) = (terminal_at IS NOT NULL))
);

CREATE INDEX ix_agent_plan_runs_plan_created ON agent_plan_runs(plan_id, created_at, id);
CREATE INDEX ix_agent_plan_runs_status ON agent_plan_runs(status);

INSERT INTO agent_plan_runs (id, parent_run_id, plan_id, node_id, role, todo_id, todo_title, todo_description, todo_metadata_json, status, depth, failure, created_at, updated_at, terminal_at, version)
SELECT id, parent_run_id, plan_id, node_id, role, todo_id, todo_title, todo_description, todo_metadata_json, status, depth, failure, created_at, updated_at, terminal_at, version
FROM agent_plan_runs_0041_old;

CREATE TABLE agent_plan_run_events (
    sequence INTEGER PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES agent_plan_runs(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('run_created','status_changed','restart_reconciled')),
    from_status TEXT NOT NULL DEFAULT '' CHECK (from_status = '' OR from_status IN ('queued','running','joining','succeeded','failed','cancel_requested','cancelled','timed_out')),
    to_status TEXT NOT NULL CHECK (to_status IN ('queued','running','joining','succeeded','failed','cancel_requested','cancelled','timed_out')),
    detail TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 2048),
    created_at TEXT NOT NULL
);

CREATE INDEX ix_agent_plan_run_events_run_sequence ON agent_plan_run_events(run_id, sequence);

INSERT INTO agent_plan_run_events (sequence, run_id, type, from_status, to_status, detail, created_at)
SELECT sequence, run_id, type, from_status, to_status, detail, created_at
FROM agent_plan_run_events_0041_old;

DROP TABLE agent_plan_run_events_0041_old;
DROP TABLE agent_plan_runs_0041_old;
