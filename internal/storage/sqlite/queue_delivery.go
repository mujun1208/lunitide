package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/queueapp"
)

const deliveryColumns = `id,session_id,consumer,state,stream_id,message_ids_json,created_at,updated_at`

// A receipt belongs to exactly one input scope. Checking every member also
// rejects a corrupt mixed receipt instead of exposing just a partial batch.
const deliveryOfficeScope = `EXISTS(SELECT 1 FROM queue_delivery_items WHERE delivery_id=queue_deliveries.id)
 AND NOT EXISTS(SELECT 1 FROM queue_delivery_items qi JOIN queued_user_messages qm ON qm.id=qi.queued_id
 WHERE qi.delivery_id=queue_deliveries.id AND (qm.session_id<>queue_deliveries.session_id OR COALESCE(qm.office_task_id,'')<>?))`

func getQueueDelivery(ctx context.Context, q sqlRunner, sessionID, id string) (queueapp.Delivery, error) {
	var d queueapp.Delivery
	var raw string
	err := q.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM queue_deliveries WHERE session_id=? AND id=? AND `+deliveryOfficeScope, sessionID, id, queueinput.OfficeTaskID(ctx)).Scan(&d.ID, &d.SessionID, &d.Consumer, &d.State, &d.StreamID, &raw, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, queueapp.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal([]byte(raw), &d.MessageIDs); err != nil {
		return d, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+queueColumns+` FROM queued_user_messages WHERE id IN (SELECT queued_id FROM queue_delivery_items WHERE delivery_id=?) ORDER BY seq`, id)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanQueued(rows)
		if err != nil {
			return d, err
		}
		d.Items = append(d.Items, item)
	}
	return d, rows.Err()
}

func (s *Store) GetQueueDelivery(ctx context.Context, sessionID, id string) (queueapp.Delivery, error) {
	return getQueueDelivery(ctx, s.db, sessionID, id)
}

func (s *Store) PendingQueueDelivery(ctx context.Context, sessionID string) (queueapp.Delivery, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM queue_deliveries WHERE session_id=? AND state<>'confirmed' AND `+deliveryOfficeScope+` ORDER BY created_at,id LIMIT 1`, sessionID, queueinput.OfficeTaskID(ctx)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return queueapp.Delivery{}, nil
	}
	if err != nil {
		return queueapp.Delivery{}, err
	}
	return getQueueDelivery(ctx, s.db, sessionID, id)
}

func (s *Store) ClaimQueueDelivery(ctx context.Context, sessionID, consumer string) (queueapp.Delivery, error) {
	if !message.CanonicalULID(sessionID) || (consumer != "renderer" && !message.CanonicalULID(consumer)) {
		return queueapp.Delivery{}, queueapp.ErrPayloadInvalid
	}
	var out queueapp.Delivery
	err := s.do(ctx, func(tx *txAdapter) error {
		var id string
		err := tx.q.QueryRowContext(ctx, `SELECT id FROM queue_deliveries WHERE session_id=? AND (state IN ('claimed','prepared') OR (?='renderer' AND state IN ('started','unknown'))) AND `+deliveryOfficeScope+` ORDER BY created_at,id LIMIT 1`, sessionID, consumer, queueinput.OfficeTaskID(ctx)).Scan(&id)
		if err == nil {
			out, err = getQueueDelivery(ctx, tx.q, sessionID, id)
			if err != nil {
				return err
			}
			if consumer != "renderer" && out.Consumer != consumer {
				return queueapp.ErrDeliveryBusy
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Older admission paths could exceed the per-session quota concurrently.
		// Preserve that backlog in bounded batches instead of returning a receipt
		// that cannot fit the Bridge contract.
		rows, err := tx.q.QueryContext(ctx, `SELECT `+queueColumns+` FROM queued_user_messages WHERE session_id=? AND COALESCE(office_task_id,'')=? AND status='queued' AND NOT EXISTS(SELECT 1 FROM queue_delivery_items WHERE queued_id=queued_user_messages.id) ORDER BY seq LIMIT ?`, sessionID, queueinput.OfficeTaskID(ctx), queueinput.MaxQueuedPerSession)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanQueued(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(out.Items) == 0 {
			return nil
		}
		now := time.Now().UTC()
		out.ID, err = s.newULID(now)
		if err != nil {
			return err
		}
		out.SessionID, out.Consumer, out.State = sessionID, consumer, "claimed"
		out.CreatedAt, out.UpdatedAt = formatTime(now), formatTime(now)
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO queue_deliveries(id,session_id,consumer,state,created_at,updated_at) VALUES(?,?,?,'claimed',?,?)`, out.ID, sessionID, consumer, out.CreatedAt, out.UpdatedAt); err != nil {
			return err
		}
		for _, item := range out.Items {
			if _, err := tx.q.ExecContext(ctx, `INSERT INTO queue_delivery_items(delivery_id,queued_id) VALUES(?,?)`, out.ID, item.ID); err != nil {
				return err
			}
		}
		return s.appendAuditTx(ctx, tx.q, "queue.consume", sessionID, "engine", map[string]any{"deliveryId": out.ID, "phase": "claimed", "count": len(out.Items), "officeTaskId": queueinput.OfficeTaskID(ctx)})
	})
	if err != nil {
		return queueapp.Delivery{}, err
	}
	return out, nil
}

func (s *Store) PrepareQueueDelivery(ctx context.Context, sessionID, id string, messageIDs []string) (queueapp.Delivery, error) {
	var out queueapp.Delivery
	err := s.do(ctx, func(tx *txAdapter) error {
		var err error
		out, err = getQueueDelivery(ctx, tx.q, sessionID, id)
		if err != nil {
			return err
		}
		if out.State != "claimed" && out.State != "prepared" {
			return queueapp.ErrDeliveryBusy
		}
		parts := queueapp.DeliveryParts(out)
		if len(parts) != len(messageIDs) {
			return queueapp.ErrPayloadInvalid
		}
		var prior int64
		for i, mid := range messageIDs {
			value, err := tx.Message(ctx, mid)
			if err != nil {
				return err
			}
			if value.SessionID != sessionID || value.Role != message.RoleUser || value.Text != parts[i].Text || value.Sequence <= prior {
				return queueapp.ErrPayloadInvalid
			}
			prior = value.Sequence
		}
		raw, err := json.Marshal(messageIDs)
		if err != nil {
			return err
		}
		out.State, out.MessageIDs, out.UpdatedAt = "prepared", append([]string{}, messageIDs...), formatTime(time.Now().UTC())
		_, err = tx.q.ExecContext(ctx, `UPDATE queue_deliveries SET state='prepared',message_ids_json=?,updated_at=? WHERE id=?`, string(raw), out.UpdatedAt, id)
		return err
	})
	if err != nil {
		return queueapp.Delivery{}, err
	}
	return out, nil
}

func (s *Store) StartQueueDelivery(ctx context.Context, sessionID, id, streamID string) error {
	if !message.CanonicalULID(streamID) {
		return queueapp.ErrPayloadInvalid
	}
	return s.do(ctx, func(tx *txAdapter) error {
		d, err := getQueueDelivery(ctx, tx.q, sessionID, id)
		if err != nil {
			return err
		}
		if d.State != "prepared" {
			return queueapp.ErrDeliveryBusy
		}
		if d.Consumer != "renderer" && d.Consumer != streamID {
			return queueapp.ErrDeliveryBusy
		}
		at := formatTime(time.Now().UTC())
		if _, err := tx.q.ExecContext(ctx, `UPDATE queue_deliveries SET state='started',stream_id=?,updated_at=? WHERE id=?`, streamID, at, id); err != nil {
			return err
		}
		if _, err := tx.q.ExecContext(ctx, `UPDATE queued_user_messages SET status='injected',consumed_at=?,updated_at=? WHERE id IN (SELECT queued_id FROM queue_delivery_items WHERE delivery_id=?) AND status='queued'`, at, at, id); err != nil {
			return err
		}
		return s.appendAuditTx(ctx, tx.q, "queue.consume", sessionID, "engine", map[string]any{"deliveryId": id, "phase": "started", "streamId": streamID, "officeTaskId": queueinput.OfficeTaskID(ctx)})
	})
}

func (s *Store) FinishQueueDeliveries(ctx context.Context, sessionID, streamID string, success bool) error {
	state := "unknown"
	if success {
		state = "confirmed"
	}
	return s.do(ctx, func(tx *txAdapter) error {
		res, err := tx.q.ExecContext(ctx, `UPDATE queue_deliveries SET state=?,updated_at=? WHERE session_id=? AND stream_id=? AND state='started' AND `+deliveryOfficeScope, state, formatTime(time.Now().UTC()), sessionID, streamID, queueinput.OfficeTaskID(ctx))
		if err != nil {
			return err
		}
		count, err := res.RowsAffected()
		if err != nil || count == 0 {
			return err
		}
		return s.appendAuditTx(ctx, tx.q, "queue.consume", sessionID, "engine", map[string]any{"phase": state, "streamId": streamID, "deliveries": count, "officeTaskId": queueinput.OfficeTaskID(ctx)})
	})
}

// Recovery is an explicit acknowledgement, never an automatic replay of work.
// mark-unknown is used only by the engine after ruling out a live owner.
func (s *Store) RecoverQueueDelivery(ctx context.Context, sessionID, id, action string) (queueapp.Delivery, error) {
	var out queueapp.Delivery
	err := s.do(ctx, func(tx *txAdapter) error {
		var err error
		out, err = getQueueDelivery(ctx, tx.q, sessionID, id)
		if err != nil {
			return err
		}
		switch action {
		case "handoff":
			if out.State != "claimed" {
				return queueapp.ErrDeliveryBusy
			}
			out.Consumer = "renderer"
		case "mark-unknown":
			if out.State == "confirmed" || out.State == "unknown" {
				return nil
			}
			out.State = "unknown"
		case "resume":
			if (out.State == "claimed" || out.State == "prepared") && out.Consumer == "renderer" && out.StreamID == "" {
				return nil
			}
			if out.State != "unknown" {
				return queueapp.ErrDeliveryBusy
			}
			out.State, out.Consumer, out.StreamID = "claimed", "renderer", ""
		case "dismiss":
			if out.State == "confirmed" {
				return nil
			}
			if out.State != "unknown" && out.State != "claimed" && out.State != "prepared" {
				return queueapp.ErrDeliveryBusy
			}
			out.State = "confirmed"
		default:
			return queueapp.ErrPayloadInvalid
		}
		out.UpdatedAt = formatTime(time.Now().UTC())
		if _, err = tx.q.ExecContext(ctx, `UPDATE queue_deliveries SET state=?,consumer=?,stream_id=?,updated_at=? WHERE id=?`, out.State, out.Consumer, out.StreamID, out.UpdatedAt, id); err != nil {
			return err
		}
		if action == "dismiss" {
			if _, err = tx.q.ExecContext(ctx, `UPDATE queued_user_messages SET status='injected',consumed_at=?,updated_at=? WHERE id IN (SELECT queued_id FROM queue_delivery_items WHERE delivery_id=?) AND status='queued'`, out.UpdatedAt, out.UpdatedAt, id); err != nil {
				return err
			}
		}
		return s.appendAuditTx(ctx, tx.q, "queue.consume", sessionID, "engine", map[string]any{"deliveryId": id, "phase": action, "officeTaskId": queueinput.OfficeTaskID(ctx)})
	})
	if err != nil {
		return queueapp.Delivery{}, err
	}
	return out, nil
}
