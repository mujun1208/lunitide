-- M6 T-6.3.1/T-6.3.3/T-6.3.4/T-6.4.2/T-6.4.4: cloud tasks, delegation
-- envelopes, budget ledger, join barriers, merge intents and the outbox.
-- Design deltas recorded in docs/evidence/m6-day0.txt:
--   - root_id references agent_run (design DDL said "root_run").
--   - m6_budget_account keeps the CHECK(granted = reserved + consumed +
--     refundable) invariant from the design; the ledger service guarantees
--     transitions stay inside it (BGT-001/002).
--   - m6_delegation stores the canonical envelope JSON plus its digest so
--     replay of the signature chain is possible after a crash.
--   - m6_cloud_task carries payload_digest so idempotency-key replays with
--     a different payload are rejected (TSK-001) instead of silently
--     returning the first result.

CREATE TABLE m6_cloud_task (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 256),
    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 64 AND payload_digest NOT GLOB '*[^0-9a-f]*'),
    lease_owner TEXT,
    lease_expires_at TEXT,
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    state TEXT NOT NULL CHECK (state IN ('created','queued','leased','running','joining',
                                          'succeeded','failed','cancelled')),
    result_ref TEXT,
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_m6_cloud_task_state ON m6_cloud_task(state, created_at);

CREATE TABLE m6_delegation (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    parent_id TEXT NOT NULL CHECK (length(parent_id) BETWEEN 1 AND 256),
    child_task_id TEXT REFERENCES m6_cloud_task(id),
    envelope TEXT NOT NULL CHECK (json_valid(envelope) AND length(envelope) BETWEEN 2 AND 262144),
    envelope_digest TEXT NOT NULL UNIQUE CHECK (length(envelope_digest) = 64 AND envelope_digest NOT GLOB '*[^0-9a-f]*'),
    nonce TEXT NOT NULL UNIQUE CHECK (length(nonce) BETWEEN 16 AND 128),
    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 4),
    state TEXT NOT NULL CHECK (state IN ('planned','grant_reserved','dispatched','arrived','settled',
                                          'rejected','expired')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    settled_at TEXT,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (root_id, nonce)
);
CREATE INDEX ix_delegation_root ON m6_delegation(root_id);

CREATE TABLE m6_budget_account (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    dimension TEXT NOT NULL CHECK (dimension IN ('cpu_seconds','tokens','cost','wall_clock')),
    granted INTEGER NOT NULL CHECK (granted >= 0),
    reserved INTEGER NOT NULL DEFAULT 0 CHECK (reserved >= 0),
    consumed INTEGER NOT NULL DEFAULT 0 CHECK (consumed >= 0),
    refundable INTEGER NOT NULL DEFAULT 0 CHECK (refundable >= 0),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    CHECK (granted = reserved + consumed + refundable),
    UNIQUE (root_id, dimension)
);

CREATE TABLE m6_barrier (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    policy TEXT NOT NULL CHECK (policy IN ('ALL','QUORUM','FAIL_FAST')),
    expected_children INTEGER NOT NULL CHECK (expected_children BETWEEN 1 AND 100),
    quorum INTEGER CHECK (quorum IS NULL OR (quorum BETWEEN 1 AND expected_children)),
    state TEXT NOT NULL CHECK (state IN ('open','closed')),
    closed_reason TEXT CHECK (closed_reason IS NULL OR length(closed_reason) BETWEEN 1 AND 128),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    closed_at TEXT,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)
);
CREATE INDEX ix_m6_barrier_root ON m6_barrier(root_id);

CREATE TABLE m6_barrier_arrival (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    barrier_id TEXT NOT NULL REFERENCES m6_barrier(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL CHECK (length(child_id) BETWEEN 1 AND 256),
    attempt INTEGER NOT NULL CHECK (attempt >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','failed','cancelled','expired')),
    result_digest TEXT NOT NULL CHECK (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*'),
    arrived_at TEXT NOT NULL,
    UNIQUE (barrier_id, child_id)
);

CREATE TABLE m6_merge_intent (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL CHECK (length(child_id) BETWEEN 1 AND 256),
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    expected_head TEXT NOT NULL CHECK (length(expected_head) BETWEEN 1 AND 256),
    current_head TEXT,
    patch_digest TEXT NOT NULL CHECK (length(patch_digest) = 64 AND patch_digest NOT GLOB '*[^0-9a-f]*'),
    tests_ref TEXT NOT NULL CHECK (length(tests_ref) BETWEEN 1 AND 512),
    state TEXT NOT NULL CHECK (state IN ('submitted','validating','queued','cas_check',
                                          'applying','merged','rejected','stale','rebase_required')),
    version INTEGER NOT NULL CHECK (version > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),
    UNIQUE (root_id, sequence)
);
CREATE INDEX ix_m6_merge_intent_root_state ON m6_merge_intent(root_id, state, sequence);

CREATE TABLE m6_outbox (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    aggregate_type TEXT NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 64),
    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 256),
    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 128),
    payload TEXT NOT NULL CHECK (json_valid(payload) AND length(payload) BETWEEN 2 AND 262144),
    published INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
    published_at TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_outbox_unpub ON m6_outbox(published, created_at);
