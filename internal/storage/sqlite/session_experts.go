package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

const sessionExpertMountCap = 8

func validSessionExpertULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func (s *Store) ListSessionExpertIDs(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT expert_id FROM session_expert_mounts WHERE session_id=? ORDER BY ordinal, expert_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session expert mounts: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan session expert mount: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session expert mounts: %w", err)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

func (s *Store) ReplaceSessionExpertIDs(ctx context.Context, sessionID string, expertIDs []string) error {
	if !validSessionExpertULID(sessionID) {
		return sessionapp.ErrSessionNotFound
	}
	if len(expertIDs) > sessionExpertMountCap {
		return fmt.Errorf("session expert capacity reached")
	}
	seen := map[string]bool{}
	for _, id := range expertIDs {
		if !validSessionExpertULID(id) || seen[id] {
			return fmt.Errorf("session expert id invalid")
		}
		seen[id] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session expert mounts: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, sessionID).Scan(&count); err != nil {
		return fmt.Errorf("check session for expert mounts: %w", err)
	}
	if count == 0 {
		return sessionapp.ErrSessionNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM session_expert_mounts WHERE session_id=?`, sessionID); err != nil {
		return fmt.Errorf("clear session expert mounts: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i, id := range expertIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_expert_mounts(session_id, expert_id, ordinal, created_at) VALUES(?,?,?,?)`, sessionID, id, i, now); err != nil {
			return fmt.Errorf("insert session expert mount: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session expert mounts: %w", err)
	}
	return nil
}
