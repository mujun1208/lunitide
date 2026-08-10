package sqlite

import (
	"context"
	"time"

	"github.com/lunitide/lunitide/internal/domain/stage"
)

func (s *Store) ListStages(ctx context.Context, filter stage.Filter) ([]stage.Stage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,phase,title,status,created_at,updated_at,version FROM stages WHERE project_id=? ORDER BY phase,id`, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []stage.Stage{}
	for rows.Next() {
		var v stage.Stage
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Phase, &v.Title, &v.Status, &created, &updated, &v.Version); err != nil {
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
	return items, nil
}
