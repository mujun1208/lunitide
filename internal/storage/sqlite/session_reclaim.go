package sqlite

import (
	"context"
	"fmt"
	"strings"
)

// EmptyDraftSessionTitles are leftover launch/companion shells that fill the
// 100-session project cap while staying hidden or untitled in the sidebar.
var EmptyDraftSessionTitles = []string{
	"新对话", "New chat",
	"月伴对话", "Companion talk",
	"创建技能", "创建专家", "创建能力包", "创建自动化",
}

// ListEmptyDraftSessionIDs returns unpinned, unbound sessions with no messages
// whose titles are leftover drafts. Oldest first.
func (s *Store) ListEmptyDraftSessionIDs(ctx context.Context, projectID string, titles []string, limit int) ([]string, error) {
	if s == nil || s.db == nil || strings.TrimSpace(projectID) == "" || limit < 1 {
		return nil, nil
	}
	if len(titles) == 0 {
		titles = EmptyDraftSessionTitles
	}
	holders := make([]string, len(titles))
	args := make([]any, 0, 2+len(titles))
	args = append(args, projectID)
	for i, title := range titles {
		holders[i] = "?"
		args = append(args, title)
	}
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT s.id FROM sessions s
		LEFT JOIN message_session_state st ON st.session_id=s.id
		LEFT JOIN people_thread_session pts ON pts.session_id=s.id
		WHERE s.project_id=?
		  AND s.pinned=0
		  AND pts.session_id IS NULL
		  AND COALESCE(st.message_count,0)=0
		  AND s.title IN (%s)
		ORDER BY s.updated_at ASC, s.id ASC
		LIMIT ?`, strings.Join(holders, ","))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}
