package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/handoff"
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

// ActivateCapsule binds a capsule to a destination session and sets its status to activated.
func (s *Store) ActivateCapsule(ctx context.Context, id string, destSessionID string) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx,
		`UPDATE handoff_capsules SET dest_session_id=?, status='activated', activated_at=? WHERE id=? AND status='active'`,
		destSessionID, now, id)
	return mapWriteError(err)
}

// RevokeCapsule sets a capsule's status to revoked.
func (s *Store) RevokeCapsule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE handoff_capsules SET status='revoked' WHERE id=? AND status='active'`, id)
	return mapWriteError(err)
}

// ExpireCapsule sets a capsule's status to expired.
func (s *Store) ExpireCapsule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE handoff_capsules SET status='expired' WHERE id=? AND status='active'`, id)
	return mapWriteError(err)
}

// RecordImport records that a capsule was imported into a target session as
// provenance-linked untrusted prior context (ADR-005 §5). The
// (capsule_id, target_session_id) UNIQUE constraint makes repeat imports
// idempotent: re-importing the same capsule into the same session is a no-op
// that returns the existing imported_at timestamp with isNew=false.
//
// The target_session_id FK to sessions(id) ON DELETE CASCADE ensures import
// records are cleaned up when the target session is deleted.
func (s *Store) RecordImport(ctx context.Context, capsuleID, targetSessionID string) (importID string, importedAt time.Time, isNew bool, err error) {
	now := time.Now().UTC()
	importID, idErr := s.newULID(now)
	if idErr != nil {
		return "", time.Time{}, false, idErr
	}
	importedAt = now
	res, execErr := s.db.ExecContext(ctx,
		`INSERT INTO handoff_imports(id, capsule_id, target_session_id, imported_at)
		 VALUES(?,?,?,?)
		 ON CONFLICT(capsule_id, target_session_id) DO NOTHING`,
		importID, capsuleID, targetSessionID, formatTime(importedAt))
	if execErr != nil {
		return "", time.Time{}, false, mapWriteError(execErr)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// Idempotent re-import: load the existing imported_at.
		var existingAt string
		scanErr := s.db.QueryRowContext(ctx,
			`SELECT imported_at FROM handoff_imports WHERE capsule_id=? AND target_session_id=?`,
			capsuleID, targetSessionID).Scan(&existingAt)
		if scanErr != nil {
			return "", time.Time{}, false, scanErr
		}
		importedAt, err = time.Parse(time.RFC3339Nano, existingAt)
		if err != nil {
			return "", time.Time{}, false, err
		}
		return "", importedAt, false, nil
	}
	return importID, importedAt, true, nil
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