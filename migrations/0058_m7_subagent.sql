-- 0058 M7 slice 6 (T-7.6.x): the read-only subagent runtime.
--
-- SubagentRun is a capability-restricted derived execution unit of one Root
-- Run (02-技术设计 §只读子代理运行时): read-only evidence collection only,
-- UNIQUE(root_run_id, idempotency_key) makes spawn idempotent per root, and
-- capability_digest binds the read-only whitelist snapshot so any drift
-- between spawn and join fails closed (M7-SAG-004 TOCTOU).
--
-- SubagentObservation is append-only join evidence (seq ordered): UPDATE and
-- DELETE both trip M7-EVD-001 (same semantics as scenario 39). House
-- adaptations as in 0051-0057: TEXT RFC3339 timestamps, ULID CHECKs on id
-- columns, 64-hex digest CHECKs.

CREATE TABLE subagent_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_run_id TEXT NOT NULL CHECK (length(root_run_id) BETWEEN 1 AND 128),
    stage_run_id TEXT CHECK (stage_run_id IS NULL OR length(stage_run_id) BETWEEN 1 AND 128),
    purpose TEXT NOT NULL CHECK (length(purpose) BETWEEN 1 AND 2000),
    capability_digest TEXT NOT NULL CHECK (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*'),
    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),
    persona_digest TEXT CHECK (persona_digest IS NULL OR (length(persona_digest) = 64 AND persona_digest NOT GLOB '*[^0-9a-f]*')),
    status TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed','cancelled','orphaned')),
    budget_tokens INTEGER NOT NULL CHECK (budget_tokens >= 1),
    spent_tokens INTEGER NOT NULL DEFAULT 0 CHECK (spent_tokens >= 0),
    deadline_ms INTEGER NOT NULL CHECK (deadline_ms >= 1),
    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    created_at TEXT NOT NULL,
    completed_at TEXT,
    UNIQUE (root_run_id, idempotency_key)
);

CREATE INDEX ix_sar_root ON subagent_runs(root_run_id, status);

CREATE TABLE subagent_observations (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subagent_run_id TEXT NOT NULL REFERENCES subagent_runs(id),
    seq INTEGER NOT NULL CHECK (seq >= 1),
    evidence_id TEXT NOT NULL CHECK (length(evidence_id) BETWEEN 1 AND 128),
    summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 2000),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    UNIQUE (subagent_run_id, seq)
);

-- Observations are append-only evaluation records (M7-EVD-001 semantics,
-- scenario 39: direct UPDATE/DELETE must ABORT).
CREATE TRIGGER trg_sao_append_only BEFORE UPDATE ON subagent_observations
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_sao_nodelete BEFORE DELETE ON subagent_observations
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;