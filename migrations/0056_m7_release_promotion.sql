-- 0056 M7 slice 3/4 (T-7.3.1): CR revisions, immutable release packages,
-- content-addressed blobs and the promotion-saga tables (promotions,
-- migration_executions, deployments, rollback_attempts). Slice 3 uses
-- cr_revisions / release_packages / release_blobs; slice 4 consumes the
-- saga tables already created here so no second migration is needed.
--
-- House adaptations of the design DDL (02-技术设计 (N+3)): TEXT RFC3339
-- timestamps (0051 style), ULID CHECKs on id columns, 64-hex digest CHECKs
-- and split UPDATE/DELETE triggers (modernc.org/sqlite cannot compile the
-- compound "UPDATE OR DELETE" form). cr_revisions.cr_id is a free-form
-- business key (1..128): the first revision created for a legacy CR id is
-- revision_no 1 - the "old CR converts to revision 1" import rule; this
-- codebase has no pre-M7 CR table, so there is nothing to backfill and no
-- legacy row is ever marked legacy_unverified. release_blobs is the
-- content-addressed store for sealed package documents (digest = sha256 of
-- content, enforced by the service; UPDATE/DELETE trip M7-PKG-001).

CREATE TABLE cr_revisions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    cr_id TEXT NOT NULL CHECK (length(cr_id) BETWEEN 1 AND 128),
    revision_no INTEGER NOT NULL CHECK (revision_no >= 1),
    manifest_json TEXT NOT NULL CHECK (length(manifest_json) >= 2),
    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    status TEXT NOT NULL CHECK (status IN ('draft','submitted','approved','rejected','superseded')),
    created_at TEXT NOT NULL,
    UNIQUE (cr_id, revision_no)
);
CREATE INDEX ix_crr_cr ON cr_revisions(cr_id, revision_no);

-- REV-002 (database layer): once a revision leaves draft, its frozen fields
-- (cr_id, revision_no, manifest_json, digest) can never change; only the
-- status column may still move within the canonical state machine.
CREATE TRIGGER trg_crr_frozen BEFORE UPDATE ON cr_revisions
    WHEN OLD.status <> 'draft' AND (NEW.cr_id <> OLD.cr_id OR NEW.revision_no <> OLD.revision_no
        OR NEW.manifest_json <> OLD.manifest_json OR NEW.digest <> OLD.digest)
    BEGIN SELECT RAISE(ABORT, 'M7-REV-002'); END;
CREATE TRIGGER trg_crr_nodelete BEFORE DELETE ON cr_revisions
    WHEN OLD.status <> 'draft'
    BEGIN SELECT RAISE(ABORT, 'M7-REV-002'); END;

-- Content-addressed blob store for sealed package documents. The service
-- guarantees digest = sha256(content); rows are immutable (M7-PKG-001).
CREATE TABLE release_blobs (
    digest TEXT PRIMARY KEY CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),
    content TEXT NOT NULL CHECK (length(content) >= 2),
    created_at TEXT NOT NULL
);
CREATE TRIGGER trg_rb_immutable_u BEFORE UPDATE ON release_blobs
    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END;
CREATE TRIGGER trg_rb_immutable_d BEFORE DELETE ON release_blobs
    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END;

CREATE TABLE release_packages (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    cr_revision_id TEXT NOT NULL REFERENCES cr_revisions(id),
    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),
    blob_digest TEXT NOT NULL CHECK (length(blob_digest) = 64 AND blob_digest NOT GLOB '*[^0-9a-f]*'),
    signature TEXT NOT NULL CHECK (length(signature) BETWEEN 1 AND 512),
    state TEXT NOT NULL CHECK (state IN ('building','sealed','failed')),
    created_at TEXT NOT NULL,
    sealed_at TEXT
);
CREATE INDEX ix_rp_revision ON release_packages(cr_revision_id);

-- PKG-001 (database layer): sealed packages can never be updated or deleted.
CREATE TRIGGER trg_pkg_sealed_ro_u BEFORE UPDATE ON release_packages
    WHEN OLD.state = 'sealed'
    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END;
CREATE TRIGGER trg_pkg_sealed_ro_d BEFORE DELETE ON release_packages
    WHEN OLD.state = 'sealed'
    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END;

-- Promotion saga tables (consumed by slice 4; created here per the design
-- DDL so slice 4 needs no further migration).

CREATE TABLE promotions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    package_id TEXT NOT NULL REFERENCES release_packages(id),
    from_env TEXT NOT NULL CHECK (length(from_env) BETWEEN 1 AND 32),
    to_env TEXT NOT NULL CHECK (to_env IN ('dev','stage','prod')),
    canonical_intent_digest TEXT NOT NULL CHECK (length(canonical_intent_digest) = 64 AND canonical_intent_digest NOT GLOB '*[^0-9a-f]*'),
    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 64),
    approval_expiry TEXT,
    state TEXT NOT NULL CHECK (state IN ('requested','policy_check','approval_check','denied','expired','migrating','deploying','validating','succeeded','failed','rolling_back','rolled_back','rollback_failed','outcome_unknown','manual')),
    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    requested_by TEXT NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 128),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX ix_prm_package ON promotions(package_id);
CREATE INDEX ix_prm_state ON promotions(state);

CREATE TABLE migration_executions (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    promotion_id TEXT NOT NULL REFERENCES promotions(id),
    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64 AND plan_digest NOT GLOB '*[^0-9a-f]*'),
    state TEXT NOT NULL CHECK (state IN ('planned','applied','verified','failed')),
    rollback_ref TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX ix_me_promotion ON migration_executions(promotion_id);

CREATE TABLE deployments (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    promotion_id TEXT NOT NULL REFERENCES promotions(id),
    target_env TEXT NOT NULL CHECK (target_env IN ('dev','stage','prod')),
    state TEXT NOT NULL CHECK (state IN ('pending','running','succeeded','failed','outcome_unknown')),
    started_at TEXT,
    completed_at TEXT,
    receipt_json TEXT
);
CREATE INDEX ix_dep_promotion ON deployments(promotion_id);

CREATE TABLE rollback_attempts (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    promotion_id TEXT NOT NULL REFERENCES promotions(id),
    dimension TEXT NOT NULL CHECK (dimension IN ('binary','schema','data','external')),
    state TEXT NOT NULL CHECK (state IN ('pending','running','succeeded','failed')),
    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64 AND plan_digest NOT GLOB '*[^0-9a-f]*'),
    operator_id TEXT NOT NULL CHECK (length(operator_id) BETWEEN 1 AND 128),
    result_json TEXT NOT NULL CHECK (length(result_json) >= 2),
    created_at TEXT NOT NULL,
    completed_at TEXT
);
CREATE INDEX ix_rba_promotion ON rollback_attempts(promotion_id);

-- RBK-002: rollback attempts are append-only evidence (never deleted).
CREATE TRIGGER trg_rba_nodelete BEFORE DELETE ON rollback_attempts
    BEGIN SELECT RAISE(ABORT, 'M7-RBK-002'); END;