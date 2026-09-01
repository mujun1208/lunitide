package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	return s.deleteSession(ctx, id, "system")
}

// DeleteSessionAudited hard-deletes a session and attributes the deletion to
// actor in the same transaction. A blank actor falls back to "system" so the
// audit row always names someone.
func (s *Store) DeleteSessionAudited(ctx context.Context, id, actor string) error {
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	return s.deleteSession(ctx, id, actor)
}

func (s *Store) deleteSession(ctx context.Context, id, actor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete session tx: %w", err)
	}
	defer tx.Rollback()

	// Idempotent: if session doesn't exist, return success. Capture the owning
	// project and the session's accounted bytes before deleting either row so
	// the aggregate quota counters remain consistent.
	var projectID string
	var sessionTextBytes int64
	err = tx.QueryRowContext(ctx, `SELECT s.project_id,st.text_bytes FROM sessions s JOIN message_session_state st ON st.session_id=s.id WHERE s.id=?`, id).Scan(&projectID, &sessionTextBytes)
	if err == sql.ErrNoRows {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, id).Scan(&count); err != nil {
			return fmt.Errorf("check session exists: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("delete session data invariant violation: missing message session state")
		}
		return tx.Commit() // no-op, idempotent
	}
	if err != nil {
		return fmt.Errorf("check session exists: %w", err)
	}
	projectUsage, err := tx.ExecContext(ctx, `UPDATE message_project_usage SET text_bytes=text_bytes-? WHERE project_id=? AND text_bytes>=?`, sessionTextBytes, projectID, sessionTextBytes)
	if err != nil {
		return fmt.Errorf("decrement project message usage: %w", err)
	}
	projectRows, err := projectUsage.RowsAffected()
	if err != nil || projectRows != 1 {
		return fmt.Errorf("delete session project usage invariant violation")
	}
	workspaceUsage, err := tx.ExecContext(ctx, `UPDATE message_workspace_usage SET text_bytes=text_bytes-? WHERE singleton=1 AND text_bytes>=?`, sessionTextBytes, sessionTextBytes)
	if err != nil {
		return fmt.Errorf("decrement workspace message usage: %w", err)
	}
	workspaceRows, err := workspaceUsage.RowsAffected()
	if err != nil || workspaceRows != 1 {
		return fmt.Errorf("delete session workspace usage invariant violation")
	}

	// Record tombstones before physical deletion for fail-closed readability.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := recordTombstonesForSessionTx(ctx, tx, now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) SELECT 'attachment_file',file_ref,?,'pending' FROM attachments WHERE session_id=?`, now, id); err != nil {
		return fmt.Errorf("schedule session attachment cleanup: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) SELECT 'attachment',id,?,'pending' FROM attachments WHERE session_id=?`, now, id); err != nil {
		return fmt.Errorf("record session attachment tombstones: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete session attachments: %w", err)
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
	// 3. compaction_activations (FK to sessions and compaction_checkpoints)
	if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_activations WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete compaction_activations: %w", err)
	}
	// 4. compaction_checkpoints (FK to sessions, messages; ON DELETE RESTRICT)
	if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete compaction_checkpoints: %w", err)
	}
	// Message idempotency responses embed the session ID and must not outlive
	// their authoritative messages, otherwise startup replay validation fails.
	if _, err := tx.ExecContext(ctx, `DELETE FROM idempotency_records WHERE operation IN ('message.append','message.append-assistant') AND json_extract(response_json,'$.sessionId')=?`, id); err != nil {
		return fmt.Errorf("delete message idempotency records: %w", err)
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_expert_mounts WHERE session_id=?`, id); err != nil {
		return fmt.Errorf("delete session_expert_mounts: %w", err)
	}
	// 8. session
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	// Audit lands on the same transaction as the physical delete: a hard delete
	// of a real session either records why it happened or does not commit.
	if err := s.appendAuditTx(ctx, tx, "session.deleted", id, actor, map[string]any{"projectId": projectID}); err != nil {
		return fmt.Errorf("audit session delete: %w", err)
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

	// Idempotent: if project doesn't exist, return success. Capture the
	// project's accounted bytes before deleting its usage row so the workspace
	// aggregate can be decremented in this transaction.
	var projectTextBytes int64
	err = tx.QueryRowContext(ctx, `SELECT u.text_bytes FROM projects p JOIN message_project_usage u ON u.project_id=p.id WHERE p.id=?`, id).Scan(&projectTextBytes)
	if err == sql.ErrNoRows {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=?`, id).Scan(&count); err != nil {
			return fmt.Errorf("check project exists: %w", err)
		}
		if count != 0 {
			return fmt.Errorf("delete project data invariant violation: missing message project usage")
		}
		return tx.Commit() // no-op, idempotent
	}
	if err != nil {
		return fmt.Errorf("check project exists: %w", err)
	}
	workspaceUsage, err := tx.ExecContext(ctx, `UPDATE message_workspace_usage SET text_bytes=text_bytes-? WHERE singleton=1 AND text_bytes>=?`, projectTextBytes, projectTextBytes)
	if err != nil {
		return fmt.Errorf("decrement workspace message usage: %w", err)
	}
	workspaceRows, err := workspaceUsage.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect workspace message usage decrement: %w", err)
	}
	if workspaceRows != 1 {
		return fmt.Errorf("delete project workspace usage invariant violation")
	}

	// Record tombstones for fail-closed readability (ADR-005 §7), then delete
	// every session and its dependents in FK-respecting child-first order.
	// These are project-scoped set operations rather than a per-session loop, so
	// deleting a project with many sessions costs a fixed number of round-trips
	// instead of the ~10 statements per session the loop used to issue. The
	// deletes read `sessions` through the subquery and sessions themselves are
	// removed afterwards, so every subquery still resolves while it runs.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const inProject = `IN (SELECT id FROM sessions WHERE project_id=?)`

	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status)
		SELECT 'session', id, ?, 'pending' FROM sessions WHERE project_id=?`, now, id); err != nil {
		return fmt.Errorf("record session tombstones: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status)
		SELECT 'checkpoint', id, ?, 'pending' FROM compaction_checkpoints WHERE session_id `+inProject, now, id); err != nil {
		return fmt.Errorf("record checkpoint tombstones: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status)
		SELECT 'capsule', id, ?, 'pending' FROM handoff_capsules WHERE source_session_id `+inProject+` OR dest_session_id `+inProject, now, id, id); err != nil {
		return fmt.Errorf("record capsule tombstones: %w", err)
	}

	// 1. handoff_imports (FK to handoff_capsules)
	if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_imports WHERE capsule_id IN (SELECT id FROM handoff_capsules WHERE source_session_id `+inProject+` OR dest_session_id `+inProject+`)`, id, id); err != nil {
		return fmt.Errorf("delete handoff_imports: %w", err)
	}
	// 2. handoff_capsules (FK to sessions, compaction_checkpoints)
	if _, err := tx.ExecContext(ctx, `DELETE FROM handoff_capsules WHERE source_session_id `+inProject+` OR dest_session_id `+inProject, id, id); err != nil {
		return fmt.Errorf("delete handoff_capsules: %w", err)
	}
	// 3. compaction_activations (FK to sessions and compaction_checkpoints)
	if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_activations WHERE session_id `+inProject, id); err != nil {
		return fmt.Errorf("delete compaction_activations: %w", err)
	}
	// 4. compaction_checkpoints (FK to sessions, messages; ON DELETE RESTRICT)
	if _, err := tx.ExecContext(ctx, `DELETE FROM compaction_checkpoints WHERE session_id `+inProject, id); err != nil {
		return fmt.Errorf("delete compaction_checkpoints: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM idempotency_records WHERE operation IN ('message.append','message.append-assistant') AND json_extract(response_json,'$.sessionId') `+inProject, id); err != nil {
		return fmt.Errorf("delete message idempotency records: %w", err)
	}
	// 5. message_parts (FK to messages)
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_parts WHERE message_id IN (SELECT id FROM messages WHERE session_id `+inProject+`)`, id); err != nil {
		return fmt.Errorf("delete message_parts: %w", err)
	}
	// 6. token_ledger (FK to messages)
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_ledger WHERE message_id IN (SELECT id FROM messages WHERE session_id `+inProject+`)`, id); err != nil {
		return fmt.Errorf("delete token_ledger: %w", err)
	}
	// 7. messages
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id `+inProject, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	// 8. message_session_state (FK to sessions)
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_session_state WHERE session_id `+inProject, id); err != nil {
		return fmt.Errorf("delete message_session_state: %w", err)
	}
	// 9. session_expert_mounts (FK to sessions)
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_expert_mounts WHERE session_id `+inProject, id); err != nil {
		return fmt.Errorf("delete session_expert_mounts: %w", err)
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
	// Attachments: project_id has ON DELETE RESTRICT, so we must delete all
	// attachments before deleting the project. Record tombstones for
	// fail-closed readability (ADR-005 §7).
	attRows, err := tx.QueryContext(ctx, `SELECT id, file_ref FROM attachments WHERE project_id=?`, id)
	if err != nil {
		return fmt.Errorf("list attachments for project delete: %w", err)
	}
	var attachmentIDs []string
	var attachmentFileRefs []string
	for attRows.Next() {
		var aid, fileRef string
		if err := attRows.Scan(&aid, &fileRef); err != nil {
			attRows.Close()
			return fmt.Errorf("scan attachment id: %w", err)
		}
		attachmentIDs = append(attachmentIDs, aid)
		attachmentFileRefs = append(attachmentFileRefs, fileRef)
	}
	attRows.Close()
	if err := attRows.Err(); err != nil {
		return fmt.Errorf("iterate attachments: %w", err)
	}
	for _, aid := range attachmentIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('attachment',?,?,'pending')`, aid, now); err != nil {
			return fmt.Errorf("record attachment tombstone: %w", err)
		}
	}
	for _, fileRef := range attachmentFileRefs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deletion_tombstones(owner_type,owner_id,deleted_at,propagation_status) VALUES('attachment_file',?,?,'pending')`, fileRef, now); err != nil {
			return fmt.Errorf("schedule project attachment cleanup: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE project_id=?`, id); err != nil {
		return fmt.Errorf("delete attachments: %w", err)
	}
	// Delete project
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	return tx.Commit()
}
