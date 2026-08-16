-- 0066 M8 FR-17 (T-8.8.x): the write-collaboration evaluation gate. The
-- orchestrated multi-agent write capability stays DISABLED through all of
-- M8; these tables only carry the evaluation/decision runtime.
--
-- Evaluation snapshots are append-only WORM (UPDATE/DELETE trips M8-034):
-- computing -> insufficient_evidence|pass|fail, idempotent on
-- (subject_id, window_start, window_end, criteria_version). Decisions are
-- one-time-token, single-use, and only a pass evaluation may yield an enable
-- decision; disable decisions may be produced at any time. Decision writes
-- commit in the same transaction as the m6_outbox audit event.
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE collab_gate_evaluations (
    evaluation_id TEXT PRIMARY KEY CHECK (length(evaluation_id) = 26 AND substr(evaluation_id, 1, 1) GLOB '[0-7]' AND evaluation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    window_start INTEGER NOT NULL CHECK (window_start >= 0),
    window_end INTEGER NOT NULL CHECK (window_end > window_start),
    evidence_json TEXT NOT NULL CHECK (length(evidence_json) >= 2),
    evidence_digest TEXT NOT NULL CHECK (length(evidence_digest) = 64 AND evidence_digest NOT GLOB '*[^0-9a-f]*'),
    criteria_version TEXT NOT NULL CHECK (length(criteria_version) BETWEEN 1 AND 64),
    outcome TEXT NOT NULL CHECK (outcome IN ('computing','insufficient_evidence','pass','fail')),
    failed_criteria_json TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (subject_id, window_start, window_end, criteria_version)
);

CREATE TABLE collab_gate_decisions (
    decision_id TEXT PRIMARY KEY CHECK (length(decision_id) = 26 AND substr(decision_id, 1, 1) GLOB '[0-7]' AND decision_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    evaluation_id TEXT NOT NULL REFERENCES collab_gate_evaluations(evaluation_id),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    decision_token TEXT NOT NULL UNIQUE CHECK (length(decision_token) = 64 AND decision_token NOT GLOB '*[^0-9a-f]*'),
    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),
    capability_digest TEXT NOT NULL CHECK (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*'),
    action TEXT NOT NULL CHECK (action IN ('enable','disable')),
    state TEXT NOT NULL CHECK (state IN ('pending','confirmed','expired','revoked')),
    confirmed_at TEXT,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX idx_cge_subject ON collab_gate_evaluations(subject_id, outcome);
CREATE INDEX idx_cgd_subject ON collab_gate_decisions(subject_id, state);

-- Evaluation snapshots are append-only (design DDL: UPDATE/DELETE -> M8-034).
CREATE TRIGGER trg_cge_append_only BEFORE UPDATE ON collab_gate_evaluations
    BEGIN SELECT RAISE(ABORT, 'M8-034'); END;
CREATE TRIGGER trg_cge_nodelete BEFORE DELETE ON collab_gate_evaluations
    BEGIN SELECT RAISE(ABORT, 'M8-034'); END;
