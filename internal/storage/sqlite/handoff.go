package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/handoff"
	"github.com/lunitide/lunitide/internal/handoffapp"
)

// CreateCapsule inserts a new handoff capsule.
func (s *Store) CreateCapsule(ctx context.Context, c handoff.Capsule) (handoff.Capsule, error) {
	if c.ID == "" {
		var err error
		c.ID, err = s.newULID(time.Now())
		if err != nil {
			return c, err
		}
	}
	c.CreatedAt = time.Now().UTC()
	if c.Status == "" {
		c.Status = handoff.StatusActive
	}
	if c.ActiveTasksJSON == "" {
		c.ActiveTasksJSON = "[]"
	}
	if c.RecentMessageIDs == "" {
		c.RecentMessageIDs = "[]"
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	var destSessionID, activatedAt, expiresAt any
	if c.DestSessionID != nil {
		destSessionID = *c.DestSessionID
	}
	if c.ActivatedAt != nil {
		activatedAt = formatTime(*c.ActivatedAt)
	}
	if c.ExpiresAt != nil {
		expiresAt = formatTime(*c.ExpiresAt)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO handoff_capsules(id, source_session_id, dest_session_id, checkpoint_id,
		 active_tasks_json, recent_message_ids, digest, status, created_at, activated_at, expires_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.SourceSessionID, destSessionID, c.CheckpointID,
		c.ActiveTasksJSON, c.RecentMessageIDs, c.Digest, c.Status,
		formatTime(c.CreatedAt), activatedAt, expiresAt)
	return c, mapWriteError(err)
}

// GetCapsule returns a capsule by ID.
func (s *Store) GetCapsule(ctx context.Context, id string) (*handoff.Capsule, error) {
	var c handoff.Capsule
	var created string
	var destSessionID, activatedAt, expiresAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, source_session_id, dest_session_id, checkpoint_id,
		 active_tasks_json, recent_message_ids, digest, status, created_at, activated_at, expires_at
		 FROM handoff_capsules WHERE id=?`, id).Scan(
		&c.ID, &c.SourceSessionID, &destSessionID, &c.CheckpointID,
		&c.ActiveTasksJSON, &c.RecentMessageIDs, &c.Digest, &c.Status,
		&created, &activatedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, err
	}
	if destSessionID.Valid {
		c.DestSessionID = &destSessionID.String
	}
	if activatedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, activatedAt.String)
		if err != nil {
			return nil, err
		}
		c.ActivatedAt = &t
	}
	if expiresAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, expiresAt.String)
		if err != nil {
			return nil, err
		}
		c.ExpiresAt = &t
	}
	return &c, nil
}

// ListCapsulesBySourceSession returns capsules for a source session ordered by creation time descending.
func (s *Store) ListCapsulesBySourceSession(ctx context.Context, sessionID string, limit int) ([]handoff.Capsule, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_session_id, dest_session_id, checkpoint_id,
		 active_tasks_json, recent_message_ids, digest, status, created_at, activated_at, expires_at
		 FROM handoff_capsules WHERE source_session_id=? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCapsules(rows)
}

// ListActiveCapsules returns all active (non-terminal) capsules for a session.
func (s *Store) ListActiveCapsules(ctx context.Context, sessionID string) ([]handoff.Capsule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_session_id, dest_session_id, checkpoint_id,
		 active_tasks_json, recent_message_ids, digest, status, created_at, activated_at, expires_at
		 FROM handoff_capsules WHERE source_session_id=? AND status='active' ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCapsules(rows)
}

// ActivateCapsule atomically validates the capsule and destination before
// binding it. BEGIN IMMEDIATE serializes activation with revoke/expire so only
// one terminal transition can win.
func (s *Store) ActivateCapsule(ctx context.Context, id string, destSessionID string, now time.Time) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)

	var status, sourceProject string
	var expiresAt sql.NullString
	err = conn.QueryRowContext(ctx, `SELECT c.status,c.expires_at,source.project_id
		FROM handoff_capsules c JOIN sessions source ON source.id=c.source_session_id
		WHERE c.id=?`, id).Scan(&status, &expiresAt, &sourceProject)
	if errors.Is(err, sql.ErrNoRows) {
		return handoffapp.ErrCapsuleNotFound
	}
	if err != nil {
		return err
	}
	var destProject string
	err = conn.QueryRowContext(ctx, `SELECT project_id FROM sessions WHERE id=?`, destSessionID).Scan(&destProject)
	if errors.Is(err, sql.ErrNoRows) {
		return handoffapp.ErrDestinationSessionNotFound
	}
	if err != nil {
		return err
	}
	if status != string(handoff.StatusActive) {
		return handoffapp.ErrCapsuleNotActive
	}
	if expiresAt.Valid {
		expires, parseErr := time.Parse(time.RFC3339Nano, expiresAt.String)
		if parseErr != nil {
			return parseErr
		}
		if !now.Before(expires) {
			return handoffapp.ErrCapsuleExpired
		}
	}
	if sourceProject != destProject {
		return handoffapp.ErrCrossProjectImport
	}
	res, err := conn.ExecContext(ctx,
		`UPDATE handoff_capsules SET dest_session_id=?, status='activated', activated_at=? WHERE id=? AND status='active'`,
		destSessionID, formatTime(now), id)
	if err := handoffConditionalResult(res, err); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return mapWriteError(err)
	}
	return nil
}

// RevokeCapsule sets a capsule's status to revoked.
func (s *Store) RevokeCapsule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE handoff_capsules SET status='revoked' WHERE id=? AND status='active'`, id)
	return handoffConditionalResult(res, err)
}

// ExpireCapsule sets a capsule's status to expired.
func (s *Store) ExpireCapsule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE handoff_capsules SET status='expired' WHERE id=? AND status='active'`, id)
	return handoffConditionalResult(res, err)
}

func handoffConditionalResult(res sql.Result, err error) error {
	if err != nil {
		return mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return handoffapp.ErrCapsuleNotActive
	}
	return nil
}

// RecordImport records that a capsule was imported into a target session as
// provenance-linked untrusted prior context (ADR-005 §5). The
// (capsule_id, target_session_id) UNIQUE constraint makes repeat imports
// idempotent: re-importing the same capsule into the same session is a no-op
// that returns the existing imported_at timestamp with isNew=false.
//
// The target_session_id FK to sessions(id) ON DELETE CASCADE ensures import
// records are cleaned up when the target session is deleted.
func (s *Store) ValidateAndRecordImport(ctx context.Context, capsuleID, targetSessionID string, now time.Time) (importedAt time.Time, isNew bool, err error) {
	importID, idErr := s.newULID(now)
	if idErr != nil {
		return time.Time{}, false, idErr
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return time.Time{}, false, mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	var status string
	var expiresAt sql.NullString
	var sourceProject, targetProject string
	err = conn.QueryRowContext(ctx, `SELECT c.status,c.expires_at,source.project_id,target.project_id
		FROM handoff_capsules c JOIN sessions source ON source.id=c.source_session_id
		JOIN sessions target ON target.id=? WHERE c.id=?`, targetSessionID, capsuleID).
		Scan(&status, &expiresAt, &sourceProject, &targetProject)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, handoffapp.ErrCapsuleNotFound
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if status != string(handoff.StatusActive) {
		return time.Time{}, false, handoffapp.ErrCapsuleNotActive
	}
	if expiresAt.Valid {
		expires, parseErr := time.Parse(time.RFC3339Nano, expiresAt.String)
		if parseErr != nil {
			return time.Time{}, false, parseErr
		}
		if now.After(expires) {
			return time.Time{}, false, handoffapp.ErrCapsuleExpired
		}
	}
	if sourceProject != targetProject {
		return time.Time{}, false, handoffapp.ErrCrossProjectImport
	}
	importedAt = now
	res, execErr := conn.ExecContext(ctx,
		`INSERT INTO handoff_imports(id, capsule_id, target_session_id, imported_at)
		 VALUES(?,?,?,?)
		 ON CONFLICT(capsule_id, target_session_id) DO NOTHING`,
		importID, capsuleID, targetSessionID, formatTime(importedAt))
	if execErr != nil {
		return time.Time{}, false, mapWriteError(execErr)
	}
	affected, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		return time.Time{}, false, rowsErr
	}
	if affected == 0 {
		// Idempotent re-import: load the existing imported_at.
		var existingAt string
		scanErr := conn.QueryRowContext(ctx,
			`SELECT imported_at FROM handoff_imports WHERE capsule_id=? AND target_session_id=?`,
			capsuleID, targetSessionID).Scan(&existingAt)
		if scanErr != nil {
			return time.Time{}, false, scanErr
		}
		importedAt, err = time.Parse(time.RFC3339Nano, existingAt)
		if err != nil {
			return time.Time{}, false, err
		}
		if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
			return time.Time{}, false, mapWriteError(err)
		}
		return importedAt, false, nil
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return time.Time{}, false, mapWriteError(err)
	}
	return importedAt, true, nil
}

// GetImport returns the imported_at timestamp for a capsule imported into a
// target session. Returns ok=false when no import record exists.
func (s *Store) GetImport(ctx context.Context, capsuleID, targetSessionID string) (importedAt time.Time, ok bool, err error) {
	var existingAt string
	scanErr := s.db.QueryRowContext(ctx,
		`SELECT imported_at FROM handoff_imports WHERE capsule_id=? AND target_session_id=?`,
		capsuleID, targetSessionID).Scan(&existingAt)
	if scanErr == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if scanErr != nil {
		return time.Time{}, false, scanErr
	}
	importedAt, err = time.Parse(time.RFC3339Nano, existingAt)
	if err != nil {
		return time.Time{}, false, err
	}
	return importedAt, true, nil
}

// ListImportedCapsules returns all capsules imported into the target session,
// ordered by imported_at descending. Only non-deleted, valid capsules are
// returned; capsules whose source has been revoked or expired are still
// returned so the caller can surface import history, but the chat send path
// must re-validate readability before injecting the summary.
func (s *Store) ListImportedCapsules(ctx context.Context, targetSessionID string) ([]handoff.Capsule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.source_session_id, c.dest_session_id, c.checkpoint_id,
		 c.active_tasks_json, c.recent_message_ids, c.digest, c.status,
		 c.created_at, c.activated_at, c.expires_at
		 FROM handoff_imports i
		 JOIN handoff_capsules c ON c.id = i.capsule_id
		 WHERE i.target_session_id=?
		 ORDER BY i.imported_at DESC`, targetSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCapsules(rows)
}

func scanCapsules(rows *sql.Rows) ([]handoff.Capsule, error) {
	var result []handoff.Capsule
	for rows.Next() {
		var c handoff.Capsule
		var created string
		var destSessionID, activatedAt, expiresAt sql.NullString
		if err := rows.Scan(
			&c.ID, &c.SourceSessionID, &destSessionID, &c.CheckpointID,
			&c.ActiveTasksJSON, &c.RecentMessageIDs, &c.Digest, &c.Status,
			&created, &activatedAt, &expiresAt); err != nil {
			return nil, err
		}
		var err error
		c.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		if destSessionID.Valid {
			c.DestSessionID = &destSessionID.String
		}
		if activatedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, activatedAt.String)
			if err != nil {
				return nil, err
			}
			c.ActivatedAt = &t
		}
		if expiresAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, expiresAt.String)
			if err != nil {
				return nil, err
			}
			c.ExpiresAt = &t
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
