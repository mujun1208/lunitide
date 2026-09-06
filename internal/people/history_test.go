package people_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/oklog/ulid/v2"
)

func TestPeopleHistoryReadsEveryLargeMessageWithoutEnvelopeOverflow(t *testing.T) {
	a, b := newDurableNode(t), newDurableNode(t)
	thread := pairDurable(t, a, b)
	ctx := context.Background()
	for range 450 {
		msg := people.Message{MessageID: ulid.Make().String(), ThreadID: thread.ThreadID, SenderID: a.ident.SubjectID(), Kind: "text", Body: strings.Repeat("中", 1500), CreatedAt: "2026-09-06T12:00:00Z"}
		if err := a.store.InsertMessage(ctx, msg, nil); err != nil {
			t.Fatal(err)
		}
	}
	_, page, err := a.service.OpenThread(ctx, thread.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for len(page) > 0 {
		raw, _ := json.Marshal(page)
		if len(raw) > 512<<10 || len(page) > 200 {
			t.Fatalf("page overflow %d bytes / %d messages", len(raw), len(page))
		}
		for _, item := range page {
			if seen[item.MessageID] {
				t.Fatal("cursor duplicated a message")
			}
			seen[item.MessageID] = true
			if len([]rune(item.Body)) != 1500 {
				t.Fatal("message truncated")
			}
		}
		_, page, err = a.service.History(ctx, thread.ThreadID, page[0].MessageID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 450 {
		t.Fatalf("history lost messages: %d", len(seen))
	}
	if _, _, err := b.service.History(ctx, thread.ThreadID, ulid.Make().String()); err == nil {
		t.Fatal("foreign cursor accepted")
	}
	if _, err := a.ident.SetPassword(ctx, "local-history-lock", ""); err != nil {
		t.Fatal(err)
	}
	locked := identity.New(a.store)
	if err := locked.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	service := people.New(a.store, locked, t.TempDir(), t.TempDir())
	defer service.Close()
	if _, _, err := service.OpenThread(ctx, thread.ThreadID); err != people.ErrLocked {
		t.Fatalf("locked history exposed: %v", err)
	}
}
