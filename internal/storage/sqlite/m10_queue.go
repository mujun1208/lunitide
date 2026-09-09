// M10 queued-input storage (migration 0074): queued_user_messages rows on
// the Store connection. Every write is one audited transaction; seq is
// allocated inside the enqueue transaction (MAX+1, never recycled).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/queueinput"
)

// ErrQueuedMessageNotFound / ErrQueuedMessageSettled are matched by the
// queueapp service via errors.Is to map M10-QI failure codes.
var (
	ErrQueuedMessageNotFound = queueinput.ErrNotFound
	ErrQueuedMessageSettled  = queueinput.ErrSettled
)

// SessionExists reports whether the sessions row is present (queue writes
// fail closed for unknown sessions instead of relying on FK enforcement).
func (s *Store) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id=?`, sessionID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// EnqueueQueuedMessage atomically admits, deduplicates and audits one supplement.
func (s *Store) EnqueueQueuedMessage(ctx context.Context, sessionID, runID, payload, mark, requestID string) (queueinput.Message, error) {
	officeTaskID := queueinput.OfficeTaskID(ctx)
	var out queueinput.Message
	err := s.do(ctx, func(tx *txAdapter) error {
		now := time.Now().UTC()
		existing, err := scanQueued(tx.q.QueryRowContext(ctx, `SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND request_id=?`, sessionID, requestID))
		if err == nil {
			if existing.Payload != payload || existing.Mark != mark || existing.RunID != runID || existing.OfficeTaskID != officeTaskID || existing.Status != queueinput.StatusQueued {
				return queueinput.ErrRequestReused
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrQueuedMessageNotFound) {
			return err
		}
		var active, recent int
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND status='queued'`, sessionID, officeTaskID).Scan(&active); err != nil {
			return err
		}
		if active >= queueinput.MaxQueuedPerSession {
			return queueinput.ErrCapacity
		}
		if err := tx.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND created_at>=?`, sessionID, officeTaskID, formatTime(now.Add(-time.Minute))).Scan(&recent); err != nil {
			return err
		}
		if recent >= queueinput.MaxPerMinute {
			return queueinput.ErrRateLimited
		}
		id, err := s.newULID(now)
		if err != nil {
			return err
		}
		var seq int64
		if err := tx.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM queued_user_messages WHERE session_id=?`, sessionID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO queued_user_messages(id,session_id,run_id,office_task_id,seq,payload,status,mark,request_id,created_at,updated_at) VALUES(?,?,?,?,?,?,'queued',?,?,?,?)`, id, sessionID, nullableULID(runID), nullableULID(officeTaskID), seq, payload, mark, requestID, formatTime(now), formatTime(now)); err != nil {
			return err
		}
		out = queueinput.Message{ID: id, SessionID: sessionID, RunID: runID, OfficeTaskID: officeTaskID, Seq: seq, Payload: payload, Status: queueinput.StatusQueued, Mark: mark, RequestID: requestID, CreatedAt: formatTime(now), UpdatedAt: formatTime(now)}
		return s.appendAuditTx(ctx, tx.q, "queue.input", sessionID, "renderer", map[string]any{"mark": mark, "bytes": len(payload), "officeTaskId": officeTaskID})
	})
	if err != nil {
		return queueinput.Message{}, err
	}
	return out, nil
}

const queueColumns = `id,session_id,COALESCE(run_id,''),seq,payload,status,mark,request_id,COALESCE(consumed_at,''),created_at,updated_at,COALESCE(office_task_id,'')`

func scanQueued(row interface{ Scan(...any) error }) (queueinput.Message, error) {
	var m queueinput.Message
	err := row.Scan(&m.ID, &m.SessionID, &m.RunID, &m.Seq, &m.Payload, &m.Status, &m.Mark, &m.RequestID, &m.ConsumedAt, &m.CreatedAt, &m.UpdatedAt, &m.OfficeTaskID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrQueuedMessageNotFound
	}
	return m, err
}

// GetQueuedByRequest returns the row for one idempotency key or nil.
func (s *Store) GetQueuedByRequest(ctx context.Context, sessionID, requestID string) (queueinput.Message, error) {
	m, err := s.queryQueueOne(ctx,
		`SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND request_id=? AND COALESCE(office_task_id,'')=?`, sessionID, requestID, queueinput.OfficeTaskID(ctx))
	if errors.Is(err, ErrQueuedMessageNotFound) {
		return queueinput.Message{}, nil
	}
	return m, err
}

// GetQueuedByID returns one row regardless of status.
func (s *Store) GetQueuedByID(ctx context.Context, sessionID, id string) (queueinput.Message, error) {
	return s.queryQueueOne(ctx,
		`SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND id=? AND COALESCE(office_task_id,'')=?`, sessionID, id, queueinput.OfficeTaskID(ctx))
}

// CountQueued returns the number of rows still queued for the session.
func (s *Store) CountQueued(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND status='queued'`, sessionID, queueinput.OfficeTaskID(ctx)).Scan(&n)
	return n, err
}

// CountQueuedSince returns rows enqueued after the boundary (rate limit).
func (s *Store) CountQueuedSince(ctx context.Context, sessionID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND created_at>=?`, sessionID, queueinput.OfficeTaskID(ctx), formatTime(since)).Scan(&n)
	return n, err
}

// ListQueued returns queued rows of the session ordered by seq.
func (s *Store) ListQueued(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND status='queued' AND NOT EXISTS(SELECT 1 FROM queue_delivery_items WHERE queued_id=queued_user_messages.id) ORDER BY seq LIMIT ?`, sessionID, queueinput.OfficeTaskID(ctx), queueinput.MaxQueuedPerSession)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []queueinput.Message
	for rows.Next() {
		m, err := scanQueued(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// WithdrawQueuedMessage returns the exact row settled in its audit transaction.
func (s *Store) WithdrawQueuedMessage(ctx context.Context, sessionID, id string) (queueinput.Message, error) {
	var out queueinput.Message
	err := s.do(ctx, func(tx *txAdapter) error {
		row, err := scanQueued(tx.q.QueryRowContext(ctx, `SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND id=? AND COALESCE(office_task_id,'')=?`, sessionID, id, queueinput.OfficeTaskID(ctx)))
		if err != nil {
			return err
		}
		if !queueinput.ValidStatusTransition(row.Status, queueinput.StatusWithdrawn) {
			return ErrQueuedMessageSettled
		}
		var claimed bool
		if err := tx.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM queue_delivery_items WHERE queued_id=?)`, id).Scan(&claimed); err != nil {
			return err
		}
		if claimed {
			return queueinput.ErrSettled
		}
		row.Status = queueinput.StatusWithdrawn
		row.UpdatedAt = formatTime(time.Now().UTC())
		if _, err := tx.q.ExecContext(ctx, `UPDATE queued_user_messages SET status=?,updated_at=? WHERE session_id=? AND id=?`, row.Status, row.UpdatedAt, sessionID, id); err != nil {
			return err
		}
		out = row
		return s.appendAuditTx(ctx, tx.q, "queue.withdraw", id, "renderer", map[string]any{"sessionId": sessionID, "officeTaskId": row.OfficeTaskID})
	})
	if err != nil {
		return queueinput.Message{}, err
	}
	return out, nil
}

// ConsumeQueuedMessages captures exactly the rows transitioned by this call.
// Looking them up after commit by wall-clock timestamp can replay another batch.
func (s *Store) ConsumeQueuedMessages(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	var out []queueinput.Message
	err := s.do(ctx, func(tx *txAdapter) error {
		rows, err := tx.q.QueryContext(ctx, `SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND status='queued' AND NOT EXISTS(SELECT 1 FROM queue_delivery_items WHERE queued_id=queued_user_messages.id) ORDER BY seq`, sessionID, queueinput.OfficeTaskID(ctx))
		if err != nil {
			return err
		}
		defer rows.Close()
		at := formatTime(time.Now().UTC())
		for rows.Next() {
			row, err := scanQueued(rows)
			if err != nil {
				return err
			}
			row.Status = queueinput.StatusInjected
			row.ConsumedAt = at
			row.UpdatedAt = at
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(out) == 0 {
			return nil
		}
		if _, err := tx.q.ExecContext(ctx, `UPDATE queued_user_messages SET status='injected',consumed_at=?,updated_at=? WHERE session_id=? AND COALESCE(office_task_id,'')=? AND status='queued' AND NOT EXISTS(SELECT 1 FROM queue_delivery_items WHERE queued_id=queued_user_messages.id)`, at, at, sessionID, queueinput.OfficeTaskID(ctx)); err != nil {
			return err
		}
		return s.appendAuditTx(ctx, tx.q, "queue.consume", sessionID, "renderer", map[string]any{"sessionId": sessionID, "count": len(out), "officeTaskId": queueinput.OfficeTaskID(ctx)})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) queryQueueOne(ctx context.Context, query string, args ...any) (queueinput.Message, error) {
	return scanQueued(s.db.QueryRowContext(ctx, query, args...))
}

func nullableULID(v string) any {
	if v == "" {
		return nil
	}
	return v
}
