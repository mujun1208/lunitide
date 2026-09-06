CREATE TABLE plan_run_executions (
    plan_run_id TEXT PRIMARY KEY REFERENCES agent_plan_runs(id) ON DELETE RESTRICT,
    agent_run_id TEXT NOT NULL UNIQUE REFERENCES agent_run(id) ON DELETE RESTRICT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
    spec_json TEXT NOT NULL CHECK (length(spec_json) BETWEEN 2 AND 16384 AND json_valid(spec_json)),
    status TEXT NOT NULL CHECK (status IN ('running','cancel_requested','succeeded','failed','cancelled','interrupted','outcome_unknown')),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 16384),
    failure TEXT NOT NULL DEFAULT '' CHECK (length(failure) <= 4096),
    artifacts_json TEXT NOT NULL DEFAULT '[]' CHECK (length(artifacts_json) BETWEEN 2 AND 65536 AND json_valid(artifacts_json)),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_plan_run_executions_status ON plan_run_executions(status, updated_at);
