package sqlite

import (
	"context"
	"fmt"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/messageapp"
)

// compactionMessageReader adapts the Store's ListMessages to the
// compactionapp.MessageReader interface used by the compaction trigger.
type compactionMessageReader struct {
	store *Store
}

func (r *compactionMessageReader) ListMessages(ctx context.Context, sessionID string, direction string, limit int) ([]compactionapp.MessageInfo, error) {
	dir := messageapp.Forward
	if direction == "backward" {
		dir = messageapp.Backward
	}
	msgs, _, _, err := r.store.ListMessages(ctx, messageapp.PageQuery{
		SessionID: sessionID,
		Direction: dir,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]compactionapp.MessageInfo, len(msgs))
	for i, m := range msgs {
		result[i] = compactionapp.MessageInfo{
			ID:       m.ID,
			Sequence: m.Sequence,
		}
	}
	return result, nil
}

// CompactionMessageReader returns a compactionapp.MessageReader backed by this store.
// The Store already implements compactionapp.CheckpointStore via CreateCheckpoint,
// GetLatestCheckpoint and CountCheckpointsBySession.
func (s *Store) CompactionMessageReader() compactionapp.MessageReader {
	return &compactionMessageReader{store: s}
}

// ListMessagesByRange returns messages in the session within [startSeq, endSeq]
// inclusive, ordered by sequence ascending. Implements compactionapp.SourceReader.
func (s *Store) ListMessagesByRange(ctx context.Context, sessionID string, startSeq, endSeq int64) ([]compactionapp.SummaryMessage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.role, m.sequence,
			MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END)
		 FROM messages m
		 LEFT JOIN message_parts p ON p.message_id=m.id
		 WHERE m.session_id=? AND m.sequence>=? AND m.sequence<=?
		 GROUP BY m.id
		 ORDER BY m.sequence ASC`,
		sessionID, startSeq, endSeq)
	if err != nil {
		return nil, fmt.Errorf("list messages by range: %w", err)
	}
	defer rows.Close()
	items := make([]compactionapp.SummaryMessage, 0, 64)
	for rows.Next() {
		var m compactionapp.SummaryMessage
		var text *string
		if err := rows.Scan(&m.ID, &m.Role, &m.Sequence, &text); err != nil {
			return nil, fmt.Errorf("scan message by range: %w", err)
		}
		if text != nil {
			m.Content = *text
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages by range: %w", err)
	}
	return items, nil
}

// GetLatestCompactionSummary returns the human-readable summary of the latest
// succeeded compaction checkpoint for the session, or empty string if none.
func (s *Store) GetLatestCompactionSummary(ctx context.Context, sessionID string) (string, error) {
	var summary string
	err := s.db.QueryRowContext(ctx,
		`SELECT human_summary FROM compaction_checkpoints
		 WHERE session_id=? AND status='succeeded'
		 ORDER BY version DESC LIMIT 1`, sessionID).Scan(&summary)
	if err != nil {
		return "", nil //nolint:nilerr // 没有摘要不是错误
	}
	return summary, nil
}
