CREATE TABLE governance_reviews (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    plan_id TEXT REFERENCES plans(id),
    node_id TEXT REFERENCES plan_nodes(id),
    action_type TEXT NOT NULL CHECK (length(action_type) BETWEEN 1 AND 64),
    action_digest TEXT NOT NULL CHECK (length(action_digest) = 64 AND action_digest NOT GLOB '*[^0-9a-f]*'),
    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),
    state_digest TEXT NOT NULL CHECK (length(state_digest) = 64 AND state_digest NOT GLOB '*[^0-9a-f]*'),
    policy_version INTEGER NOT NULL CHECK (policy_version > 0),
    risk_level TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'changed_after_approval')),
    reviewer_note TEXT NOT NULL DEFAULT '' CHECK (length(reviewer_note) <= 4096),
    expires_at TEXT,
    created_at TEXT NOT NULL,
    reviewed_at TEXT
);

CREATE INDEX ix_governance_reviews_plan ON governance_reviews(plan_id, created_at DESC);
CREATE INDEX ix_governance_reviews_node ON governance_reviews(node_id);

CREATE TABLE governance_policies (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),
    version INTEGER NOT NULL CHECK (version > 0),
    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),
    rules_json TEXT NOT NULL CHECK (length(rules_json) BETWEEN 2 AND 65536),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);