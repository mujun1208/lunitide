package sqlite

import (
	"context"
	"errors"
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

var errEmptyDraftChanged = errors.New("empty draft is no longer eligible for reclamation")

// Keep candidate discovery and the transactional delete guard identical. A
// session with an attachment already contains user input even before sending.
func emptyDraftEligibility(titles []string) (string, []any) {
	if len(titles) == 0 {
		titles = EmptyDraftSessionTitles
	}
	holders := make([]string, len(titles))
	args := make([]any, len(titles))
	for i, title := range titles {
		holders[i] = "?"
		args[i] = title
	}
	return `s.pinned=0
		AND EXISTS (SELECT 1 FROM message_session_state st WHERE st.session_id=s.id AND st.message_count=0)
		AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.session_id=s.id)
		AND NOT EXISTS (SELECT 1 FROM attachments a WHERE a.session_id=s.id)
		AND NOT EXISTS (SELECT 1 FROM people_thread_session pts WHERE pts.session_id=s.id)
		AND NOT EXISTS (SELECT 1 FROM audit_events a WHERE a.aggregate_id=s.id AND a.action='session.created' AND a.actor='automation')
		AND NOT EXISTS (SELECT 1 FROM office_tasks ot WHERE ot.session_id=s.id)
		AND s.title IN (` + strings.Join(holders, ",") + `)`, args
}

// DeleteEmptyDraftSession rechecks eligibility inside the same transaction as
// the existing audited cascade. A stale candidate becomes a harmless skip.
func (s *Store) DeleteEmptyDraftSession(ctx context.Context, projectID, id string) (bool, error) {
	err := s.deleteSession(ctx, id, "draft-reclaimer", projectID)
	if errors.Is(err, errEmptyDraftChanged) {
		return false, nil
	}
	return err == nil, err
}

// ListEmptyDraftSessionIDs returns unpinned, unbound sessions with no messages
// whose titles are leftover drafts. Oldest first.
func (s *Store) ListEmptyDraftSessionIDs(ctx context.Context, projectID string, titles []string, limit int) ([]string, error) {
	if s == nil || s.db == nil || strings.TrimSpace(projectID) == "" || limit < 1 {
		return nil, nil
	}
	predicate, titleArgs := emptyDraftEligibility(titles)
	args := append([]any{projectID}, titleArgs...)
	args = append(args, limit)
	q := fmt.Sprintf(`SELECT s.id FROM sessions s
		WHERE s.project_id=? AND %s
		ORDER BY s.updated_at ASC, s.id ASC
		LIMIT ?`, predicate)
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
