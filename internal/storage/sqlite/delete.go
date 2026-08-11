package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecordTombstone writes a deletion tombstone for the given owner. Tombstones
// are used for fail-closed readability checks: after a session or checkpoint is
// deleted, derived objects (e.g. handoff capsules imported into other sessions)
// can distinguish "source was deleted" from "source never existed" and return
// an explicit error instead of silently disappearing.
func (s *Store) RecordTombstone(ctx context.Context, ownerType, ownerID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO deletion_tombstones(owner_type, owner_id, deleted_at, propagation_status)
		 VALUES(?,?,?,'pending')`,
		ownerType, ownerID, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record tombstone %s:%s: %w", ownerType, ownerID, err)
	}
	return nil
}

// HasTombstone returns true if a deletion tombstone exists for the given owner.
func (s *Store) HasTombstone(ctx context.Context, ownerType, ownerID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM deletion_tombstones WHERE owner_type=? AND owner_id=?`,
		ownerType, ownerID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check tombstone %s:%s: %w", ownerType, ownerID, err)
	}
	return count > 0, nil
}

// recordTombstonesForSessionTx writes tombstones for the session itself, all
// its compaction checkpoints, and all its handoff capsules. Must be called
// within the delete transaction before the rows are physically removed.
func recordTombstonesForSessionTx(ctx context.Context, tx *sql.Tx, now, sessionID string) error {
	// Session tombstone
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('session',?,?, 'pending')`, sessionID, now); err != nil {
		return fmt.Errorf("record session tombstone: %w", err)
	}
	// Checkpoint tombstones
	cpRows, err := tx.QueryContext(ctx, `SELECT id FROM compaction_checkpoints WHERE session_id=?`, sessionID)
	if err != nil {
		return fmt.Errorf("list checkpoints for tombstone: %w", err)
	}
	var checkpointIDs []string
	for cpRows.Next() {
		var id string
		if err := cpRows.Scan(&id); err != nil {
			cpRows.Close()
			return fmt.Errorf("scan checkpoint id: %w", err)
		}
		checkpointIDs = append(checkpointIDs, id)
	}
	cpRows.Close()
	if err := cpRows.Err(); err != nil {
		return fmt.Errorf("iterate checkpoints: %w", err)
	}
	for _, id := range checkpointIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('checkpoint',?,?,'pending')`, id, now); err != nil {
			return fmt.Errorf("record checkpoint tombstone: %w", err)
		}
	}
	// Capsule tombstones (source or dest session)
	capRows, err := tx.QueryContext(ctx, `SELECT id FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?`, sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("list capsules for tombstone: %w", err)
	}
	var capsuleIDs []string
	for capRows.Next() {
		var id string
		if err := capRows.Scan(&id); err != nil {
			capRows.Close()
			return fmt.Errorf("scan capsule id: %w", err)
		}
		capsuleIDs = append(capsuleIDs, id)
	}
	capRows.Close()
	if err := capRows.Err(); err != nil {
		return fmt.Errorf("iterate capsules: %w", err)
	}
	for _, id := range capsuleIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('capsule',?,?,'pending')`, id, now); err != nil {
			return fmt.Errorf("record capsule tombstone: %w", err)
		}
	}
	return nil
}

// DeleteSession deletes a session and all its dependent records (messages,
// message_parts, message_session_state, compaction_checkpoints, token_ledger)
// in a single transaction. If the session does not exist, the operation is
// a no-op (idempotent delete). Tombstones are recorded for fail-closed
// readability checks before physical deletion.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete session tx: %w", err)
	}
	defer tx.Rollback()

	// Idempotent: if session doesn't exist, return success.
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, id).Scan(&count); err != nil {
		return fmt.Errorf("check session exists: %w", err)
	}
	if count == 0 {
		return tx.Commit() // no-op, idempotent
	}

	// Record tombstones before physical deletion for fail-closed readability.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := recordTombstonesForSessionTx(ctx, tx, now, id); err != nil {
		return err
	}

	// Delete in FK-respecting order (child-first):
	// 1. handoff_imports (FK to handoff_capsules)
	if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_imports WHERE capsule_id IN (SELECT id FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?)`, id, id); err != nil {
		return fmt.Errorf("delete handoff_imports: %w", err)
	}
	// 2. handoff_capsules (FK to sessions, compaction_checkpoints)
	if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?`, id, id); err != nil {
		return fmt.Errorf("delete handoff_capsules: %w", err)
	}
	// 3. compaction_checkpoints (FK to sessions, messages; ON DELETE RESTRICT)
	if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete compaction_checkpoints: %w", err)
	}
	// 4. message_parts (FK to messages)
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_parts WHERE message_id IN (SELECT id FROM messages WHERE session_id=?)`, id); err != nil {
		return fmt.Errorf("delete message_parts: %w", err)
	}
	// 5. token_ledger (FK to messages)
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_ledger WHERE message_id IN (SELECT id FROM messages WHERE session_id=?)`, id); err != nil {
		return fmt.Errorf("delete token_ledger: %w", err)
	}
	// 6. messages
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	// 7. message_session_state (FK to sessions)
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_session_state WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete message_session_state: %w", err)
	}
	// 8. session
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return tx.Commit()
}

// DeleteProject deletes a project and all its dependent records (sessions,
// messages, message_parts, etc.) in a single transaction. If the project
// does not exist, the operation is a no-op (idempotent delete). Tombstones
// are recorded for all sessions, checkpoints, and capsules before deletion.
func (s *Store) DeleteProject(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete project tx: %w", err)
	}
	defer tx.Rollback()

	// Idempotent: if project doesn't exist, return success.
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=?`, id).Scan(&count); err != nil {
		return fmt.Errorf("check project exists: %w", err)
	}
	if count == 0 {
		return tx.Commit() // no-op, idempotent
	}

	// Get all session IDs for this project
	rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE project_id=?`, id)
	if err != nil {
		return fmt.Errorf("list sessions for project: %w", err)
	}
	var sessionIDs []string
	for rows.Next() {
		var sid string
		if err := rows.Scan(&sid); err != nil {
			rows.Close()
			return fmt.Errorf("scan session id: %w", err)
		}
		sessionIDs = append(sessionIDs, sid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate sessions: %w", err)
	}

	// Record tombstones and delete all sessions and their dependencies
	// in FK-respecting order (child-first): handoff_imports → handoff_capsules
	// → compaction_checkpoints → message_parts → token_ledger → messages
	// → message_session_state. Sessions are deleted after the loop.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, sid := range sessionIDs {
		if err := recordTombstonesForSessionTx(ctx, tx, now, sid); err != nil {
			return err
		}
		// 1. handoff_imports (FK to handoff_capsules)
		if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_imports WHERE capsule_id IN (SELECT id FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?)`, sid, sid); err != nil {
			return fmt.Errorf("delete handoff_imports for session %s: %w", sid, err)
		}
		// 2. handoff_capsules (FK to sessions, compaction_checkpoints)
		if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?`, sid, sid); err != nil {
			return fmt.Errorf("delete handoff_capsules for session %s: %w", sid, err)
		}
		// 3. compaction_checkpoints (FK to sessions, messages; ON DELETE RESTRICT)
		if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete compaction_checkpoints for session %s: %w", sid, err)
		}
		// 4. message_parts (FK to messages)
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_parts WHERE message_id IN (SELECT id FROM messages WHERE session_id=?)`, sid); err != nil {
			return fmt.Errorf("delete message_parts for session %s: %w", sid, err)
		}
		// 5. token_ledger (FK to messages)
		if _, err := tx.ExecContext(ctx, `DELETE FROM token_ledger WHERE message_id IN (SELECT id FROM messages WHERE session_id=?)`, sid); err != nil {
			return fmt.Errorf("delete token_ledger for session %s: %w", sid, err)
		}
		// 6. messages
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete messages for session %s: %w", sid, err)
		}
		// 7. message_session_state (FK to sessions)
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_session_state WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete message_session_state for session %s: %w", sid, err)
		}
	}

	// Delete all sessions (dependencies already deleted in the loop).
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	// Delete project-level tables that reference projects(id) ON DELETE RESTRICT.
	// Child tables use ON DELETE CASCADE and are cleaned up automatically.
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_project_usage WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete message_project_usage: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stages WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete stages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plans WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete plans: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete memories: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ontology_nodes WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete ontology_nodes: %w", err)
	}
	// Delete project
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	return tx.Commit()
}
