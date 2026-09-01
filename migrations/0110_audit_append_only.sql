-- 0110: audit_events becomes append-only at the storage layer.
-- Every other evidence table in this schema is already sealed this way
-- (trg_sao_*, trg_art_immutable_*, trg_evd_*, trg_cge_*). The audit log — the
-- record of what the assistant was permitted to do, and the only place a
-- surprising action can be reconstructed from afterwards — was the one that
-- any holder of the database handle could still rewrite or erase.
CREATE TRIGGER trg_audit_append_only BEFORE UPDATE ON audit_events
    BEGIN SELECT RAISE(ABORT, 'M10-AUD-001'); END;
CREATE TRIGGER trg_audit_nodelete BEFORE DELETE ON audit_events
    BEGIN SELECT RAISE(ABORT, 'M10-AUD-001'); END;
