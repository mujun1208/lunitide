package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/meetings"
)

func (s *Store) ReadMeetingSegmentsBounded(ctx context.Context, id string) ([]meetings.Segment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT segment_id,meeting_id,seq,started_ms,body,created_at FROM meeting_segments WHERE meeting_id=? ORDER BY seq LIMIT ?`, id, meetings.MaxSegments+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []meetings.Segment
	runes := 0
	for rows.Next() {
		var seg meetings.Segment
		if err = rows.Scan(&seg.SegmentID, &seg.MeetingID, &seg.Seq, &seg.StartedMS, &seg.Text, &seg.CreatedAt); err != nil {
			return nil, err
		}
		runes += utf8.RuneCountInString(seg.Text)
		if len(out) > 0 {
			runes++
		}
		if runes > meetings.MaxTranscriptRunes || len(out) >= meetings.MaxSegments {
			return nil, meetings.ErrCapacity
		}
		out = append(out, seg)
	}
	return out, rows.Err()
}

func (s *Store) MeetingSegmentCoverage(ctx context.Context, id string) (int64, int, error) {
	_, _, last, runes, err := meetingSegmentTotals(ctx, s.db, id)
	return last, runes, err
}

type meetingSegmentQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Count actual runes, including embedded NUL: SQLite length(text) stops at
// NUL. Read one legal segment at a time and stop at the aggregate budget.
func meetingSegmentTotals(ctx context.Context, q meetingSegmentQuerier, id string) (n, maxSeq int, last int64, runes int, err error) {
	rows, err := q.QueryContext(ctx, `SELECT seq,started_ms,body FROM meeting_segments WHERE meeting_id=? ORDER BY seq LIMIT ?`, id, meetings.MaxSegments+1)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var started int64
		var body string
		if err = rows.Scan(&seq, &started, &body); err != nil {
			return
		}
		n++
		maxSeq = max(maxSeq, seq)
		last = max(last, started)
		runes += utf8.RuneCountInString(body)
		if n > meetings.MaxSegments || runes+n-1 > meetings.MaxTranscriptRunes {
			err = meetings.ErrCapacity
			return
		}
	}
	err = rows.Err()
	return
}

// Admission and insert share the writer transaction, including across two
// Service instances. A capacity refusal preserves all prior rows and latches
// a visible notice so Stop cannot present the accepted prefix as complete.
func (s *Store) AppendMeetingSegmentBounded(ctx context.Context, seg meetings.Segment) (meetings.Segment, error) {
	capacity := false
	err := s.do(ctx, func(tx *txAdapter) error {
		var status, notice string
		if err := tx.q.QueryRowContext(ctx, `SELECT status,summary_error FROM meetings WHERE meeting_id=?`, seg.MeetingID).Scan(&status, &notice); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return meetings.ErrNotFound
			}
			return err
		}
		if status != string(meetings.StatusRecording) {
			return meetings.ErrNotRecording
		}
		n, maxSeq, _, runes, readErr := meetingSegmentTotals(ctx, tx.q, seg.MeetingID)
		if readErr != nil && !errors.Is(readErr, meetings.ErrCapacity) {
			return readErr
		}
		if errors.Is(readErr, meetings.ErrCapacity) || n >= meetings.MaxSegments || runes+n+utf8.RuneCountInString(seg.Text) > meetings.MaxTranscriptRunes || strings.HasPrefix(notice, meetings.CapacityNotice) {
			capacity = true
			_, err := tx.q.ExecContext(ctx, `UPDATE meetings SET summary_error=? WHERE meeting_id=? AND status='recording'`, meetings.CapacityNotice, seg.MeetingID)
			return err
		}
		seg.Seq = maxSeq + 1
		_, err := tx.q.ExecContext(ctx, `INSERT INTO meeting_segments(segment_id,meeting_id,seq,started_ms,body,created_at) VALUES(?,?,?,?,?,?)`, seg.SegmentID, seg.MeetingID, seg.Seq, seg.StartedMS, seg.Text, seg.CreatedAt)
		return err
	})
	if err == nil && capacity {
		err = meetings.ErrCapacity
	}
	return seg, err
}
