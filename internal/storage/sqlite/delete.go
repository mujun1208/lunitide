package sqlite

import (
	"context"
	"fmt"
)

// DeleteSession deletes a session and all its dependent records (messages,
// message_parts, message_session_state, compaction_checkpoints, token_ledger)
// in a single transaction. If the session does not exist, the operation is
// a no-op (idempotent delete).
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

	// Delete message_parts for this session's messages
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_parts WHERE message_id IN (SELECT id FROM messages WHERE session_id=?)`, id); err != nil {
		return fmt.Errorf("delete message_parts: %w", err)
	}
	// Delete messages
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	// Delete message_session_state
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_session_state WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete message_session_state: %w", err)
	}
	// Delete compaction_checkpoints
	if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete compaction_checkpoints: %w", err)
	}
	// Delete token_ledger entries
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_ledger WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete token_ledger: %w", err)
	}
	// Delete handoff_capsules
	if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?`, id, id); err != nil {
		return fmt.Errorf("delete handoff_capsules: %w", err)
	}
	// Delete session
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return tx.Commit()
}

// DeleteProject deletes a project and all its dependent records (sessions,
// messages, message_parts, etc.) in a single transaction. If the project
// does not exist, the operation is a no-op (idempotent delete).
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

	// Delete all sessions and their dependencies
	for _, sid := range sessionIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_parts WHERE message_id IN (SELECT id FROM messages WHERE session_id=?)`, sid); err != nil {
			return fmt.Errorf("delete message_parts for session %s: %w", sid, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete messages for session %s: %w", sid, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM message_session_state WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete message_session_state for session %s: %w", sid, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete compaction_checkpoints for session %s: %w", sid, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM token_ledger WHERE session_id=?`, sid); err != nil {
			return fmt.Errorf("delete token_ledger for session %s: %w", sid, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_capsules WHERE source_session_id=? OR dest_session_id=?`, sid, sid); err != nil {
			return fmt.Errorf("delete handoff_capsules for session %s: %w", sid, err)
		}
	}

	// Delete all sessions
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	// Delete project
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	return tx.Commit()
}
