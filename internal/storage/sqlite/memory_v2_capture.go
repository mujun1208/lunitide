package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func (s *Store) EnqueueMemoryCaptureJob(ctx context.Context, job m8core.MemoryCaptureJob, highWater int64) (bool, error) {
	return enqueueMemoryCaptureJob(ctx, s.db, job, highWater)
}

func (s *Store) ClaimMemoryCaptureJob(ctx context.Context, owner string, lease time.Duration) (m8core.MemoryCaptureJob, bool, error) {
	return claimMemoryCaptureJob(ctx, s.db, owner, lease)
}

func (s *Store) CompleteMemoryCaptureJob(ctx context.Context, jobID string, fence int64, errCode string) error {
	return completeMemoryCaptureJob(ctx, s.db, jobID, fence, errCode)
}

func (s *Store) CountActiveMemoryCaptureJobs(ctx context.Context) (int64, error) {
	return countActiveMemoryCaptureJobs(ctx, s.db)
}

func (s *Store) GetMemoryCaptureCursor(ctx context.Context, subjectID string) (m8core.MemoryCaptureCursor, error) {
	return getMemoryCaptureCursor(ctx, s.db, subjectID)
}

func (s *Store) SetMemoryCaptureCursor(ctx context.Context, cursor m8core.MemoryCaptureCursor) error {
	return setMemoryCaptureCursor(ctx, s.db, cursor)
}

func (s *Store) ListUserMemorySourcesAfter(ctx context.Context, afterMessageID string, limit int) ([]m8core.MemoryCaptureSource, error) {
	return listUserMemorySourcesAfter(ctx, s.db, afterMessageID, limit)
}

func (s *Store) ReclaimExpiredMemoryCaptureJobs(ctx context.Context, now time.Time) (int64, error) {
	return reclaimExpiredMemoryCaptureJobs(ctx, s.db, now)
}

func enqueueMemoryCaptureJob(ctx context.Context, db *sql.DB, job m8core.MemoryCaptureJob, highWater int64) (bool, error) {
	if highWater <= 0 {
		highWater = m8core.MemoryCaptureHighWatermark
	}
	if job.JobID == "" {
		job.JobID = ulid.Make().String()
	}
	if job.CursorJSON == "" {
		job.CursorJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if job.CreatedAt == "" {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.NextAttemptAt == "" {
		job.NextAttemptAt = now
	}
	if job.State == "" {
		job.State = "queued"
	}
	active, err := countActiveMemoryCaptureJobs(ctx, db)
	if err != nil {
		return false, err
	}
	if err = setMemoryCaptureCursor(ctx, db, m8core.MemoryCaptureCursor{
		SubjectID:       job.SubjectID,
		SourceMessageID: job.SourceMessageID,
		SourceRevision:  job.SourceRevision,
		CursorJSON:      job.CursorJSON,
		UpdatedAt:       now,
	}); err != nil {
		return false, err
	}
	if active >= highWater {
		return false, nil
	}
	res, err := db.ExecContext(ctx, `INSERT INTO memory_capture_jobs(
		job_id,subject_id,source_message_id,source_revision,source_digest,priority,state,attempt,next_attempt_at,cursor_json,fence,error_code,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,0,'',?,?)
		ON CONFLICT(subject_id,source_message_id,source_revision) DO NOTHING`,
		job.JobID, job.SubjectID, job.SourceMessageID, job.SourceRevision, job.SourceDigest, job.Priority, job.State, job.Attempt, job.NextAttemptAt, job.CursorJSON, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func countActiveMemoryCaptureJobs(ctx context.Context, db *sql.DB) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_capture_jobs WHERE state IN ('queued','running')`).Scan(&n)
	return n, err
}

func getMemoryCaptureCursor(ctx context.Context, db *sql.DB, subjectID string) (m8core.MemoryCaptureCursor, error) {
	var out m8core.MemoryCaptureCursor
	err := db.QueryRowContext(ctx, `SELECT subject_id,source_message_id,source_revision,cursor_json,updated_at
		FROM memory_capture_cursor WHERE subject_id=?`, subjectID).Scan(&out.SubjectID, &out.SourceMessageID, &out.SourceRevision, &out.CursorJSON, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryCaptureCursor{SubjectID: subjectID, CursorJSON: "{}"}, nil
	}
	return out, err
}

func setMemoryCaptureCursor(ctx context.Context, db *sql.DB, cursor m8core.MemoryCaptureCursor) error {
	if cursor.CursorJSON == "" {
		cursor.CursorJSON = "{}"
	}
	if cursor.UpdatedAt == "" {
		cursor.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := db.ExecContext(ctx, `INSERT INTO memory_capture_cursor(subject_id,source_message_id,source_revision,cursor_json,updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(subject_id) DO UPDATE SET
			source_message_id=excluded.source_message_id,
			source_revision=excluded.source_revision,
			cursor_json=excluded.cursor_json,
			updated_at=excluded.updated_at
		WHERE excluded.source_message_id>memory_capture_cursor.source_message_id
			OR (excluded.source_message_id=memory_capture_cursor.source_message_id AND excluded.source_revision>=memory_capture_cursor.source_revision)`,
		cursor.SubjectID, cursor.SourceMessageID, cursor.SourceRevision, cursor.CursorJSON, cursor.UpdatedAt)
	return err
}

func listUserMemorySourcesAfter(ctx context.Context, db *sql.DB, afterMessageID string, limit int) ([]m8core.MemoryCaptureSource, error) {
	if limit <= 0 || limit > m8core.MemoryCaptureScanLimit {
		limit = m8core.MemoryCaptureScanLimit
	}
	rows, err := db.QueryContext(ctx, `SELECT m.id,m.session_id,m.sequence,p.text
		FROM messages m
		JOIN message_parts p ON p.message_id=m.id AND p.ordinal=1 AND p.type='text'
		WHERE m.role='user' AND (?='' OR m.id>?)
		ORDER BY m.id LIMIT ?`, afterMessageID, afterMessageID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []m8core.MemoryCaptureSource
	for rows.Next() {
		var src m8core.MemoryCaptureSource
		var seq int64
		if err = rows.Scan(&src.MessageID, &src.SessionID, &seq, &src.Text); err != nil {
			return nil, err
		}
		src.Revision = strconv.FormatInt(seq, 10)
		src.Digest = m8core.DigestOf(src.Text)
		out = append(out, src)
	}
	return out, rows.Err()
}

func reclaimExpiredMemoryCaptureJobs(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	stamp := now.UTC().Format(time.RFC3339Nano)
	res, err := db.ExecContext(ctx, `UPDATE memory_capture_jobs
		SET state='queued', lease_owner=NULL, lease_until=NULL, updated_at=?
		WHERE state='running' AND lease_until IS NOT NULL AND lease_until<?`, stamp, stamp)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func claimMemoryCaptureJob(ctx context.Context, db *sql.DB, owner string, lease time.Duration) (m8core.MemoryCaptureJob, bool, error) {
	if lease <= 0 {
		lease = m8core.MemoryCaptureLease
	}
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	until := now.Add(lease).Format(time.RFC3339Nano)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return m8core.MemoryCaptureJob{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var job m8core.MemoryCaptureJob
	var leaseOwner, leaseUntil, heartbeat sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT job_id,subject_id,source_message_id,source_revision,source_digest,priority,state,attempt,next_attempt_at,cursor_json,lease_owner,lease_until,heartbeat_at,fence,error_code,created_at,updated_at
		FROM memory_capture_jobs
		WHERE state IN ('queued','running') AND next_attempt_at<=? AND (state='queued' OR lease_until IS NULL OR lease_until<?)
		ORDER BY priority DESC, next_attempt_at, job_id
		LIMIT 1`, stamp, stamp).Scan(
		&job.JobID, &job.SubjectID, &job.SourceMessageID, &job.SourceRevision, &job.SourceDigest, &job.Priority, &job.State, &job.Attempt, &job.NextAttemptAt, &job.CursorJSON,
		&leaseOwner, &leaseUntil, &heartbeat, &job.Fence, &job.ErrorCode, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.MemoryCaptureJob{}, false, tx.Commit()
	}
	if err != nil {
		return m8core.MemoryCaptureJob{}, false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE memory_capture_jobs
		SET state='running', attempt=attempt+1, lease_owner=?, lease_until=?, heartbeat_at=?, fence=fence+1, updated_at=?
		WHERE job_id=? AND (state='queued' OR lease_until IS NULL OR lease_until<?)`,
		owner, until, stamp, stamp, job.JobID, stamp)
	if err != nil {
		return m8core.MemoryCaptureJob{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return m8core.MemoryCaptureJob{}, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT attempt,fence,lease_owner,lease_until,heartbeat_at,state,updated_at FROM memory_capture_jobs WHERE job_id=?`, job.JobID).
		Scan(&job.Attempt, &job.Fence, &job.LeaseOwner, &job.LeaseUntil, &job.HeartbeatAt, &job.State, &job.UpdatedAt); err != nil {
		return m8core.MemoryCaptureJob{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return m8core.MemoryCaptureJob{}, false, err
	}
	return job, true, nil
}

func completeMemoryCaptureJob(ctx context.Context, db *sql.DB, jobID string, fence int64, errCode string) error {
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	if errCode == "" {
		_, err := db.ExecContext(ctx, `UPDATE memory_capture_jobs
			SET state='succeeded', lease_owner=NULL, lease_until=NULL, error_code='', updated_at=?
			WHERE job_id=? AND fence=? AND state='running'`, stamp, jobID, fence)
		return err
	}
	var attempt int64
	err := db.QueryRowContext(ctx, `SELECT attempt FROM memory_capture_jobs WHERE job_id=? AND fence=? AND state='running'`, jobID, fence).Scan(&attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	state := "queued"
	next := now.Add(captureRetryDelay(attempt)).Format(time.RFC3339Nano)
	if attempt >= m8core.MemoryCaptureMaxAttempts {
		state = "deferred"
		next = stamp
	}
	_, err = db.ExecContext(ctx, `UPDATE memory_capture_jobs
		SET state=?, next_attempt_at=?, lease_owner=NULL, lease_until=NULL, error_code=?, updated_at=?
		WHERE job_id=? AND fence=? AND state='running'`, state, next, errCode, stamp, jobID, fence)
	return err
}

func captureRetryDelay(attempt int64) time.Duration {
	idx := int(attempt - 1)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m8core.MemoryCaptureRetry) {
		idx = len(m8core.MemoryCaptureRetry) - 1
	}
	return m8core.MemoryCaptureRetry[idx]
}
