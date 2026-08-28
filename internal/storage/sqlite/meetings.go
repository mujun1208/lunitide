package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/meetings"
)

func (s *Store) InsertMeeting(ctx context.Context, m meetings.Meeting) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meetings(meeting_id, title, status, audio_source, started_at, ended_at, duration_ms, summary, actions, transcript, summary_error, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.MeetingID, m.Title, string(m.Status), m.AudioSource, m.StartedAt, m.EndedAt, m.DurationMS, m.Summary, m.Actions, m.Transcript, m.SummaryError, m.CreatedAt, m.UpdatedAt)
	return err
}

func (s *Store) UpdateMeeting(ctx context.Context, m meetings.Meeting) error {
	res, err := s.db.ExecContext(ctx, `UPDATE meetings SET title=?, status=?, audio_source=?, started_at=?, ended_at=?, duration_ms=?, summary=?, actions=?, transcript=?, summary_error=?, updated_at=? WHERE meeting_id=?`,
		m.Title, string(m.Status), m.AudioSource, m.StartedAt, m.EndedAt, m.DurationMS, m.Summary, m.Actions, m.Transcript, m.SummaryError, m.UpdatedAt, m.MeetingID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return meetings.ErrNotFound
	}
	return nil
}

func (s *Store) GetMeeting(ctx context.Context, id string) (meetings.Meeting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT meeting_id, title, status, audio_source, started_at, ended_at, duration_ms, summary, actions, transcript, summary_error, created_at, updated_at FROM meetings WHERE meeting_id=?`, id)
	m, err := scanMeeting(row)
	if errors.Is(err, sql.ErrNoRows) {
		return meetings.Meeting{}, meetings.ErrNotFound
	}
	return m, err
}

func (s *Store) ListMeetings(ctx context.Context, limit int) ([]meetings.Meeting, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT meeting_id, title, status, audio_source, started_at, ended_at, duration_ms, summary, actions, transcript, summary_error, created_at, updated_at FROM meetings ORDER BY started_at DESC, meeting_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []meetings.Meeting
	for rows.Next() {
		m, scanErr := scanMeeting(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *Store) InsertSegment(ctx context.Context, seg meetings.Segment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_segments(segment_id, meeting_id, seq, started_ms, body, created_at) VALUES(?,?,?,?,?,?)`,
		seg.SegmentID, seg.MeetingID, seg.Seq, seg.StartedMS, seg.Text, seg.CreatedAt)
	return err
}

func (s *Store) CountSegments(ctx context.Context, meetingID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meeting_segments WHERE meeting_id=?`, meetingID).Scan(&n)
	return n, err
}

func (s *Store) ListSegments(ctx context.Context, meetingID string) ([]meetings.Segment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT segment_id, meeting_id, seq, started_ms, body, created_at FROM meeting_segments WHERE meeting_id=? ORDER BY seq`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []meetings.Segment
	for rows.Next() {
		var seg meetings.Segment
		if err := rows.Scan(&seg.SegmentID, &seg.MeetingID, &seg.Seq, &seg.StartedMS, &seg.Text, &seg.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, seg)
	}
	return items, rows.Err()
}

func (s *Store) ReplaceDocs(ctx context.Context, meetingID string, docs []meetings.Doc) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM meeting_docs WHERE meeting_id=?`, meetingID); err != nil {
		return err
	}
	for _, doc := range docs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meeting_docs(doc_id, meeting_id, kind, body, created_at) VALUES(?,?,?,?,?)`,
			doc.DocID, doc.MeetingID, doc.Kind, doc.Body, doc.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListDocs(ctx context.Context, meetingID string) ([]meetings.Doc, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc_id, meeting_id, kind, body, created_at FROM meeting_docs WHERE meeting_id=? ORDER BY created_at DESC, doc_id DESC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []meetings.Doc
	for rows.Next() {
		var doc meetings.Doc
		if err := rows.Scan(&doc.DocID, &doc.MeetingID, &doc.Kind, &doc.Body, &doc.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, doc)
	}
	return items, rows.Err()
}

func (s *Store) DeleteMeeting(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM meeting_segments WHERE meeting_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meeting_docs WHERE meeting_id=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM meetings WHERE meeting_id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return meetings.ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) HasRecording(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meetings WHERE status='recording'`).Scan(&n)
	return n > 0, err
}

type meetingScanner interface {
	Scan(dest ...any) error
}

func scanMeeting(row meetingScanner) (meetings.Meeting, error) {
	var m meetings.Meeting
	var status string
	err := row.Scan(&m.MeetingID, &m.Title, &status, &m.AudioSource, &m.StartedAt, &m.EndedAt, &m.DurationMS, &m.Summary, &m.Actions, &m.Transcript, &m.SummaryError, &m.CreatedAt, &m.UpdatedAt)
	m.Status = meetings.Status(status)
	return m, err
}
