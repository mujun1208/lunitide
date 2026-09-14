-- 0158: task contracts, run bindings, reservation isolated rebuild (T06).
CREATE TABLE execution_task_contracts (
    task_id TEXT PRIMARY KEY CHECK (length(task_id) BETWEEN 1 AND 64),
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    goal_revision INTEGER NOT NULL DEFAULT 1 CHECK (goal_revision >= 1),
    spec_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(spec_json) AND length(spec_json) <= 1048576),
    budget_policy_json TEXT NOT NULL CHECK (json_valid(budget_policy_json) AND length(budget_policy_json) <= 65536),
    policy_revision TEXT NOT NULL CHECK (length(policy_revision) BETWEEN 1 AND 128),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    outcome_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(outcome_json)),
    active_elapsed_ms INTEGER NOT NULL DEFAULT 0 CHECK (active_elapsed_ms >= 0),
    active_since TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TEXT NOT NULL DEFAULT '',
    runtime_epoch TEXT NOT NULL DEFAULT '' CHECK (length(runtime_epoch) <= 64),
    activity_integrity TEXT NOT NULL DEFAULT 'measured' CHECK (activity_integrity IN ('measured','estimated')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE execution_run_bindings (
    run_id TEXT PRIMARY KEY REFERENCES agent_run(id) ON DELETE CASCADE,
    owner_scope TEXT NOT NULL CHECK (length(owner_scope) BETWEEN 1 AND 128),
    task_id TEXT NOT NULL REFERENCES execution_task_contracts(task_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 64),
    parent_scope_id TEXT CHECK (parent_scope_id IS NULL OR (length(parent_scope_id) BETWEEN 1 AND 64 AND parent_scope_id != scope_id)),
    run_kind TEXT NOT NULL CHECK (length(run_kind) BETWEEN 1 AND 64),
    execution_ref TEXT NOT NULL CHECK (length(execution_ref) BETWEEN 1 AND 128),
    execution_mode TEXT NOT NULL CHECK (execution_mode IN ('accounting_only','owned_runtime')),
    scope_policy_json TEXT NOT NULL CHECK (json_valid(scope_policy_json) AND length(scope_policy_json) <= 65536),
    scope_policy_revision TEXT NOT NULL CHECK (length(scope_policy_revision) BETWEEN 1 AND 128),
    active_elapsed_ms INTEGER NOT NULL DEFAULT 0 CHECK (active_elapsed_ms >= 0),
    active_since TEXT NOT NULL DEFAULT '',
    last_heartbeat_at TEXT NOT NULL DEFAULT '',
    runtime_epoch TEXT NOT NULL DEFAULT '' CHECK (length(runtime_epoch) <= 64),
    activity_integrity TEXT NOT NULL DEFAULT 'measured' CHECK (activity_integrity IN ('measured','estimated')),
    created_at TEXT NOT NULL,
    UNIQUE(task_id, scope_id),
    UNIQUE(owner_scope, run_kind, execution_ref),
    FOREIGN KEY (task_id, parent_scope_id) REFERENCES execution_run_bindings(task_id, scope_id)
);
CREATE TABLE execution_step_outcomes (
    task_id TEXT NOT NULL REFERENCES execution_task_contracts(task_id) ON DELETE CASCADE,
    goal_revision INTEGER NOT NULL CHECK (goal_revision >= 1),
    step_id TEXT NOT NULL CHECK (length(step_id) BETWEEN 1 AND 64),
    attempt_id TEXT NOT NULL CHECK (length(attempt_id) BETWEEN 1 AND 64),
    state TEXT NOT NULL CHECK (length(state) BETWEEN 1 AND 32),
    outcome_json TEXT NOT NULL CHECK (json_valid(outcome_json)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    PRIMARY KEY (task_id, goal_revision, step_id, attempt_id)
);
CREATE TABLE run_usage_reservation_0158 (
    id TEXT PRIMARY KEY CHECK (length(id) = 26),
    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    reserved_json TEXT NOT NULL CHECK (json_valid(reserved_json)),
    committed_json TEXT CHECK (committed_json IS NULL OR json_valid(committed_json)),
    status TEXT NOT NULL CHECK (status IN ('reserved','committed','released','isolated')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    task_id TEXT CHECK (task_id IS NULL OR length(task_id) BETWEEN 1 AND 64),
    scope_id TEXT CHECK (scope_id IS NULL OR length(scope_id) BETWEEN 1 AND 64),
    attempt_id TEXT CHECK (attempt_id IS NULL OR length(attempt_id) BETWEEN 1 AND 64),
    request_digest TEXT CHECK (request_digest IS NULL OR length(request_digest) BETWEEN 1 AND 128),
    dispatched INTEGER NOT NULL DEFAULT 0 CHECK (dispatched IN (0,1)),
    integrity TEXT CHECK (integrity IS NULL OR integrity IN ('reported','partial','unknown')),
    reserved_v2_json TEXT CHECK (reserved_v2_json IS NULL OR json_valid(reserved_v2_json)),
    settled_v2_json TEXT CHECK (settled_v2_json IS NULL OR json_valid(settled_v2_json)),
    receipt_id TEXT CHECK (receipt_id IS NULL OR length(receipt_id) BETWEEN 1 AND 128),
    settlement_revision INTEGER CHECK (settlement_revision IS NULL OR settlement_revision >= 1),
    settlement_digest TEXT CHECK (settlement_digest IS NULL OR length(settlement_digest) BETWEEN 1 AND 128),
    isolated_json TEXT CHECK (isolated_json IS NULL OR json_valid(isolated_json)),
    CHECK (
        (task_id IS NULL AND scope_id IS NULL AND attempt_id IS NULL)
        OR
        (task_id IS NOT NULL AND scope_id IS NOT NULL AND attempt_id IS NOT NULL)
    )
);
INSERT INTO run_usage_reservation_0158(
    id, run_id, reserved_json, committed_json, status, created_at, updated_at,
    task_id, scope_id, attempt_id, request_digest, dispatched, integrity,
    reserved_v2_json, settled_v2_json, receipt_id, settlement_revision, settlement_digest, isolated_json
)
SELECT id, run_id, reserved_json, committed_json, status, created_at, updated_at,
    NULL, NULL, NULL, NULL, 0, NULL,
    NULL, NULL, NULL, NULL, NULL, NULL
FROM run_usage_reservation;
DROP INDEX IF EXISTS ix_run_usage_reservation_active;
DROP TABLE run_usage_reservation;
ALTER TABLE run_usage_reservation_0158 RENAME TO run_usage_reservation;
CREATE INDEX ix_run_usage_reservation_active ON run_usage_reservation(run_id,status);
CREATE INDEX ix_run_usage_reservation_task_status ON run_usage_reservation(task_id,status);
CREATE INDEX ix_run_usage_reservation_task_scope_status ON run_usage_reservation(task_id,scope_id,status);
CREATE UNIQUE INDEX ix_run_usage_reservation_attempt ON run_usage_reservation(attempt_id) WHERE attempt_id IS NOT NULL;
ALTER TABLE evidence ADD COLUMN metadata_json TEXT CHECK (metadata_json IS NULL OR (json_valid(metadata_json) AND length(metadata_json) <= 262144));
