-- 0075 M10 wave 2 (T2-B): memory operations sidecars over the 0061 core —
-- growth box (observation window for freshly promoted facts), fact flags
-- (pinned/hidden + note) and per-subject memory settings. The immutable
-- memory_facts chain is never altered: growth/flag state lives in sidecar
-- rows keyed by fact_id. Audit reuses m6_outbox via execWithAudit; no new
-- audit ledger is created here.

CREATE TABLE memory_settings (
    subject_id TEXT PRIMARY KEY CHECK (length(subject_id) BETWEEN 1 AND 128),
    memory_enabled INTEGER NOT NULL DEFAULT 1 CHECK (memory_enabled IN (0,1)),
    auto_nominate INTEGER NOT NULL DEFAULT 0 CHECK (auto_nominate IN (0,1)),
    growth_days INTEGER NOT NULL DEFAULT 14 CHECK (growth_days BETWEEN 1 AND 90),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE memory_fact_flags (
    fact_id TEXT NOT NULL CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    flag TEXT NOT NULL CHECK (flag IN ('pinned','hidden')),
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (fact_id, flag)
);

CREATE INDEX idx_mff_flag ON memory_fact_flags(flag);

CREATE TABLE memory_growth_box (
    fact_id TEXT PRIMARY KEY CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),
    status TEXT NOT NULL DEFAULT 'observing' CHECK (status IN ('observing','promoted','dropped')),
    reference_count INTEGER NOT NULL DEFAULT 0 CHECK (reference_count >= 0),
    last_referenced_at TEXT,
    review_at TEXT NOT NULL,
    decided_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_mgb_status ON memory_growth_box(status, review_at);
CREATE INDEX idx_mgb_scope ON memory_growth_box(scope_id, status);
