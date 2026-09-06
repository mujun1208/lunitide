package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
)

func TestTalkUserFinalReplaySurvivesLegacyExpiryAndReopen(t *testing.T) {
	f := newMessageFixture(t, "retained-talk-user")
	ctx := context.Background()
	key := "talk-final:stable-provider-item"
	value := message.Message{SessionID: f.sessionID, Text: "已确认的一句话"}
	first, err := f.app.Append(ctx, key, "talk", value, value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.db.Exec(`UPDATE idempotency_records SET expires_at='2000-01-01T00:00:00Z' WHERE operation='message.append' AND idempotency_key=?`, key); err != nil {
		t.Fatal(err)
	}
	path := f.store.path
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service := newMessageApp(t, reopened)
	second, err := service.Append(ctx, key, "talk", value, value)
	if err != nil || first != second {
		t.Fatalf("expired replay: %#v %v", second, err)
	}
	value.Text = "同一来源换了文本"
	if _, err = service.Append(ctx, key, "talk", value, value); !errors.Is(err, messageapp.ErrIdempotencyConflict) {
		t.Fatalf("changed final accepted: %v", err)
	}
	if n := tableCount(t, reopened, "messages"); n != 1 {
		t.Fatalf("messages=%d", n)
	}
	if err = reopened.DeleteSession(ctx, f.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Append(ctx, key, "talk", value, value); err == nil {
		t.Fatal("deleted session was resurrected")
	}
}
