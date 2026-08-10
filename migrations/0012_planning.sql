CREATE TABLE plans (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    stage_id TEXT,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'paused', 'completed', 'cancelled', 'failed')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX ix_plans_project_status ON plans(project_id, status);

CREATE TABLE plan_nodes (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    parent_node_id TEXT REFERENCES plan_nodes(id),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'running', 'paused', 'completed', 'failed', 'cancelled', 'blocked')),
    risk_level TEXT NOT NULL DEFAULT 'low' CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    budget_tokens INTEGER,
    estimate_tokens INTEGER,
    worker_role TEXT NOT NULL DEFAULT '' CHECK (length(worker_role) <= 128),
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (plan_id, sequence)
);

CREATE INDEX ix_plan_nodes_plan_sequence ON plan_nodes(plan_id, sequence);
CREATE INDEX ix_plan_nodes_status ON plan_nodes(plan_id, status);