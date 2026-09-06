package sqlite

import (
	"context"
	"github.com/lunitide/lunitide/internal/people"
)

func (s *Store) ListPeopleMessagesBefore(ctx context.Context, threadID, before string, limit int) ([]people.Message, error) {
	if limit < 1 || limit > 200 || before == "" || len(before) > 128 {
		return nil, people.ErrInvalid
	}
	var stamp string
	if err := s.db.QueryRowContext(ctx, `SELECT created_at FROM people_messages WHERE thread_id=? AND message_id=?`, threadID, before).Scan(&stamp); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, peopleMessageSelect+` WHERE m.message_id IN
	 (SELECT message_id FROM people_messages WHERE thread_id=? AND (created_at,message_id)<(?,?) ORDER BY created_at DESC,message_id DESC LIMIT ?)
	 ORDER BY m.created_at,m.message_id`, threadID, stamp, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []people.Message{}
	for rows.Next() {
		item, err := scanPeopleMessage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
