-- 0065 M8 slice: learning candidates (T-8.7.x). Eligibility snapshots bind
-- artifact/version/digest, review and gate evidence, scope, classification,
-- license evidence and policy version; any change stales the snapshot.
-- Skill/Workflow candidates default to quarantined and are NOT executable;
-- state transitions commit in the same transaction as the m6_outbox audit
-- event. CandidateEvaluationBinding reuses the M7 evaluation baseline/run
-- contract. FeedbackEvents only ever form new evidence - they never rewrite
-- history in place.
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE eligibility_snapshots (
    snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) = 26 AND substr(snapshot_id, 1, 1) GLOB '[0-7]' AND snapshot_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    artifact_version_id TEXT NOT NULL CHECK (length(artifact_version_id) BETWEEN 1 AND 128),
    artifact_digest TEXT NOT NULL CHECK (length(artifact_digest) = 64 AND artifact_digest NOT GLOB '*[^0-9a-f]*'),
    review_digest TEXT NOT NULL CHECK (length(review_digest) = 64 AND review_digest NOT GLOB '*[^0-9a-f]*'),
    gate_digest TEXT NOT NULL CHECK (length(gate_digest) = 64 AND gate_digest NOT GLOB '*[^0-9a-f]*'),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    classification TEXT NOT NULL CHECK (length(classification) BETWEEN 1 AND 64),
    license_evidence TEXT NOT NULL CHECK (length(license_evidence) >= 2),
    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),
    expiry_at TEXT NOT NULL,
    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 64 AND snapshot_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('valid','stale','revoked','expired')),
    created_at TEXT NOT NULL
);

CREATE TABLE skill_candidates (
    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    snapshot_id TEXT NOT NULL REFERENCES eligibility_snapshots(snapshot_id),
    goal TEXT NOT NULL CHECK (length(goal) BETWEEN 1 AND 2000),
    input_schema TEXT NOT NULL CHECK (length(input_schema) >= 2),
    output_schema TEXT NOT NULL CHECK (length(output_schema) >= 2),
    minimal_permissions TEXT NOT NULL CHECK (length(minimal_permissions) >= 2),
    trigger_condition TEXT NOT NULL CHECK (length(trigger_condition) >= 2),
    evidence_json TEXT NOT NULL CHECK (length(evidence_json) >= 2),
    evaluation_set TEXT NOT NULL CHECK (length(evaluation_set) >= 2),
    rollback_version TEXT,
    state TEXT NOT NULL CHECK (state IN ('evaluating','quarantined','approved','rejected','superseded')),
    created_at TEXT NOT NULL
);

CREATE TABLE workflow_candidates (
    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    snapshot_id TEXT NOT NULL REFERENCES eligibility_snapshots(snapshot_id),
    definition_digest TEXT NOT NULL CHECK (length(definition_digest) = 64 AND definition_digest NOT GLOB '*[^0-9a-f]*'),
    permissions TEXT NOT NULL CHECK (length(permissions) >= 2),
    rollback_ref TEXT,
    state TEXT NOT NULL CHECK (state IN ('evaluating','quarantined','approved','rejected','superseded')),
    created_at TEXT NOT NULL
);

CREATE TABLE candidate_evaluation_bindings (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    candidate_type TEXT NOT NULL CHECK (candidate_type IN ('skill','workflow')),
    candidate_id TEXT NOT NULL CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    baseline_ref TEXT NOT NULL CHECK (length(baseline_ref) BETWEEN 1 AND 128),
    environment_digest TEXT NOT NULL CHECK (length(environment_digest) = 64 AND environment_digest NOT GLOB '*[^0-9a-f]*'),
    attestation_digest TEXT NOT NULL CHECK (length(attestation_digest) = 64 AND attestation_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);

CREATE TABLE feedback_events (
    event_id TEXT PRIMARY KEY CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    action TEXT NOT NULL CHECK (action IN ('correct','accept','reject','defer','run_result')),
    target_type TEXT NOT NULL CHECK (length(target_type) BETWEEN 1 AND 64),
    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 128),
    evidence TEXT NOT NULL CHECK (length(evidence) >= 2),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_es_subject ON eligibility_snapshots(subject_id, state);
CREATE INDEX idx_sc_state ON skill_candidates(state);
CREATE INDEX idx_wc_state ON workflow_candidates(state);
