-- 0064 M8 slice 4 (T-8.4.x): workflow bundles and automation run
-- projections.
--
-- WorkflowBundle checksum/permission precheck failures quarantine with zero
-- dispatch (M8-021/022); high-risk actions wait at the execution point for
-- just-in-time confirmation (M8-023). AutomationRun is an M5/M6 Run ID
-- projection: M8 never advances run state itself and creates no second
-- lease/checkpoint/retry/effect-journal/compensation table (the single
-- execution kernel stays M5/M6). Idempotency keys are unique so replays
-- answer the original run id.
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE workflow_bundles (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    version INTEGER NOT NULL CHECK (version >= 1),
    checksum TEXT NOT NULL UNIQUE CHECK (length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'),
    permissions TEXT NOT NULL CHECK (length(permissions) >= 2),
    rollback_ref TEXT,
    state TEXT NOT NULL CHECK (state IN ('verified','quarantined')),
    created_at TEXT NOT NULL
);

CREATE TABLE automation_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    bundle_id TEXT NOT NULL REFERENCES workflow_bundles(id),
    state TEXT NOT NULL CHECK (state IN ('RECEIVED','POLICY_CHECKED','WAITING_CONFIRMATION','DISPATCHED','CHECKPOINTED','SUCCEEDED','COMPENSATING','QUARANTINED')),
    approval_ref TEXT,
    budget_json TEXT NOT NULL CHECK (length(budget_json) >= 2),
    checkpoint_json TEXT,
    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
