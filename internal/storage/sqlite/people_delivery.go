package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/people"
)

const peopleMessageSelect = `SELECT m.message_id,m.thread_id,m.sender_subject_id,m.kind,m.body,m.file_name,m.file_mime,m.file_size,m.file_sha256,m.created_at,COALESCE(o.offer_id,''),COALESCE(o.status,''),COALESCE(NULLIF(o.dest_path,''),o.staging_path,''),
 (SELECT count(*) FROM people_delivery_outbox d WHERE d.message_id=m.message_id),
 (SELECT count(*) FROM people_delivery_outbox d WHERE d.message_id=m.message_id AND d.state='delivered')
 FROM people_messages m LEFT JOIN people_file_offers o ON o.message_id=m.message_id`

func scanPeopleMessage(row rowScanner) (people.Message, error) {
	var m people.Message
	err := row.Scan(&m.MessageID, &m.ThreadID, &m.SenderID, &m.Kind, &m.Body, &m.FileName, &m.FileMIME, &m.FileSize, &m.FileSHA256, &m.CreatedAt, &m.OfferID, &m.OfferStatus, &m.DestPath, &m.RecipientCount, &m.DeliveredCount)
	if errors.Is(err, sql.ErrNoRows) {
		return m, people.ErrNotFound
	}
	if m.RecipientCount > 0 {
		m.DeliveryState = "pending"
		if m.DeliveredCount == m.RecipientCount {
			m.DeliveryState = "delivered"
		}
	}
	return m, err
}
func (s *Store) GetPeopleMessage(ctx context.Context, id string) (people.Message, error) {
	return scanPeopleMessage(s.db.QueryRowContext(ctx, peopleMessageSelect+` WHERE m.message_id=?`, id))
}

func (t *txAdapter) replayPeopleSend(ctx context.Context, key, digest string) (people.Message, *people.FileOffer, bool, error) {
	if key == "" {
		return people.Message{}, nil, false, nil
	}
	var saved, id string
	err := t.q.QueryRowContext(ctx, `SELECT request_digest,message_id FROM people_send_requests WHERE request_key=?`, key).Scan(&saved, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Message{}, nil, false, nil
	}
	if err != nil {
		return people.Message{}, nil, false, err
	}
	if saved != digest {
		return people.Message{}, nil, true, people.ErrInvalid
	}
	msg, err := scanPeopleMessage(t.q.QueryRowContext(ctx, peopleMessageSelect+` WHERE m.message_id=?`, id))
	if err != nil {
		return msg, nil, true, err
	}
	msg.Replayed = true
	var offer *people.FileOffer
	if msg.OfferID != "" {
		offer = &people.FileOffer{}
		err = t.q.QueryRowContext(ctx, `SELECT offer_id,message_id,thread_id,from_subject_id,to_subject_id,status,file_name,file_mime,file_size,file_sha256,staging_path,dest_path,created_at,decided_at FROM people_file_offers WHERE offer_id=?`, msg.OfferID).Scan(&offer.OfferID, &offer.MessageID, &offer.ThreadID, &offer.FromID, &offer.ToID, &offer.Status, &offer.FileName, &offer.FileMIME, &offer.FileSize, &offer.FileSHA256, &offer.StagingPath, &offer.DestPath, &offer.CreatedAt, &offer.DecidedAt)
	}
	return msg, offer, true, err
}
func (s *Store) ReplayPeopleSend(ctx context.Context, key, digest string) (people.Message, *people.FileOffer, bool, error) {
	var msg people.Message
	var offer *people.FileOffer
	var found bool
	err := s.do(ctx, func(t *txAdapter) error {
		var err error
		msg, offer, found, err = t.replayPeopleSend(ctx, key, digest)
		return err
	})
	return msg, offer, found, err
}
func (s *Store) EnqueuePeopleMessage(ctx context.Context, input people.OutgoingMessage) (people.Message, *people.FileOffer, error) {
	msg, offer, recipients, key, digest := input.Message, input.Offer, input.Recipients, input.RequestKey, input.Digest
	var out people.Message
	var file *people.FileOffer
	err := s.do(ctx, func(t *txAdapter) error {
		var ownerMember int
		if err := t.q.QueryRowContext(ctx, `SELECT count(*) FROM people_thread_members WHERE thread_id=? AND subject_id=?`, msg.ThreadID, input.OwnerID).Scan(&ownerMember); err != nil {
			return err
		}
		if ownerMember != 1 || input.WireMessage.SenderID != input.OwnerID || input.WireMessage.MessageID != msg.MessageID || input.WireMessage.ThreadID != msg.ThreadID {
			return people.ErrNotTrusted
		}
		wireJSON, err := json.Marshal(input.WireMessage)
		if err != nil {
			return err
		}
		saved, savedFile, found, err := t.replayPeopleSend(ctx, key, digest)
		if err != nil {
			return err
		}
		if found {
			out = saved
			file = savedFile
			return nil
		}
		if err = t.insertPeopleMessage(ctx, msg, offer); err != nil {
			return err
		}
		for _, id := range recipients {
			if id == msg.SenderID {
				continue
			}
			res, err := t.q.ExecContext(ctx, `INSERT INTO people_delivery_outbox(message_id,recipient_id,owner_id,message_json,state,next_attempt_at) SELECT ?,?,?,?,'pending',? WHERE EXISTS(SELECT 1 FROM people_thread_members WHERE thread_id=? AND subject_id=?)`, msg.MessageID, id, input.OwnerID, string(wireJSON), msg.CreatedAt, msg.ThreadID, id)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return people.ErrNotTrusted
			}
		}
		if key != "" {
			if _, err = t.q.ExecContext(ctx, `INSERT INTO people_send_requests(request_key,request_digest,message_id,created_at)VALUES(?,?,?,?)`, key, digest, msg.MessageID, msg.CreatedAt); err != nil {
				return err
			}
		}
		out, err = scanPeopleMessage(t.q.QueryRowContext(ctx, peopleMessageSelect+` WHERE m.message_id=?`, msg.MessageID))
		file = offer
		return err
	})
	return out, file, err
}
func (s *Store) PendingPeopleDeliveries(ctx context.Context, self string, limit int) ([]people.Delivery, error) {
	if limit < 1 || limit > 100 {
		limit = 32
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.message_json,d.recipient_id,d.attempts,COALESCE(o.staging_path,'') FROM people_delivery_outbox d JOIN people_messages m ON m.message_id=d.message_id LEFT JOIN people_file_offers o ON o.message_id=m.message_id WHERE d.state='pending' AND d.next_attempt_at<=? AND d.owner_id=? ORDER BY d.next_attempt_at,d.message_id LIMIT ?`, time.Now().UTC().Format(time.RFC3339Nano), self, limit)
	if err != nil {
		return nil, err
	}
	var out []people.Delivery
	for rows.Next() {
		var d people.Delivery
		var wireJSON string
		if err = rows.Scan(&wireJSON, &d.RecipientID, &d.Attempts, &d.FilePath); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(wireJSON), &d.Message); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) FinishPeopleDelivery(ctx context.Context, id, recipient string, delivered bool, next string) error {
	state := "pending"
	var at any
	if delivered {
		state = "delivered"
		at = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE people_delivery_outbox SET state=?,attempts=attempts+1,next_attempt_at=?,delivered_at=? WHERE message_id=? AND recipient_id=? AND state='pending'`, state, next, at, id, recipient)
	return err
}
