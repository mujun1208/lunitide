-- Handoff imports: provenance-linked records binding an imported capsule
-- to a target session. Each row records that a capsule was imported into a
-- target session as untrusted prior context (ADR-005 §5). The UNIQUE
-- constraint on (capsule_id, target_session_id) makes repeat imports
-- idempotent: re-importing the same capsule into the same session is a
-- no-op rather than an error.
CREATE TABLE handoff_imports (
    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),
    capsule_id TEXT NOT NULL REFERENCES handoff_capsules(id) ON DELETE CASCADE,
    target_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    imported_at TEXT NOT NULL,
    UNIQUE (capsule_id, target_session_id)
);

CREATE INDEX ix_handoff_imports_target ON handoff_imports(target_session_id, imported_at DESC);
CREATE INDEX ix_handoff_imports_capsule ON handoff_imports(capsule_id);
