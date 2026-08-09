package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/providerapp"
)

// Claim atomically leases available events. BEGIN IMMEDIATE serializes competing
// workers; expired claims are eligible again after a worker crash.
func (s *Store) Claim(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]providerapp.ClaimedEvent, error) {
	return s.claim(ctx, "", owner, now, lease, limit)
}
func (s *Store) ClaimTopic(ctx context.Context, topic, owner string, now time.Time, lease time.Duration, limit int) ([]providerapp.ClaimedEvent, error) {
	if topic == "" || len(topic) > 128 {
		return nil, fmt.Errorf("invalid outbox topic")
	}
	return s.claim(ctx, topic, owner, now, lease, limit)
}
func (s *Store) claim(ctx context.Context, topic, owner string, now time.Time, lease time.Duration, limit int) ([]providerapp.ClaimedEvent, error) {
	if owner == "" || len(owner) > 128 || lease <= 0 || limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid outbox claim")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	// Pending poison events stop at the delivery limit, but an expired final
	// claim is deliberately reclaimable without increasing attempts. A worker
	// may have crashed before disposition (or after an uncertain side effect),
	// and leaving that row claimed forever would leak its cleanup obligation.
	query := `SELECT id FROM outbox_events WHERE ((status='pending' AND attempts<1000 AND available_at<=?) OR (status='claimed' AND lease_until<=?))`
	args := []any{formatTime(now), formatTime(now)}
	if topic != "" {
		query += ` AND topic=?`
		args = append(args, topic)
	}
	query += ` ORDER BY available_at,id LIMIT ?`
	args = append(args, limit)
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	until := formatTime(now.Add(lease))
	result := make([]providerapp.ClaimedEvent, 0, len(ids))
	for _, id := range ids {
		query = `UPDATE outbox_events SET status='claimed',lease_owner=?,lease_until=?,attempts=CASE WHEN attempts<1000 THEN attempts+1 ELSE attempts END WHERE id=? AND ((status='pending' AND attempts<1000 AND available_at<=?) OR (status='claimed' AND lease_until<=?))`
		updateArgs := []any{owner, until, id, formatTime(now), formatTime(now)}
		if topic != "" {
			query += ` AND topic=?`
			updateArgs = append(updateArgs, topic)
		}
		r, updateErr := conn.ExecContext(ctx, query, updateArgs...)
		if updateErr != nil {
			err = updateErr
			return nil, err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return nil, sql.ErrNoRows
		}
		var e providerapp.ClaimedEvent
		var payload, created string
		err = conn.QueryRowContext(ctx, `SELECT id,topic,aggregate_id,payload_json,created_at,attempts,lease_until FROM outbox_events WHERE id=?`, id).Scan(&e.ID, &e.Topic, &e.AggregateID, &payload, &created, &e.Attempts, &until)
		if err != nil {
			return nil, err
		}
		e.Payload = []byte(payload)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		e.LeaseUntil, _ = time.Parse(time.RFC3339Nano, until)
		result = append(result, e)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, mapWriteError(err)
	}
	return result, nil
}

func (s *Store) Complete(ctx context.Context, id, owner string, now time.Time) error {
	r, err := s.db.ExecContext(ctx, `UPDATE outbox_events SET status='completed',completed_at=?,lease_owner=NULL,lease_until=NULL,last_error=NULL WHERE id=? AND status='claimed' AND lease_owner=?`, formatTime(now), id, owner)
	if err != nil {
		return mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Retry(ctx context.Context, id, owner string, available time.Time, message string) error {
	if message == "" || len(message) > 2000 {
		return fmt.Errorf("invalid outbox retry error")
	}
	// The 1000th failed delivery becomes a terminal dead letter, so a poison
	// event cannot remain claimable or block later work.
	r, err := s.db.ExecContext(ctx, `UPDATE outbox_events SET status=CASE WHEN attempts>=1000 THEN 'dead_letter' ELSE 'pending' END,available_at=?,lease_owner=NULL,lease_until=NULL,last_error=?,completed_at=CASE WHEN attempts>=1000 THEN ? ELSE NULL END WHERE id=? AND status='claimed' AND lease_owner=?`, formatTime(available), message, formatTime(time.Now().UTC()), id, owner)
	if err != nil {
		return mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
