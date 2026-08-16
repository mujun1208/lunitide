-- 0057 M7 slice 5 (T-7.5.1/T-7.5.2): the AppUpdate split-track domain and
-- the M7 audit hash chain.
--
-- AppUpdate is a physically isolated domain (02-技术设计 §05): no foreign
-- key crosses into the project-release tables and no project release ID is
-- accepted anywhere in this schema. Six entities per the design DDL
-- ((N+4)_m7_appupdate_split): update_channels (seeded stable/beta),
-- update_packages, update_installations, update_receipts,
-- update_rollback_attempts and consumed_nonces.
--
-- House adaptations (same as 0051/0056): TEXT RFC3339 timestamps, ULID
-- CHECKs on id columns and 64-hex digest CHECKs. update_receipts are
-- append-only installation evidence (M7-EVD-001 family); update rollback
-- attempts mirror the release-domain no-delete rule (M7-RBK-002 semantics).
--
-- m7_audit_events is the M7 audit outbox (T-7.5.2): an append-only WORM
-- ledger where every row chains seq + prev_hash + event_hash (computed over
-- the canonical event document). UPDATE/DELETE trip M7-DR-001; a detected
-- chain break freezes production promotions with the same code at the
-- bridge layer.

CREATE TABLE update_channels (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL UNIQUE CHECK (name IN ('stable','beta')),
    state TEXT NOT NULL CHECK (state IN ('active','retired')),
    created_at TEXT NOT NULL
);

-- Fixed channel identities so the split-track seeds are deterministic
-- across installs (stable=…5FAV, beta=…5FAW).
INSERT INTO update_channels (id, name, state, created_at) VALUES
    ('01ARZ3NDEKTSV4RRFFQ69G5FAV', 'stable', 'active', '2026-01-01T00:00:00Z'),
    ('01ARZ3NDEKTSV4RRFFQ69G5FAW', 'beta', 'active', '2026-01-01T00:00:00Z');

CREATE TABLE update_packages (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    channel_id TEXT NOT NULL REFERENCES update_channels(id),
    app_version TEXT NOT NULL CHECK (length(app_version) BETWEEN 1 AND 32),
    min_version TEXT NOT NULL CHECK (length(min_version) BETWEEN 1 AND 32),
    package_digest TEXT NOT NULL CHECK (length(package_digest) = 64 AND package_digest NOT GLOB '*[^0-9a-f]*'),
    signature TEXT NOT NULL CHECK (length(signature) BETWEEN 1 AND 512),
    nonce TEXT NOT NULL CHECK (length(nonce) BETWEEN 1 AND 128),
    not_before TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    key_id TEXT NOT NULL CHECK (length(key_id) BETWEEN 1 AND 64),
    state TEXT NOT NULL CHECK (state IN ('building','published','revoked')),
    created_at TEXT NOT NULL,
    UNIQUE (channel_id, app_version)
);
CREATE INDEX ix_updp_channel_state ON update_packages(channel_id, state);

CREATE TABLE update_installations (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    package_id TEXT NOT NULL REFERENCES update_packages(id),
    device_id TEXT NOT NULL CHECK (length(device_id) BETWEEN 1 AND 128),
    state TEXT NOT NULL CHECK (state IN ('pending','downloading','installing','succeeded','failed','rolled_back')),
    created_at TEXT NOT NULL,
    completed_at TEXT
);
CREATE INDEX ix_updi_device ON update_installations(device_id, state);
CREATE INDEX ix_updi_package ON update_installations(package_id);

CREATE TABLE update_receipts (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    installation_id TEXT NOT NULL REFERENCES update_installations(id),
    receipt_json TEXT NOT NULL CHECK (length(receipt_json) >= 2),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);
CREATE INDEX ix_updr_installation ON update_receipts(installation_id);

-- Installation receipts are append-only evidence (M7-EVD-001 family).
CREATE TRIGGER trg_updr_immutable_u BEFORE UPDATE ON update_receipts
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;
CREATE TRIGGER trg_updr_nodelete BEFORE DELETE ON update_receipts
    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END;

CREATE TABLE update_rollback_attempts (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    installation_id TEXT NOT NULL REFERENCES update_installations(id),
    state TEXT NOT NULL CHECK (state IN ('pending','running','succeeded','failed')),
    operator_id TEXT NOT NULL CHECK (length(operator_id) BETWEEN 1 AND 128),
    result_json TEXT NOT NULL CHECK (length(result_json) >= 2),
    created_at TEXT NOT NULL,
    completed_at TEXT
);
CREATE INDEX ix_upra_installation ON update_rollback_attempts(installation_id);

-- Update rollback attempts are append-only (M7-RBK-002 semantics).
CREATE TRIGGER trg_upra_nodelete BEFORE DELETE ON update_rollback_attempts
    BEGIN SELECT RAISE(ABORT, 'M7-RBK-002'); END;

-- Nonce single-consumption: one install per package nonce, ever.
CREATE TABLE consumed_nonces (
    nonce TEXT PRIMARY KEY CHECK (length(nonce) BETWEEN 1 AND 128),
    consumed_at TEXT NOT NULL
);

-- M7 audit outbox (T-7.5.2): append-only WORM hash chain. event_hash is the
-- SHA-256 over the canonical event document including prev_hash; seq is
-- strictly increasing and gap-free so tampering is detectable.
CREATE TABLE m7_audit_events (
    seq INTEGER PRIMARY KEY CHECK (seq >= 1),
    id TEXT NOT NULL UNIQUE CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 128),
    resource_type TEXT NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 64),
    resource_id TEXT NOT NULL CHECK (length(resource_id) BETWEEN 1 AND 128),
    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),
    before_digest TEXT CHECK (before_digest IS NULL OR (length(before_digest) = 64 AND before_digest NOT GLOB '*[^0-9a-f]*')),
    after_digest TEXT CHECK (after_digest IS NULL OR (length(after_digest) = 64 AND after_digest NOT GLOB '*[^0-9a-f]*')),
    correlation_id TEXT CHECK (correlation_id IS NULL OR length(correlation_id) BETWEEN 1 AND 128),
    prev_hash TEXT NOT NULL CHECK (length(prev_hash) = 64 AND prev_hash NOT GLOB '*[^0-9a-f]*'),
    event_hash TEXT NOT NULL UNIQUE CHECK (length(event_hash) = 64 AND event_hash NOT GLOB '*[^0-9a-f]*'),
    created_at TEXT NOT NULL
);

-- WORM: the ledger is write-once (M7-DR-001).
CREATE TRIGGER trg_m7ae_immutable_u BEFORE UPDATE ON m7_audit_events
    BEGIN SELECT RAISE(ABORT, 'M7-DR-001'); END;
CREATE TRIGGER trg_m7ae_nodelete BEFORE DELETE ON m7_audit_events
    BEGIN SELECT RAISE(ABORT, 'M7-DR-001'); END;