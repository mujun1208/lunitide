package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/meetings"
)

func (s *Store) ReadMeetingSegmentPage(ctx context.Context, id string, revision int64, afterSeq, throughSeq int) (meetings.SegmentPage, error) {
	page := meetings.SegmentPage{Items: []meetings.Segment{}, Revision: revision}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	var actual int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM meetings WHERE meeting_id=?`, id).Scan(&actual); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = meetings.ErrNotFound
		}
		return page, err
	}
	if actual != revision {
		return page, meetings.ErrConflict
	}
	if throughSeq == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM meeting_segments WHERE meeting_id=?`, id).Scan(&throughSeq); err != nil {
			return page, err
		}
	}
	page.ThroughSeq = throughSeq
	rows, err := tx.QueryContext(ctx, `SELECT segment_id,meeting_id,seq,started_ms,body,created_at FROM meeting_segments WHERE meeting_id=? AND seq>? AND seq<=? ORDER BY seq LIMIT ?`, id, afterSeq, throughSeq, meetings.SegmentPageSize+1)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var seg meetings.Segment
		if err = rows.Scan(&seg.SegmentID, &seg.MeetingID, &seg.Seq, &seg.StartedMS, &seg.Text, &seg.CreatedAt); err != nil {
			break
		}
		page.Items = append(page.Items, seg)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > meetings.SegmentPageSize {
		page.HasMore = true
		page.Items = page.Items[:meetings.SegmentPageSize]
		page.NextSeq = page.Items[len(page.Items)-1].Seq
	}
	return page, tx.Commit()
}
