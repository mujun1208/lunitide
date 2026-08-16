-- 0061 M8 slice 1 (T-8.1.x): the governed long-term memory core.
--
-- MemoryCandidate -> MemoryFact promotion is explicit-only: a one-time
-- confirmation token bound to candidate_id+payload_digest+actor+scope+expiry
-- is the sole promotion path; confidence/frequency/compaction never promote
-- (FR-02, M8-001/002/003). MemoryFact is an immutable version chain (no
-- in-place overwrite; supersede creates a new version). SourceLeaf rows bind
-- each fact version to leaf-level evidence with tamper-evident digests
-- (FR-03). RecallTrace rows store only the minimal explanation payload and
-- never write back into facts (FR-04). Audit reuses m6_outbox - no second
-- audit ledger is created here.
--
-- House adaptations as in 0051-0060: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE memory_candidates (
    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    payload TEXT NOT NULL CHECK (length(payload) BETWEEN 1 AND 65536),
    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 64 AND payload_digest NOT GLOB '*[^0-9a-f]*'),
    inferred INTEGER NOT NULL CHECK (inferred IN (0,1)),
    trust TEXT NOT NULL CHECK (trust IN ('untrusted','confirmed_source')),
    state TEXT NOT NULL CHECK (state IN ('pending','confirmed','rejected','expired')),
    confirm_token TEXT CHECK (confirm_token IS NULL OR (length(confirm_token) = 64 AND confirm_token NOT GLOB '*[^0-9a-f]*')),
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    confirmed_at TEXT
);

CREATE TABLE memory_facts (
    fact_id TEXT NOT NULL CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    version INTEGER NOT NULL CHECK (version >= 1),
    sensitivity TEXT NOT NULL CHECK (sensitivity IN ('public','private','sensitive')),
    state TEXT NOT NULL CHECK (state IN ('active','superseded','tombstoned')),
    superseded_by TEXT,
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    PRIMARY KEY (fact_id, version)
);

CREATE TABLE memory_source_leaves (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    json_pointer TEXT NOT NULL CHECK (length(json_pointer) BETWEEN 1 AND 512),
    evidence_ref TEXT NOT NULL CHECK (length(evidence_ref) BETWEEN 1 AND 512),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    UNIQUE (fact_id, fact_version, json_pointer),
    FOREIGN KEY (fact_id, fact_version) REFERENCES memory_facts(fact_id, version)
);

CREATE TABLE memory_recall_traces (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    query_digest TEXT NOT NULL CHECK (length(query_digest) = 64 AND query_digest NOT GLOB '*[^0-9a-f]*'),
    hits_json TEXT NOT NULL CHECK (length(hits_json) >= 2),
    reasons_json TEXT NOT NULL CHECK (length(reasons_json) >= 2),
    policy_redactions_json TEXT NOT NULL CHECK (length(policy_redactions_json) >= 2),
    created_at TEXT NOT NULL
);

CREATE INDEX idx_cand_subject ON memory_candidates(subject_id, state);
CREATE INDEX idx_fact_scope ON memory_facts(scope_id, state);
