package toolruntime

import (
	"database/sql"
	"path/filepath"
)

func (r *Runtime) ensureAudit() error {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	if r.db != nil {
		return nil
	}
	db, err := sql.Open("sqlite", filepath.Join(r.root, ".tool-runtime.sqlite"))
	if err != nil {
		return err
	}
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS chat_tool_calls(
		id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL, call_id TEXT NOT NULL,
		tool_name TEXT NOT NULL, args_json BLOB NOT NULL, args_digest TEXT NOT NULL, execution_mode TEXT NOT NULL,
		workspace_digest TEXT NOT NULL, decision TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
		result_digest TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
		decided_at TEXT NOT NULL DEFAULT '', completed_at TEXT NOT NULL DEFAULT '', expires_at TEXT NOT NULL,
		UNIQUE(session_id, call_id, args_digest));
		CREATE TABLE IF NOT EXISTS chat_tool_approval_rules(
			id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, tool_name TEXT NOT NULL,
			args_digest TEXT NOT NULL, scope TEXT NOT NULL, created_at TEXT NOT NULL,
			UNIQUE(session_id, tool_name, args_digest));
		CREATE TABLE IF NOT EXISTS chat_tool_hook_events(
		id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, tool_name TEXT NOT NULL,
		hook_id TEXT NOT NULL, event TEXT NOT NULL, decision TEXT NOT NULL DEFAULT '',
		args_digest TEXT NOT NULL DEFAULT '', result_digest TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL);
	CREATE INDEX IF NOT EXISTS ix_chat_tool_calls_status ON chat_tool_calls(status, expires_at);
	CREATE INDEX IF NOT EXISTS ix_tool_approval_rules_match ON chat_tool_approval_rules(tool_name, args_digest, scope);
	CREATE INDEX IF NOT EXISTS ix_chat_tool_hook_events_session ON chat_tool_hook_events(session_id, id);`); err != nil {
		db.Close()
		return err
	}
	r.db = db
	return nil
}
