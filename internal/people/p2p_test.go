package people_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func node(t *testing.T) (*people.Service, *identity.Service) {
	t.Helper()
	store, err := sqlitestore.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "people.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ident := identity.New(store)
	if err := ident.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	roster := people.New(store, ident, t.TempDir(), t.TempDir())
	roster.SetListenAddr("127.0.0.1:0")
	if err := roster.StartTCP(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(roster.Close)
	return roster, ident
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for LAN delivery")
}

func TestP2PTextAndConfirmedFile(t *testing.T) {
	a, identA := node(t)
	b, identB := node(t)
	ctx := context.Background()
	if _, err := a.AddPeer(ctx, b.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddPeer(ctx, a.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pair(ctx, people.PairInput{PairingCode: identB.Public().PairingCode, SubjectID: identB.SubjectID()}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Pair(ctx, people.PairInput{PairingCode: identA.Public().PairingCode, SubjectID: identA.SubjectID()}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := a.OpenDirect(ctx, identB.SubjectID())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Send(ctx, people.SendInput{ThreadID: thread.ThreadID, Kind: "text", Body: "在吗"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		listed, err := b.ListThreads(ctx)
		return err == nil && len(listed) == 1 && listed[0].LastMessage != nil && listed[0].LastMessage.Body == "在吗"
	})
	src := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(src, []byte("payload-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	sent, offer, err := a.Send(ctx, people.SendInput{ThreadID: thread.ThreadID, Kind: "file", FileName: "secret.txt", LocalPath: src})
	if err != nil || offer == nil {
		t.Fatalf("send file: %v %#v", err, offer)
	}
	if sent.FileSize != 13 {
		t.Fatalf("file size %d", sent.FileSize)
	}
	var pending people.Message
	waitFor(t, func() bool {
		listed, err := b.ListThreads(ctx)
		if err != nil || len(listed) == 0 || listed[0].LastMessage == nil {
			return false
		}
		if listed[0].LastMessage.Kind == "file" && listed[0].LastMessage.OfferStatus == "pending" {
			pending = *listed[0].LastMessage
			return true
		}
		return false
	})
	opened, msgs, err := b.OpenThread(ctx, pending.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	var offerID string
	for _, msg := range msgs {
		if msg.OfferID != "" && msg.OfferStatus == "pending" {
			offerID = msg.OfferID
		}
	}
	if offerID == "" {
		t.Fatalf("missing pending offer in %#v %#v", opened, msgs)
	}
	decided, err := b.DecideFile(ctx, offerID, true)
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != "accepted" || decided.DestPath == "" {
		t.Fatalf("decide = %#v", decided)
	}
	body, err := os.ReadFile(decided.DestPath)
	if err != nil || string(body) != "payload-bytes" {
		t.Fatalf("dest %q %v", body, err)
	}
}

func TestP2PBlockedPeerDoesNotReceive(t *testing.T) {
	a, identA := node(t)
	b, identB := node(t)
	ctx := context.Background()
	if _, err := a.AddPeer(ctx, b.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddPeer(ctx, a.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pair(ctx, people.PairInput{PairingCode: identB.Public().PairingCode, SubjectID: identB.SubjectID()}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Pair(ctx, people.PairInput{PairingCode: identA.Public().PairingCode, SubjectID: identA.SubjectID()}); err != nil {
		t.Fatal(err)
	}
	blocked := true
	if _, err := b.UpdateContact(ctx, identA.SubjectID(), people.ContactPatch{Blocked: &blocked}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := a.OpenDirect(ctx, identB.SubjectID())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Send(ctx, people.SendInput{ThreadID: thread.ThreadID, Kind: "text", Body: "不该送到"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	listed, err := b.ListThreads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("blocked peer still received: %#v", listed)
	}
}

func TestRemarkAndTyping(t *testing.T) {
	a, identA := node(t)
	b, identB := node(t)
	ctx := context.Background()
	if _, err := a.AddPeer(ctx, b.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Pair(ctx, people.PairInput{PairingCode: identB.Public().PairingCode, SubjectID: identB.SubjectID()}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddPeer(ctx, a.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Pair(ctx, people.PairInput{PairingCode: identA.Public().PairingCode, SubjectID: identA.SubjectID()}); err != nil {
		t.Fatal(err)
	}
	remark := "阿甲"
	updated, err := a.UpdateContact(ctx, identB.SubjectID(), people.ContactPatch{Remark: &remark})
	if err != nil || updated.Remark != "阿甲" {
		t.Fatalf("remark = %#v %v", updated, err)
	}
	thread, _, err := a.OpenDirect(ctx, identB.SubjectID())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.NoteTyping(ctx, thread.ThreadID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		listed, err := b.ListThreads(ctx)
		return err == nil && len(listed) == 1 && len(listed[0].TypingSubjectIDs) == 1
	})
}
