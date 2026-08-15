-- 0052 M7 slice 2: evidence trace & quality gates (T-7.2.1).
-- Twelve append-only evidence tables + bidirectional trace indexes.
-- Every table rejects UPDATE/DELETE through M7-EVD-001 triggers (split into
-- separate update/delete triggers — modernc.org/sqlite cannot compile the
-- compound "UPDATE OR DELETE" form). Timestamps are TEXT RFC3339 (0051 style).

CREATE TABLE reviews (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_type TEXT NOT NULL CHECK (length(subject_type) BETWEEN 1 AND 64),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 64),
    subject_version INTEGER NOT NULL CHECK (subject_version >= 1),
    verdict TEXT NOT NULL CHECK (verdict IN ('approve','reject')),
    reviewer_id TEXT NOT NULL CHECK (length(reviewer_id) BETWEEN 1 AND 128),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2000),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_reviews_subject ON reviews(subject_type, subject_id, subject_version);

CREATE TABLE trace_edges (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    from_type TEXT NOT NULL CHECK (length(from_type) BETWEEN 1 AND 64),
    from_id TEXT NOT NULL CHECK (length(from_id) BETWEEN 1 AND 64),
    from_digest TEXT NOT NULL CHECK (length(from_digest) = 64 AND from_digest NOT GLOB '*[^0-9a-f]*'),
    relation TEXT NOT NULL CHECK (relation IN ('implements','verifies','traces_to','derived_from','reviews','produces','promotes')),
    to_type TEXT NOT NULL CHECK (length(to_type) BETWEEN 1 AND 64),
    to_id TEXT NOT NULL CHECK (length(to_id) BETWEEN 1 AND 64),
    to_digest TEXT NOT NULL CHECK (length(to_digest) = 64 AND to_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
CREATE INDEX idx_te_down ON trace_edges(from_type, from_id);
CREATE INDEX idx_te_up ON trace_edges(to_type, to_id);

CREATE TABLE stale_marks (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_type TEXT NOT NULL CHECK (length(subject_type) BETWEEN 1 AND 64),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 64),
    cause_edge TEXT NOT NULL CHECK (length(cause_edge) BETWEEN 1 AND 64),
    detected_at TEXT NOT NULL
);
CREATE INDEX ix_sm_subject ON stale_marks(subject_type, subject_id);

CREATE TABLE stale_resolutions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    stale_mark_id TEXT NOT NULL REFERENCES stale_marks(id),
    resolution_type TEXT NOT NULL CHECK (resolution_type IN ('recaptured','reevaluated','waived')),
    reevaluation_id TEXT CHECK (reevaluation_id IS NULL OR (length(reevaluation_id) = 26 AND reevaluation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    resolved_by TEXT NOT NULL CHECK (length(resolved_by) BETWEEN 1 AND 128),
    resolved_at TEXT NOT NULL
);
CREATE INDEX ix_sr_mark ON stale_resolutions(stale_mark_id);

CREATE TABLE gate_evaluations (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id),
    gate_key TEXT NOT NULL CHECK (length(gate_key) BETWEEN 1 AND 64),
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),
    decision TEXT NOT NULL CHECK (decision IN ('PASS','FAIL','BLOCKED')),
    findings_json TEXT NOT NULL CHECK (length(findings_json) >= 2),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_ge_run ON gate_evaluations(stage_run_id, gate_key, created_at);

CREATE TABLE checkpoints (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id),
    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 64 AND snapshot_digest NOT GLOB '*[^0-9a-f]*'),
    trace_root TEXT NOT NULL CHECK (length(trace_root) BETWEEN 1 AND 64),
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    created_at TEXT NOT NULL,
    UNIQUE (stage_run_id, sequence)
);

CREATE TABLE dev_tasks (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 512),
    state TEXT NOT NULL CHECK (state IN ('draft','ready','in_progress','blocked','in_review','done','reopened','cancelled')),
    priority TEXT NOT NULL CHECK (priority IN ('P0','P1','P2','P3')),
    risk TEXT NOT NULL CHECK (risk IN ('low','medium','high')),
    acceptance_digest TEXT NOT NULL CHECK (length(acceptance_digest) = 64 AND acceptance_digest NOT GLOB '*[^0-9a-f]*'),
    assignee_id TEXT CHECK (assignee_id IS NULL OR (length(assignee_id) BETWEEN 1 AND 128)),
    state_reason TEXT,
    block_reason TEXT,
    blocker_ref TEXT CHECK (blocker_ref IS NULL OR (length(blocker_ref) BETWEEN 1 AND 64)),
    lock_version INTEGER NOT NULL DEFAULT 1 CHECK (lock_version >= 1),
    trace_edge_id TEXT CHECK (trace_edge_id IS NULL OR (length(trace_edge_id) = 26 AND trace_edge_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_dt_run ON dev_tasks(stage_run_id, state);

CREATE TABLE test_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    task_ref TEXT NOT NULL CHECK (length(task_ref) BETWEEN 1 AND 64),
    result TEXT NOT NULL CHECK (result IN ('pass','fail','error','timeout')),
    report_digest TEXT NOT NULL CHECK (length(report_digest) = 64 AND report_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_tr_task ON test_runs(task_ref, created_at);

CREATE TABLE scan_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    task_ref TEXT NOT NULL CHECK (length(task_ref) BETWEEN 1 AND 64),
    scanner TEXT NOT NULL CHECK (length(scanner) BETWEEN 1 AND 128),
    severity_gate TEXT NOT NULL CHECK (length(severity_gate) BETWEEN 1 AND 32),
    report_digest TEXT NOT NULL CHECK (length(report_digest) = 64 AND report_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_scan_task ON scan_runs(task_ref, created_at);

CREATE TABLE artifact_derivations (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    derived_from_version TEXT NOT NULL CHECK (length(derived_from_version) = 26 AND derived_from_version NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    relation TEXT NOT NULL CHECK (relation IN ('derived_from','rebuilt_from','supersedes')),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_ad_artifact ON artifact_derivations(artifact_version_id);

CREATE TABLE reproduction_manifests (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    artifact_version_id TEXT NOT NULL REFERENCES artifact_versions(id),
    manifest_json TEXT NOT NULL CHECK (length(manifest_json) >= 2),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_rm_artifact ON reproduction_manifests(artifact_version_id);

CREATE TABLE evaluation_baselines (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    scope_type TEXT NOT NULL CHECK (length(scope_type) BETWEEN 1 AND 64),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 64),
    baseline_json TEXT NOT NULL CHECK (length(baseline_json) >= 2),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_eb_scope ON evaluation_baselines(scope_type, scope_id);

-- Append-only guards: one UPDATE and one DELETE trigger per evidence table
-- (M7-EVD-001). dev_tasks is the workflow-side exception: it is the twelfth
-- table of this migration but carries a state machine with optimistic
-- locking (lock_version), so its state transitions are legal updates.

CREATE TRIGGER trg_evd_reviews_ro_u BEFORE UPDATE ON reviews
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_reviews_ro_d BEFORE DELETE ON reviews
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_te_ro_u BEFORE UPDATE ON trace_edges
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_te_ro_d BEFORE DELETE ON trace_edges
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_sm_ro_u BEFORE UPDATE ON stale_marks
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_sm_ro_d BEFORE DELETE ON stale_marks
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_sr_ro_u BEFORE UPDATE ON stale_resolutions
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_sr_ro_d BEFORE DELETE ON stale_resolutions
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_ge_ro_u BEFORE UPDATE ON gate_evaluations
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_ge_ro_d BEFORE DELETE ON gate_evaluations
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_ck_ro_u BEFORE UPDATE ON checkpoints
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_ck_ro_d BEFORE DELETE ON checkpoints
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_tr_ro_u BEFORE UPDATE ON test_runs
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_tr_ro_d BEFORE DELETE ON test_runs
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_sc_ro_u BEFORE UPDATE ON scan_runs
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_sc_ro_d BEFORE DELETE ON scan_runs
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_ad_ro_u BEFORE UPDATE ON artifact_derivations
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_ad_ro_d BEFORE DELETE ON artifact_derivations
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_rm_ro_u BEFORE UPDATE ON reproduction_manifests
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_rm_ro_d BEFORE DELETE ON reproduction_manifests
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TRIGGER trg_evd_eb_ro_u BEFORE UPDATE ON evaluation_baselines
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_evd_eb_ro_d BEFORE DELETE ON evaluation_baselines
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
