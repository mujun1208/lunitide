package people

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"
)

type Delivery struct {
	Message     Message
	RecipientID string
	Attempts    int
	FilePath    string
}

type OutgoingMessage struct {
	Message                     Message
	WireMessage                 Message
	Offer                       *FileOffer
	Recipients                  []string
	OwnerID, RequestKey, Digest string
}

// DeliveryStore commits the local message and every recipient's pending receipt
// together. Completion requires that recipient's authenticated durable ACK.
type DeliveryStore interface {
	EnqueuePeopleMessage(context.Context, OutgoingMessage) (Message, *FileOffer, error)
	ReplayPeopleSend(context.Context, string, string) (Message, *FileOffer, bool, error)
	PendingPeopleDeliveries(context.Context, string, int) ([]Delivery, error)
	FinishPeopleDelivery(context.Context, string, string, bool, string) error
	GetPeopleMessage(context.Context, string) (Message, error)
}

func sendDigest(in SendInput) string {
	in.RequestKey = ""
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) StartDelivery() {
	if _, ok := s.store.(DeliveryStore); !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.deliveryStarted {
		return
	}
	s.deliveryStarted = true
	go s.deliveryLoop()
}
func (s *Service) wakeDelivery() {
	s.StartDelivery()
	select {
	case s.deliveryWake <- struct{}{}:
	default:
	}
}
func (s *Service) deliveryLoop() {
	defer close(s.deliveryDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if s.readyUnlocked() == nil {
			s.drainDeliveries()
		}
		select {
		case <-s.deliveryCtx.Done():
			return
		case <-s.deliveryWake:
		case <-ticker.C:
		}
	}
}
func (s *Service) drainDeliveries() {
	store := s.store.(DeliveryStore)
	pending, err := store.PendingPeopleDeliveries(s.deliveryCtx, s.identity.SubjectID(), 32)
	if err != nil {
		return
	}
	work := make(chan Delivery, len(pending))
	for _, item := range pending {
		work <- item
	}
	close(work)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range work {
				if s.deliveryCtx.Err() != nil {
					return
				}
				err := s.deliverOne(d)
				next := time.Now().Add(time.Duration(1<<min(d.Attempts, 6)) * time.Second).UTC().Format(time.RFC3339Nano)
				// Losing this update is safe: retry uses the same message ID, and the peer
				// acknowledges an identical committed message without inserting it again.
				_ = store.FinishPeopleDelivery(s.deliveryCtx, d.Message.MessageID, d.RecipientID, err == nil, next)
			}
		}()
	}
	wg.Wait()
}

func (s *Service) deliverOne(d Delivery) error {
	if err := s.readyUnlocked(); err != nil {
		return err
	}
	ctx := s.deliveryCtx
	member, err := s.store.GetContact(ctx, d.RecipientID)
	if err != nil {
		return err
	}
	if member.Blocked || member.TrustState != "trusted" || strings.TrimSpace(member.HostAddr) == "" {
		return ErrNotTrusted
	}
	thread, err := s.store.GetThread(ctx, d.Message.ThreadID)
	if err != nil {
		return err
	}
	if !threadHasMember(thread, s.identity.SubjectID()) || !threadHasMember(thread, member.SubjectID) {
		return ErrNotTrusted
	}
	return s.pushTo(member, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
		if d.FilePath != "" && (d.Message.Kind == "file" || d.Message.Kind == "image") {
			if err := s.pushFile(conn, aead, seq, thread, d.Message, d.FilePath); err != nil {
				return err
			}
		} else {
			msg := d.Message
			encrypted, nonce, ok := s.sealBody(member.PublicKey, msg.Body)
			if !ok {
				return ErrNotTrusted
			}
			msg.Body = ""
			if err := writeEncFrame(conn, aead, seq, p2pFrame{Typ: "msg", V: 1, Thread: wireOf(thread), Message: &msg, BodyEnc: bodyEncV, BodyCipher: encrypted, BodyNonce: nonce}); err != nil {
				return err
			}
		}
		ack, err := readEncFrame(conn, aead)
		if err != nil {
			return err
		}
		if ack.Typ != "delivery-ack" || ack.Message == nil || ack.Message.MessageID != d.Message.MessageID || ack.SubjectID != member.SubjectID {
			return ErrInvalid
		}
		return nil
	})
}

// pushTo binds the handshake to the intended recipient, including file traffic.
// An address reused by another trusted peer must never receive this payload.
func (s *Service) pushTo(member Contact, fn func(net.Conn, cipher.AEAD, *uint64) error) error {
	return s.pushExpected(member.HostAddr, member.SubjectID, member.PublicKey, fn)
}

func (s *Service) enqueueMessage(ctx context.Context, t Thread, msg Message, offer *FileOffer, in SendInput) (Message, *FileOffer, error) {
	if store, ok := s.store.(DeliveryStore); ok {
		ids := []string{}
		for _, member := range t.Members {
			if member.SubjectID != s.identity.SubjectID() && member.TrustState != "self" && !IsAgentContact(member) {
				ids = append(ids, member.SubjectID)
			}
		}
		wire := msg
		wire.DestPath = ""
		wire.OfferStatus = ""
		wire.DeliveryState = ""
		wire.RecipientCount = 0
		wire.DeliveredCount = 0
		if msg.SenderID != s.identity.SubjectID() {
			author, err := s.store.GetContact(ctx, msg.SenderID)
			if err != nil || !IsAgentContact(author) {
				return Message{}, nil, ErrNotTrusted
			}
			wire.SenderID = s.identity.SubjectID()
			wire.Body = "[本机专家 " + author.Nickname + "]\n" + msg.Body
		}
		saved, file, err := store.EnqueuePeopleMessage(ctx, OutgoingMessage{Message: msg, WireMessage: wire, Offer: offer, Recipients: ids, OwnerID: s.identity.SubjectID(), RequestKey: in.RequestKey, Digest: sendDigest(in)})
		if err == nil {
			s.wakeDelivery()
		}
		return saved, file, err
	}
	if err := s.store.InsertMessage(ctx, msg, offer); err != nil {
		return Message{}, nil, err
	}
	path := ""
	if offer != nil {
		path = offer.StagingPath
	}
	go s.deliverMessage(t, msg, path)
	return msg, offer, nil
}

func (s *Service) acknowledgeIncoming(ctx context.Context, conn net.Conn, aead cipher.AEAD, seq *uint64, from Contact, id, offerID string) {
	store, ok := s.store.(DeliveryStore)
	if !ok {
		return
	}
	saved, err := store.GetPeopleMessage(ctx, id)
	if err != nil || saved.SenderID != from.SubjectID {
		return
	}
	if offerID != "" && saved.OfferID != offerID {
		return
	}
	_ = writeEncFrame(conn, aead, seq, p2pFrame{Typ: "delivery-ack", V: 1, SubjectID: s.identity.SubjectID(), Message: &Message{MessageID: id}})
}
