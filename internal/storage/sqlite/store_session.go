package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func (s *Store) ListSessions(ctx context.Context, filter session.Filter) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,pinned,status,created_at,updated_at,revision FROM sessions WHERE project_id=? ORDER BY pinned DESC,created_at,id LIMIT 101`, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []session.Session{}
	for rows.Next() {
		var v session.Session
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Pinned, &v.Status, &created, &updated, &v.Version); err != nil {
			return nil, err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if err = v.Validate(); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("session data invariant violation: list exceeds capacity")
	}
	return items, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (session.Session, error) {
	var v session.Session
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,title,pinned,status,created_at,updated_at,revision FROM sessions WHERE id=?`, id).
		Scan(&v.ID, &v.ProjectID, &v.Title, &v.Pinned, &v.Status, &created, &updated, &v.Version)
	if err != nil {
		if err == sql.ErrNoRows {
			return session.Session{}, sessionapp.ErrSessionNotFound
		}
		return session.Session{}, err
	}
	if v.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return session.Session{}, err
	}
	if v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return session.Session{}, err
	}
	if err = v.Validate(); err != nil {
		return session.Session{}, err
	}
	return v, nil
}
