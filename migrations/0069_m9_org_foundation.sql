-- 0069 M9 slice 1 (T-9.1.1): the organization foundation - the immutable
-- org_id isolation root accepted by ADR-011. All M9 control-plane and
-- data-plane resources hang off organizations(org_id); the value is stamped
-- at insert time and may never be rebound (trg_*_org_immutable raise
-- M9-003 - rebinding an entity to another org IS a cross-org violation).
--
-- organizations: draft->active->suspended->closed state machine is enforced
-- in the service layer; suspended keeps Hold/audit and closed is NOT a
-- physical delete (ADR-011 "deactivation is not deletion").
-- principals carry an append-only identity_events chain whose latest
-- binding_version is the revocation watermark used to reject already-issued
-- capability tickets after a revoke (ADR-012).
--
-- House adaptations as in 0051-0068: TEXT RFC3339 timestamps, ULID CHECKs,
-- 64-hex digest CHECKs.

CREATE TABLE organizations (
    org_id TEXT PRIMARY KEY CHECK (length(org_id) = 26 AND substr(org_id, 1, 1) GLOB '[0-7]' AND org_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),
    state TEXT NOT NULL CHECK (state IN ('draft','active','suspended','closed')),
    retention_days INTEGER NOT NULL DEFAULT 730 CHECK (retention_days >= 90),
    residency_policy_digest TEXT CHECK (residency_policy_digest IS NULL OR (length(residency_policy_digest) = 64 AND residency_policy_digest NOT GLOB '*[^0-9a-f]*')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE team_spaces (
    space_id TEXT PRIMARY KEY CHECK (length(space_id) = 26 AND substr(space_id, 1, 1) GLOB '[0-7]' AND space_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    org_id TEXT NOT NULL REFERENCES organizations(org_id),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),
    state TEXT NOT NULL CHECK (state IN ('active','archived')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (org_id, name)
);

CREATE TABLE principals (
    principal_id TEXT PRIMARY KEY CHECK (length(principal_id) = 26 AND substr(principal_id, 1, 1) GLOB '[0-7]' AND principal_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    org_id TEXT NOT NULL REFERENCES organizations(org_id),
    external_id TEXT CHECK (external_id IS NULL OR length(external_id) BETWEEN 1 AND 256),
    idp_issuer TEXT CHECK (idp_issuer IS NULL OR length(idp_issuer) BETWEEN 8 AND 512),
    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 128),
    state TEXT NOT NULL CHECK (state IN ('active','suspended','expired','revoked')),
    binding_version INTEGER NOT NULL DEFAULT 1 CHECK (binding_version >= 1),
    expires_at TEXT,
    revoked_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (org_id, external_id)
);

CREATE TABLE role_bindings (
    binding_id TEXT PRIMARY KEY CHECK (length(binding_id) = 26 AND substr(binding_id, 1, 1) GLOB '[0-7]' AND binding_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    org_id TEXT NOT NULL REFERENCES organizations(org_id),
    principal_id TEXT NOT NULL REFERENCES principals(principal_id),
    scope_key TEXT NOT NULL CHECK (scope_key = 'org' OR length(scope_key) = 26),
    role TEXT NOT NULL CHECK (role IN ('org-admin','space-admin','operator','approver','auditor','legal-officer','member')),
    expires_at TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('active','revoked')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (org_id, principal_id, scope_key, role)
);

-- Append-only identity event chain: the latest event per principal pins the
-- binding_version consumed as the revocation watermark.
CREATE TABLE identity_events (
    event_id TEXT PRIMARY KEY CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    org_id TEXT NOT NULL REFERENCES organizations(org_id),
    principal_id TEXT NOT NULL REFERENCES principals(principal_id),
    kind TEXT NOT NULL CHECK (kind IN ('created','bound','rebound','suspended','restored','expired','revoked')),
    binding_version INTEGER NOT NULL CHECK (binding_version >= 1),
    created_at TEXT NOT NULL
);

-- org_id is the immutable isolation root: rebinding any row to a different
-- organization is a cross-org violation (design error code M9-003).
CREATE TRIGGER trg_org_org_immutable BEFORE UPDATE ON organizations
    WHEN NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;
CREATE TRIGGER trg_ts_org_immutable BEFORE UPDATE ON team_spaces
    WHEN NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;
CREATE TRIGGER trg_pr_org_immutable BEFORE UPDATE ON principals
    WHEN NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;
CREATE TRIGGER trg_rb_org_immutable BEFORE UPDATE ON role_bindings
    WHEN NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;
CREATE TRIGGER trg_ie_org_immutable BEFORE UPDATE ON identity_events
    WHEN NEW.org_id <> OLD.org_id
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;

-- Identity events are append-only evidence.
CREATE TRIGGER trg_ie_append_only BEFORE UPDATE ON identity_events
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;
CREATE TRIGGER trg_ie_nodelete BEFORE DELETE ON identity_events
    BEGIN SELECT RAISE(ABORT, 'M9-003'); END;

CREATE INDEX idx_ts_org ON team_spaces(org_id, state);
CREATE INDEX idx_pr_org ON principals(org_id, state);
CREATE INDEX idx_rb_scope ON role_bindings(org_id, principal_id, state);
CREATE INDEX idx_ie_principal ON identity_events(principal_id, binding_version);
