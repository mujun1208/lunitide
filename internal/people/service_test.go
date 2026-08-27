package people_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func testRoster(t *testing.T) (*people.Service, *identity.Service, *sqlitestore.Store) {
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
	t.Cleanup(roster.Close)
	return roster, ident, store
}

func TestInvisibleStatusDoesNotBroadcastOrIngest(t *testing.T) {
	roster, ident, _ := testRoster(t)
	invisible := identity.StatusInvisible
	if _, err := ident.Update(context.Background(), identity.ProfilePatch{Status: &invisible}); err != nil {
		t.Fatal(err)
	}
	if _, ok := roster.CurrentBeacon(); ok {
		t.Fatal("invisible identity must not produce a LAN beacon")
	}
	if err := roster.IngestBeacon(context.Background(), people.Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Nickname: "隐身同事", Status: "invisible",
	}, "10.0.0.9"); err != nil {
		t.Fatal(err)
	}
	items, err := roster.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Self {
		t.Fatalf("invisible peer must stay off the roster: %#v", items)
	}
}

func TestThreadUnreadClearsOnOpen(t *testing.T) {
	roster, ident, store := testRoster(t)
	ctx := context.Background()
	peerID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.IngestBeacon(ctx, people.Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: peerID,
		Nickname: "同事甲", Status: "online", OrgName: "月汐", Department: "研发",
		PairingHash: identity.PairingHash("654321", peerID),
	}, "10.0.0.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := roster.Pair(ctx, people.PairInput{PairingCode: "654321", SubjectID: peerID, Nickname: "同事甲"}); err != nil {
		t.Fatal(err)
	}
	thread, _, err := roster.OpenDirect(ctx, peerID)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	if err := store.InsertMessage(ctx, people.Message{
		MessageID: ulid.Make().String(), ThreadID: thread.ThreadID, SenderID: peerID,
		Kind: "text", Body: "在吗", CreatedAt: later,
	}, nil); err != nil {
		t.Fatal(err)
	}
	listed, err := roster.ListThreads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].UnreadCount != 1 || listed[0].LastMessage == nil || listed[0].LastMessage.Body != "在吗" {
		t.Fatalf("unread preview = %#v", listed)
	}
	opened, _, err := roster.OpenThread(ctx, thread.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if opened.UnreadCount != 0 {
		t.Fatalf("open must clear unread, got %d", opened.UnreadCount)
	}
	listed, err = roster.ListThreads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if listed[0].UnreadCount != 0 {
		t.Fatalf("list after open unread = %d", listed[0].UnreadCount)
	}
	if ident.Public().SubjectID == peerID {
		t.Fatal("peer collided with self")
	}
}
