package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/meetings"
)

func (s *Store) InsertMeeting(ctx context.Context, m meetings.Meeting) error {
	transcriptRevision := m.TranscriptRevision
	if transcriptRevision == 0 && m.Transcript != "" {
		transcriptRevision = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO meetings(meeting_id, title, status, audio_source, started_at, ended_at, duration_ms, summary, actions, transcript, summary_error, created_at, updated_at, transcript_revision, summary_source_revision, summary_source_digest, summary_source_title, summary_source_transcript, summary_edited)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.MeetingID, m.Title, string(m.Status), m.AudioSource, m.StartedAt, m.EndedAt, m.DurationMS, m.Summary, m.Actions, m.Transcript, m.SummaryError, m.CreatedAt, m.UpdatedAt,
		transcriptRevision, m.SummarySourceRevision, m.SummarySourceDigest, m.SummarySourceTitle, m.SummarySourceTranscript, m.SummaryEdited)
	return err
}

func (s *Store) UpdateMeeting(ctx context.Context, m meetings.Meeting) error {
	res, err := s.db.ExecContext(ctx, `UPDATE meetings SET title=?, status=?, audio_source=?, started_at=?, ended_at=?, duration_ms=?, summary=?, actions=?, transcript=?, summary_error=?, updated_at=?, transcript_revision=transcript_revision+CASE WHEN transcript<>? THEN 1 ELSE 0 END, summary_source_revision=?, summary_source_digest=?, summary_source_title=?, summary_source_transcript=?, summary_edited=?, revision=revision+1 WHERE meeting_id=?`,
		m.Title, string(m.Status), m.AudioSource, m.StartedAt, m.EndedAt, m.DurationMS, m.Summary, m.Actions, m.Transcript, m.SummaryError, m.UpdatedAt, m.Transcript, m.SummarySourceRevision, m.SummarySourceDigest, m.SummarySourceTitle, m.SummarySourceTranscript, m.SummaryEdited, m.MeetingID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return meetings.ErrNotFound
	}
	return nil
}

// Content revision rejects stale edits. A recording heartbeat only advances
// activity metadata, so it cannot invalidate Stop's otherwise current revision.
// Once recording ends, the complete editor/model snapshot remains mandatory.
func (s *Store) CompareAndSwapMeeting(ctx context.Context, previousUpdatedAt string, m meetings.Meeting) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE meetings SET title=?, status=?, audio_source=?, started_at=?, ended_at=?, duration_ms=CASE WHEN status='recording' THEN MAX(duration_ms,?) ELSE ? END, summary=?, actions=?, transcript=?, summary_error=?, updated_at=?, transcript_revision=transcript_revision+CASE WHEN transcript<>? THEN 1 ELSE 0 END, summary_source_revision=?, summary_source_digest=?, summary_source_title=?, summary_source_transcript=?, summary_edited=?, revision=revision+1 WHERE meeting_id=? AND (updated_at=? OR status='recording') AND revision=?`,
		m.Title, string(m.Status), m.AudioSource, m.StartedAt, m.EndedAt, m.DurationMS, m.DurationMS, m.Summary, m.Actions, m.Transcript, m.SummaryError, m.UpdatedAt, m.Transcript, m.SummarySourceRevision, m.SummarySourceDigest, m.SummarySourceTitle, m.SummarySourceTranscript, m.SummaryEdited, m.MeetingID, previousUpdatedAt, m.Revision)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Late heartbeats/captions cannot restore recording, erase the transcript or
// overwrite a title. Duration is monotonic even if heartbeat replies reorder.
func (s *Store) TouchRecording(ctx context.Context, meetingID string, durationMS int64, updatedAt string) error {
	return s.do(ctx, func(tx *txAdapter) error {
		var current string
		if err := tx.q.QueryRowContext(ctx, `SELECT updated_at FROM meetings WHERE meeting_id=? AND status='recording'`, meetingID).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return meetings.ErrNotRecording
			}
			return err
		}
		before, err := time.Parse(time.RFC3339Nano, current)
		if err != nil {
			return err
		}
		after, err := time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return err
		}
		if after.Before(before) {
			updatedAt = current
		}
		_, err = tx.q.ExecContext(ctx, `UPDATE meetings SET duration_ms=MAX(duration_ms,?), updated_at=? WHERE meeting_id=? AND status='recording'`, durationMS, updatedAt, meetingID)
		return err
	})
}

func (s *Store) GetMeeting(ctx context.Context, id string) (meetings.Meeting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT meeting_id, title, status, audio_source, started_at, ended_at, duration_ms, summary, actions, transcript, summary_error, created_at, updated_at, revision, transcript_revision, summary_source_revision, summary_source_digest, summary_source_title, summary_source_transcript, summary_edited FROM meetings WHERE meeting_id=?`, id)
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
	rows, err := s.db.QueryContext(ctx, `SELECT meeting_id, title, status, audio_source, started_at, ended_at, duration_ms, '', '', '', summary_error, created_at, updated_at, revision, transcript_revision, summary_source_revision, summary_source_digest, summary_source_title, '', summary_edited FROM meetings ORDER BY started_at DESC, meeting_id DESC LIMIT ?`, limit)
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

func (s *Store) DeleteSegments(ctx context.Context, meetingID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM meeting_segments WHERE meeting_id=?`, meetingID)
	return err
}

func (s *Store) CountSegments(ctx context.Context, meetingID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meeting_segments WHERE meeting_id=?`, meetingID).Scan(&n)
	return n, err
}

// LastSegment answers the append path, which only ever needs the tail. Reading
// it through ix_meeting_segments_meeting keeps a long meeting cheap: listing
// every segment on each append made recording quadratic against a 100k cap.
func (s *Store) LastSegment(ctx context.Context, meetingID string) (meetings.Segment, bool, error) {
	var seg meetings.Segment
	err := s.db.QueryRowContext(ctx, `SELECT segment_id, meeting_id, seq, started_ms, body, created_at FROM meeting_segments WHERE meeting_id=? ORDER BY seq DESC LIMIT 1`, meetingID).
		Scan(&seg.SegmentID, &seg.MeetingID, &seg.Seq, &seg.StartedMS, &seg.Text, &seg.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return meetings.Segment{}, false, nil
	}
	if err != nil {
		return meetings.Segment{}, false, err
	}
	return seg, true, nil
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
	return s.deleteMeeting(ctx, id, 0)
}

func (s *Store) DeleteMeetingVersion(ctx context.Context, id string, revision int64) error {
	if revision < 1 {
		return meetings.ErrConflict
	}
	return s.deleteMeeting(ctx, id, revision)
}

func (s *Store) deleteMeeting(ctx context.Context, id string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if revision > 0 {
		var actual int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM meetings WHERE meeting_id=?`, id).Scan(&actual); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return meetings.ErrNotFound
			}
			return err
		}
		if actual != revision {
			return meetings.ErrConflict
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meeting_segments WHERE meeting_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM meeting_docs WHERE meeting_id=?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM meetings WHERE meeting_id=? AND (?=0 OR revision=?)`, id, revision, revision)
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
	err := row.Scan(&m.MeetingID, &m.Title, &status, &m.AudioSource, &m.StartedAt, &m.EndedAt, &m.DurationMS, &m.Summary, &m.Actions, &m.Transcript, &m.SummaryError, &m.CreatedAt, &m.UpdatedAt, &m.Revision, &m.TranscriptRevision, &m.SummarySourceRevision, &m.SummarySourceDigest, &m.SummarySourceTitle, &m.SummarySourceTranscript, &m.SummaryEdited)
	m.Status = meetings.Status(status)
	return m, err
}
