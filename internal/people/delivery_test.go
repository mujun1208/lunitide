package people_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type durableNode struct {
	service              *people.Service
	ident                *identity.Service
	store                *storage.Store
	path, receive, stage string
}

func newDurableNode(t *testing.T) *durableNode {
	t.Helper()
	n := &durableNode{path: filepath.Join(t.TempDir(), "delivery.db"), receive: t.TempDir(), stage: t.TempDir()}
	n.open(t)
	t.Cleanup(func() { n.service.Close(); n.store.Close() })
	return n
}
func (n *durableNode) open(t *testing.T) {
	t.Helper()
	var err error
	n.store, err = storage.OpenTemplated(context.Background(), n.path)
	if err != nil {
		t.Fatal(err)
	}
	n.ident = identity.New(n.store)
	if err = n.ident.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	n.service = people.New(n.store, n.ident, n.receive, n.stage)
	n.service.SetListenAddr("127.0.0.1:0")
	if err = n.service.StartTCP(); err != nil {
		t.Fatal(err)
	}
}
func pairDurable(t *testing.T, a, b *durableNode) people.Thread {
	t.Helper()
	ctx := context.Background()
	if _, err := a.service.AddPeer(ctx, b.service.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.service.AddPeer(ctx, a.service.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.service.Pair(ctx, people.PairInput{SubjectID: b.ident.SubjectID(), PairingCode: b.ident.Public().PairingCode}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.service.Pair(ctx, people.PairInput{SubjectID: a.ident.SubjectID(), PairingCode: a.ident.Public().PairingCode}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := a.service.OpenDirect(ctx, b.ident.SubjectID())
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

// The proxy forwards the authenticated hello and all payload bytes, then drops
// only replies. The receiver commits normally while the sender loses its ACK.
func dropAcknowledgements(t *testing.T, target string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			upstream, err := net.Dial("tcp", target)
			if err != nil {
				client.Close()
				continue
			}
			mu.Lock()
			conns = append(conns, client, upstream)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer client.Close()
				defer upstream.Close()
				copied := make(chan struct{})
				sentNonce := make(chan []byte, 1)
				go func() {
					for frameNo := 0; ; frameNo++ {
						var header [4]byte
						if _, err := io.ReadFull(client, header[:]); err != nil {
							break
						}
						size := binary.BigEndian.Uint32(header[:])
						if size > 96<<10 {
							break
						}
						body := make([]byte, size)
						if _, err := io.ReadFull(client, body); err != nil {
							break
						}
						if frameNo == 1 && len(body) >= 12 {
							sentNonce <- append([]byte(nil), body[:12]...)
						}
						if _, err := upstream.Write(header[:]); err != nil {
							break
						}
						if _, err := upstream.Write(body); err != nil {
							break
						}
					}
					upstream.Close()
					close(copied)
				}()
				var header [4]byte
				if _, err := io.ReadFull(upstream, header[:]); err == nil {
					size := binary.BigEndian.Uint32(header[:])
					if size < 96<<10 {
						payload := make([]byte, size)
						if _, err = io.ReadFull(upstream, payload); err == nil {
							client.Write(header[:])
							client.Write(payload)
							var ackHeader [4]byte
							if _, err := io.ReadFull(upstream, ackHeader[:]); err == nil {
								ackSize := binary.BigEndian.Uint32(ackHeader[:])
								if ackSize >= 12 && ackSize < 96<<10 {
									ack := make([]byte, ackSize)
									if _, err := io.ReadFull(upstream, ack); err == nil {
										select {
										case nonce := <-sentNonce:
											if bytes.Equal(nonce, ack[:12]) {
												t.Error("AES-GCM nonce reused in opposite directions")
											}
										default:
											t.Error("ACK preceded request")
										}
									}
								}
							}
							io.Copy(io.Discard, upstream)
						}
					}
				}
				client.Close()
				<-copied
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return ln.Addr().String()
}

func TestPeopleDeliveryLostACKAcrossDatabaseReopen(t *testing.T) {
	a, b := newDurableNode(t), newDurableNode(t)
	thread := pairDurable(t, a, b)
	ctx := context.Background()
	contact, err := a.store.GetContact(ctx, b.ident.SubjectID())
	if err != nil {
		t.Fatal(err)
	}
	realAddr := contact.HostAddr
	contact.HostAddr = dropAcknowledgements(t, b.service.LocalAddr())
	if err = a.store.UpsertContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	input := people.SendInput{RequestKey: "logical-send", ThreadID: thread.ThreadID, Kind: "text", Body: "中文消息"}
	sent, _, err := a.service.Send(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, err := b.store.GetPeopleMessage(ctx, sent.MessageID); return err == nil })
	pending, err := a.store.GetPeopleMessage(ctx, sent.MessageID)
	if err != nil || pending.DeliveryState != "pending" {
		t.Fatalf("false delivered ACK: %+v %v", pending, err)
	}
	a.service.Close()
	if err = a.store.Close(); err != nil {
		t.Fatal(err)
	}
	a.open(t)
	contact.HostAddr = realAddr
	if err = a.store.UpsertContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		got, err := a.store.GetPeopleMessage(ctx, sent.MessageID)
		return err == nil && got.DeliveryState == "delivered"
	})
	replay, _, err := a.service.Send(ctx, input)
	if err != nil || replay.MessageID != sent.MessageID || !replay.Replayed {
		t.Fatalf("send replay: %+v %v", replay, err)
	}
	list, err := b.service.ListThreads(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("receiver threads: %+v %v", list, err)
	}
	_, messages, err := b.service.OpenThread(ctx, list[0].ThreadID)
	if err != nil || len(messages) != 1 || messages[0].Body != input.Body {
		t.Fatalf("receiver duplicated: %+v %v", messages, err)
	}
	changed := input
	changed.Body = "different"
	if _, _, err = a.service.Send(ctx, changed); err == nil {
		t.Fatal("changed request reused key")
	}
}

func TestPeopleDeliveryEmojiLongUnicodeAndPerRecipientACK(t *testing.T) {
	a, b, c := newDurableNode(t), newDurableNode(t), newDurableNode(t)
	pairDurable(t, a, b)
	pairDurable(t, a, c)
	ctx := context.Background()
	group, err := a.service.CreateGroup(ctx, "delivery", a.ident.SubjectID(), []string{b.ident.SubjectID(), c.ident.SubjectID()})
	if err != nil {
		t.Fatal(err)
	}
	sent, _, err := a.service.Send(ctx, people.SendInput{ThreadID: group.ThreadID, Kind: "emoji", Body: strings.Repeat("🙂", 5000)})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		msg, err := a.store.GetPeopleMessage(ctx, sent.MessageID)
		return err == nil && msg.DeliveredCount == 2 && msg.RecipientCount == 2
	})
	for _, recipient := range []*durableNode{b, c} {
		msg, err := recipient.store.GetPeopleMessage(ctx, sent.MessageID)
		if err != nil || msg.Kind != "emoji" || len([]rune(msg.Body)) != 5000 {
			t.Fatalf("unicode delivery: %+v %v", msg, err)
		}
	}
}

func TestPeopleDeliveryPinsRecipientIdentityBeforePayload(t *testing.T) {
	a, b, c := newDurableNode(t), newDurableNode(t), newDurableNode(t)
	thread := pairDurable(t, a, b)
	pairDurable(t, a, c)
	ctx := context.Background()
	peer, err := a.store.GetContact(ctx, b.ident.SubjectID())
	if err != nil {
		t.Fatal(err)
	}
	peer.HostAddr = c.service.LocalAddr()
	if err = a.store.UpsertContact(ctx, peer); err != nil {
		t.Fatal(err)
	}
	sent, _, err := a.service.Send(ctx, people.SendInput{ThreadID: thread.ThreadID, Kind: "text", Body: "only for B"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		rows, err := a.store.PendingPeopleDeliveries(ctx, a.ident.SubjectID(), 100)
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.Message.MessageID == sent.MessageID && row.Attempts > 0 {
				return true
			}
		}
		return false
	})
	if _, err = c.store.GetPeopleMessage(ctx, sent.MessageID); err == nil {
		t.Fatal("wrong trusted recipient received payload")
	}
	msg, err := a.store.GetPeopleMessage(ctx, sent.MessageID)
	if err != nil || msg.DeliveryState != "pending" {
		t.Fatalf("wrong recipient acknowledged: %+v %v", msg, err)
	}
}
