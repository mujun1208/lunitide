-- 0090 Widen cc_audit_log.tool for OpenClaw-parity computer-control
-- operations (drag, window list/focus, UI snapshot, wait, clipboard).
-- SQLite cannot ALTER a CHECK constraint, so the table is rebuilt.
DROP VIEW IF EXISTS cc_recent_audit;
DROP TRIGGER IF EXISTS trg_ccaudit_no_update;
DROP TRIGGER IF EXISTS trg_ccaudit_no_delete;
ALTER TABLE cc_audit_log RENAME TO cc_audit_log_0090_old;
CREATE TABLE cc_audit_log (
    entry_id TEXT PRIMARY KEY CHECK (length(entry_id) BETWEEN 1 AND 64),
    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 64),
    tool TEXT NOT NULL CHECK (tool IN ('cc.mouse_move','cc.mouse_click','cc.mouse_drag','cc.keyboard_type','cc.keyboard_shortcut','cc.screen_capture','cc.get_active_window','cc.window_list','cc.window_focus','cc.observe_dialog','cc.confirm_dialog','cc.observe_ui','cc.wait','cc.clipboard')),
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 512),
    risk_level TEXT NOT NULL CHECK (risk_level IN ('low','medium','high','critical')),
    status TEXT NOT NULL CHECK (status IN ('executed','blocked','denied','failed','stopped')),
    layer TEXT NOT NULL DEFAULT '' CHECK (layer IN ('','intent','input-filter','process-monitor')),
    detail_json TEXT NOT NULL CHECK (length(detail_json) BETWEEN 2 AND 4096),
    created_at TEXT NOT NULL
);
INSERT INTO cc_audit_log SELECT * FROM cc_audit_log_0090_old;
DROP TABLE cc_audit_log_0090_old;
CREATE INDEX ix_ccaudit_session ON cc_audit_log(session_id, created_at DESC);
CREATE INDEX ix_ccaudit_status ON cc_audit_log(status, created_at DESC);
CREATE TRIGGER trg_ccaudit_no_update BEFORE UPDATE ON cc_audit_log
    BEGIN SELECT RAISE(ABORT, 'M10-CC-003'); END;
CREATE TRIGGER trg_ccaudit_no_delete BEFORE DELETE ON cc_audit_log
    BEGIN SELECT RAISE(ABORT, 'M10-CC-003'); END;
CREATE VIEW cc_recent_audit AS SELECT entry_id, session_id, tool, action, risk_level, status, layer, detail_json, created_at FROM cc_audit_log ORDER BY created_at DESC, entry_id DESC LIMIT 200;
