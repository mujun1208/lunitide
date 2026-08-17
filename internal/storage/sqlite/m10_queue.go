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
	ErrQueuedMessageNotFound = errors.New("queued message not found")
	ErrQueuedMessageSettled  = errors.New("queued message already settled")
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

// EnqueueQueuedMessage inserts one queued row; the service layer owns the
// idempotency check, capacity and rate limits.
func (s *Store) EnqueueQueuedMessage(ctx context.Context, sessionID, runID, payload, mark, requestID string) (queueinput.Message, error) {
	now := time.Now().UTC()
	id, err := s.newULID(now)
	if err != nil {
		return queueinput.Message{}, err
	}
	err = s.execWithAudit(ctx, "queue.input", sessionID, "renderer",
		map[string]any{"mark": mark, "bytes": len(payload)},
		func(tx *sql.Tx) error {
			var seq int64
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(seq),0)+1 FROM queued_user_messages WHERE session_id=?`, sessionID).Scan(&seq); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT INTO queued_user_messages(id, session_id, run_id, seq, payload, status, mark, request_id, created_at, updated_at)
				 VALUES(?,?,?,?,?,'queued',?,?,?,?)`,
				id, sessionID, nullableULID(runID), seq, payload, mark, requestID, formatTime(now), formatTime(now))
			return err
		})
	if err != nil {
		return queueinput.Message{}, mapWriteError(err)
	}
	return s.GetQueuedByID(ctx, sessionID, id)
}

// GetQueuedByRequest returns the row for one idempotency key or nil.
func (s *Store) GetQueuedByRequest(ctx context.Context, sessionID, requestID string) (queueinput.Message, error) {
	m, err := s.queryQueueOne(ctx,
		`SELECT id, session_id, COALESCE(run_id,''), seq, payload, status, mark, request_id, COALESCE(consumed_at,''), created_at, updated_at
		 FROM queued_user_messages WHERE session_id=? AND request_id=?`, sessionID, requestID)
	if errors.Is(err, ErrQueuedMessageNotFound) {
		return queueinput.Message{}, nil
	}
	return m, err
}

// GetQueuedByID returns one row regardless of status.
func (s *Store) GetQueuedByID(ctx context.Context, sessionID, id string) (queueinput.Message, error) {
	return s.queryQueueOne(ctx,
		`SELECT id, session_id, COALESCE(run_id,''), seq, payload, status, mark, request_id, COALESCE(consumed_at,''), created_at, updated_at
		 FROM queued_user_messages WHERE session_id=? AND id=?`, sessionID, id)
}

// CountQueued returns the number of rows still queued for the session.
func (s *Store) CountQueued(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queued_user_messages WHERE session_id=? AND status='queued'`, sessionID).Scan(&n)
	return n, err
}

// CountQueuedSince returns rows enqueued after the boundary (rate limit).
func (s *Store) CountQueuedSince(ctx context.Context, sessionID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM queued_user_messages WHERE session_id=? AND created_at>=?`, sessionID, formatTime(since)).Scan(&n)
	return n, err
}

// ListQueued returns queued rows of the session ordered by seq.
func (s *Store) ListQueued(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, COALESCE(run_id,''), seq, payload, status, mark, request_id, COALESCE(consumed_at,''), created_at, updated_at
		 FROM queued_user_messages WHERE session_id=? AND status='queued' ORDER BY seq`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []queueinput.Message
	for rows.Next() {
		var m queueinput.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.RunID, &m.Seq, &m.Payload, &m.Status, &m.Mark, &m.RequestID, &m.ConsumedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// WithdrawQueuedMessage settles one queued row as withdrawn.
func (s *Store) WithdrawQueuedMessage(ctx context.Context, sessionID, id string) (queueinput.Message, error) {
	now := time.Now().UTC()
	err := s.execWithAudit(ctx, "queue.withdraw", id, "renderer",
		map[string]any{"sessionId": sessionID},
		func(tx *sql.Tx) error {
			return transitionQueueRow(ctx, tx, sessionID, id, queueinput.StatusWithdrawn, now)
		})
	if err != nil {
		return queueinput.Message{}, mapWriteError(err)
	}
	return s.GetQueuedByID(ctx, sessionID, id)
}

// ConsumeQueuedMessages settles every queued row of the session as
// injected and returns them in seq order (empty slice when idle).
func (s *Store) ConsumeQueuedMessages(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	now := time.Now().UTC()
	err := s.execWithAudit(ctx, "queue.consume", sessionID, "renderer",
		map[string]any{"sessionId": sessionID},
		func(tx *sql.Tx) error {
			res, err := tx.ExecContext(ctx,
				`UPDATE queued_user_messages SET status='injected', consumed_at=?, updated_at=?
				 WHERE session_id=? AND status='queued'`, formatTime(now), formatTime(now), sessionID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil
			}
			return nil
		})
	if err != nil {
		return nil, mapWriteError(err)
	}
	return s.listInjectedAt(ctx, sessionID, formatTime(now))
}

func (s *Store) listInjectedAt(ctx context.Context, sessionID, consumedAt string) ([]queueinput.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, COALESCE(run_id,''), seq, payload, status, mark, request_id, COALESCE(consumed_at,''), created_at, updated_at
		 FROM queued_user_messages WHERE session_id=? AND status='injected' AND consumed_at=? ORDER BY seq`, sessionID, consumedAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []queueinput.Message
	for rows.Next() {
		var m queueinput.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.RunID, &m.Seq, &m.Payload, &m.Status, &m.Mark, &m.RequestID, &m.ConsumedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func transitionQueueRow(ctx context.Context, tx *sql.Tx, sessionID, id, to string, now time.Time) error {
	var from string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM queued_user_messages WHERE session_id=? AND id=?`, sessionID, id).Scan(&from); err != nil {
		if err == sql.ErrNoRows {
			return ErrQueuedMessageNotFound
		}
		return err
	}
	if !queueinput.ValidStatusTransition(from, to) {
		return ErrQueuedMessageSettled
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE queued_user_messages SET status=?, updated_at=? WHERE session_id=? AND id=? AND status=?`,
		to, formatTime(now), sessionID, id, from)
	return err
}

func (s *Store) queryQueueOne(ctx context.Context, query string, args ...any) (queueinput.Message, error) {
	var m queueinput.Message
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&m.ID, &m.SessionID, &m.RunID, &m.Seq, &m.Payload, &m.Status, &m.Mark, &m.RequestID, &m.ConsumedAt, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return queueinput.Message{}, ErrQueuedMessageNotFound
	}
	if err != nil {
		return queueinput.Message{}, err
	}
	return m, nil
}

func nullableULID(v string) any {
	if v == "" {
		return nil
	}
	return v
}
