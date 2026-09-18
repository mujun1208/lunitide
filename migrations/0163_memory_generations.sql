-- Memory Dream / consolidation generations. One active generation per
-- subject+scope; recall is active members UNION post-cutoff overlay.
CREATE TABLE memory_generations (
    generation_id TEXT PRIMARY KEY CHECK (length(generation_id) = 26 AND substr(generation_id, 1, 1) GLOB '[0-7]' AND generation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    parent_generation_id TEXT CHECK (parent_generation_id IS NULL OR (length(parent_generation_id) = 26 AND substr(parent_generation_id, 1, 1) GLOB '[0-7]' AND parent_generation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),
    source_cutoff_seq INTEGER NOT NULL CHECK (source_cutoff_seq >= 0),
    state TEXT NOT NULL CHECK (state IN ('building','ready','active','discarded','failed','archived')),
    builder_version TEXT NOT NULL CHECK (length(builder_version) BETWEEN 1 AND 64),
    stats_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(stats_json) AND length(stats_json) <= 8192),
    activated_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE UNIQUE INDEX ux_memory_generations_active ON memory_generations(subject_id, scope_kind, scope_id) WHERE state = 'active';

CREATE TABLE memory_generation_heads (
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    active_generation_id TEXT NOT NULL REFERENCES memory_generations(generation_id),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    updated_at TEXT NOT NULL,
    PRIMARY KEY (subject_id, scope_kind, scope_id)
);

CREATE TABLE memory_generation_members (
    generation_id TEXT NOT NULL REFERENCES memory_generations(generation_id),
    fact_id TEXT NOT NULL,
    fact_version INTEGER NOT NULL,
    PRIMARY KEY (generation_id, fact_id, fact_version)
);

CREATE TABLE memory_consolidation_jobs (
    job_id TEXT PRIMARY KEY CHECK (length(job_id) = 26 AND substr(job_id, 1, 1) GLOB '[0-7]' AND job_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('user','workspace','project','expert','session')),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    generation_id TEXT NOT NULL REFERENCES memory_generations(generation_id),
    state TEXT NOT NULL CHECK (state IN ('queued','running','deferred','succeeded','failed','cancelled')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    next_attempt_at TEXT NOT NULL,
    cursor_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(cursor_json) AND length(cursor_json) <= 4096),
    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),
    lease_until TEXT,
    heartbeat_at TEXT,
    fence INTEGER NOT NULL DEFAULT 0 CHECK (fence >= 0),
    error_code TEXT NOT NULL DEFAULT '' CHECK (length(error_code) <= 64),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
