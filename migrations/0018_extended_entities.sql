-- Extended entity tables per Lunitide unified design v1.0 §12.4–12.6.
-- These tables complement the existing simplified entities (plans, plan_nodes,
-- governance_reviews, memories) with version history, execution runs, tool
-- calls, approval decisions, memory provenance/revision, recall tracking, and
-- deletion tombstones. Existing tables are not modified; the new tables are
-- purely additive so the schema fingerprint evolves safely.

CREATE TABLE plan_versions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    version_no INTEGER NOT NULL CHECK (version_no > 0),
    graph_hash TEXT NOT NULL CHECK (length(graph_hash) = 64 AND graph_hash NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL,
    UNIQUE (plan_id, version_no)
);

CREATE INDEX ix_plan_versions_plan ON plan_versions(plan_id, version_no DESC);

CREATE TABLE plan_edges (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    plan_version_id TEXT NOT NULL REFERENCES plan_versions(id) ON DELETE CASCADE,
    from_node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,
    to_node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,
    condition_json TEXT NOT NULL DEFAULT '{}' CHECK (length(condition_json) BETWEEN 2 AND 8192),
    created_at TEXT NOT NULL,
    CHECK (from_node_id != to_node_id)
);

CREATE INDEX ix_plan_edges_version ON plan_edges(plan_version_id);

CREATE TABLE node_runs (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out')),
    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),
    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),
    started_at TEXT,
    ended_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE (node_id, attempt),
    CHECK (ended_at IS NULL OR started_at IS NOT NULL)
);

CREATE INDEX ix_node_runs_node ON node_runs(node_id, attempt DESC);
CREATE INDEX ix_node_runs_status ON node_runs(status);

CREATE TABLE node_run_checkpoints (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    node_run_id TEXT NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,
    state_ref TEXT NOT NULL CHECK (length(state_ref) BETWEEN 1 AND 512),
    external_effect_digest TEXT NOT NULL CHECK (length(external_effect_digest) = 64 AND external_effect_digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);

CREATE INDEX ix_node_run_checkpoints_run ON node_run_checkpoints(node_run_id, created_at DESC);

CREATE TABLE tool_calls (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    node_run_id TEXT NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,
    tool_id TEXT NOT NULL CHECK (length(tool_id) BETWEEN 1 AND 128),
    args_hash TEXT NOT NULL CHECK (length(args_hash) = 64 AND args_hash NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),
    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),
    risk TEXT NOT NULL DEFAULT 'low' CHECK (risk IN ('low', 'medium', 'high', 'critical')),
    approval_id TEXT REFERENCES governance_reviews(id),
    created_at TEXT NOT NULL
);

CREATE INDEX ix_tool_calls_run ON tool_calls(node_run_id);

CREATE TABLE approval_decisions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    review_id TEXT NOT NULL REFERENCES governance_reviews(id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),
    decided_at TEXT NOT NULL,
    UNIQUE (review_id)
);

CREATE INDEX ix_approval_decisions_review ON approval_decisions(review_id);

CREATE TABLE memory_sources (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL CHECK (length(source_type) BETWEEN 1 AND 64),
    source_id TEXT NOT NULL CHECK (length(source_id) BETWEEN 1 AND 256),
    source_revision TEXT NOT NULL DEFAULT '' CHECK (length(source_revision) <= 128),
    quote_ref TEXT CHECK (quote_ref IS NULL OR length(quote_ref) BETWEEN 1 AND 512),
    created_at TEXT NOT NULL
);

CREATE INDEX ix_memory_sources_memory ON memory_sources(memory_id);

CREATE TABLE memory_revisions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    old_ref TEXT CHECK (old_ref IS NULL OR length(old_ref) BETWEEN 1 AND 512),
    new_ref TEXT NOT NULL CHECK (length(new_ref) BETWEEN 1 AND 512),
    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 1024),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    created_at TEXT NOT NULL
);

CREATE INDEX ix_memory_revisions_memory ON memory_revisions(memory_id, created_at DESC);

CREATE TABLE recall_events (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    query_hash TEXT NOT NULL CHECK (length(query_hash) = 64 AND query_hash NOT GLOB '*[^0-9a-f]*'),
    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    score REAL NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
    rank INTEGER NOT NULL CHECK (rank > 0),
    injected_tokens INTEGER NOT NULL DEFAULT 0 CHECK (injected_tokens >= 0),
    created_at TEXT NOT NULL
);

CREATE INDEX ix_recall_events_session ON recall_events(session_id, created_at DESC);
CREATE INDEX ix_recall_events_memory ON recall_events(memory_id);

CREATE TABLE deletion_tombstones (
    owner_type TEXT NOT NULL CHECK (length(owner_type) BETWEEN 1 AND 64),
    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 64),
    deleted_at TEXT NOT NULL,
    propagation_status TEXT NOT NULL DEFAULT 'pending' CHECK (propagation_status IN ('pending', 'propagated', 'failed')),
    PRIMARY KEY (owner_type, owner_id)
);

CREATE INDEX ix_deletion_tombstones_status ON deletion_tombstones(propagation_status, deleted_at);
