package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
)

func (s *Store) ListMessages(ctx context.Context, q messageapp.PageQuery) ([]message.Message, int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, false, err
	}
	defer tx.Rollback()
	var snapshot int64
	if err := tx.QueryRowContext(ctx, `SELECT last_sequence FROM message_session_state WHERE session_id=?`, q.SessionID).Scan(&snapshot); err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, false, messageapp.ErrSessionNotFound
		}
		return nil, 0, false, err
	}
	if q.Snapshot != 0 {
		snapshot = q.Snapshot
	}
	boundary := q.Boundary
	if boundary == 0 && q.Direction == messageapp.Backward {
		boundary = snapshot + 1
	}
	op, order := ">", "ASC"
	if q.Direction == messageapp.Backward {
		op, order = "<", "DESC"
	}
	statement := fmt.Sprintf(`SELECT m.id,m.session_id,m.role,m.status,m.sequence,MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END),m.created_at,count(p.message_id),count(CASE WHEN p.ordinal=1 AND p.type='text' THEN 1 END) FROM messages m LEFT JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=? AND m.sequence<=? AND m.sequence %s ? GROUP BY m.id ORDER BY m.sequence %s LIMIT ?`, op, order)
	rows, err := tx.QueryContext(ctx, statement, q.SessionID, snapshot, boundary, q.Limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	items := make([]message.Message, 0, q.Limit+1)
	for rows.Next() {
		var v message.Message
		var created string
		var text sql.NullString
		var parts, validParts int
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Role, &v.Status, &v.Sequence, &text, &created, &parts, &validParts); err != nil {
			return nil, 0, false, err
		}
		if parts != 1 || validParts != 1 || !text.Valid {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		v.Text = text.String
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		expected := boundary + int64(len(items)) + 1
		if q.Direction == messageapp.Backward {
			expected = boundary - int64(len(items)) - 1
		}
		if err != nil || v.Validate() != nil || v.Sequence != expected {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(items) > q.Limit
	if more {
		items = items[:q.Limit]
	}
	if !more && snapshot > 0 {
		if q.Direction == messageapp.Forward && (len(items) == 0 || items[len(items)-1].Sequence != snapshot) {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		if q.Direction == messageapp.Backward && (len(items) == 0 || items[len(items)-1].Sequence != 1) {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
	}
	if err = rows.Close(); err != nil {
		return nil, 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, false, err
	}
	return items, snapshot, more, nil
}
