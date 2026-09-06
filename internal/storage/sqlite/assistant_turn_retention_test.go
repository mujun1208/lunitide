package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/oklog/ulid/v2"
)

func TestAssistantTurnReplaySurvivesLegacyExpiryAndReopen(t *testing.T) {
	f := newMessageFixture(t, "retained-assistant")
	ctx := context.Background()
	usage := messageapp.AssistantUsage{Provider: "provider", Model: "model", OutputTokens: 17}
	first, err := f.app.AppendAssistant(ctx, "durable-turn", "test", f.sessionID, "committed answer", usage)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an old 24-hour record after a committed write whose ACK was lost.
	if _, err = f.store.db.Exec(`UPDATE idempotency_records SET expires_at='2000-01-01T00:00:00Z' WHERE operation='message.append-assistant' AND idempotency_key='durable-turn'`); err != nil {
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
	second, err := service.AppendAssistant(ctx, "durable-turn", "test", f.sessionID, "committed answer", usage)
	if err != nil || second != first {
		t.Fatalf("expired replay duplicated or lost the message: %#v %v", second, err)
	}
	if _, err = service.AppendAssistant(ctx, "durable-turn", "test", f.sessionID, "changed answer", usage); !errors.Is(err, messageapp.ErrIdempotencyConflict) {
		t.Fatalf("expired identity was reusable: %v", err)
	}
	if got := tableCount(t, reopened, "messages"); got != 1 {
		t.Fatalf("messages=%d", got)
	}
	if got := tableCount(t, reopened, "token_ledger"); got != 2 {
		t.Fatalf("token entries=%d", got)
	}
	var reported int64
	if err = reopened.db.QueryRow(`SELECT COALESCE(SUM(token_count),0) FROM token_ledger WHERE provider='provider'`).Scan(&reported); err != nil || reported != 17 {
		t.Fatalf("usage duplicated: %d %v", reported, err)
	}
	if err = reopened.DeleteSession(ctx, f.sessionID); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err = reopened.db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE operation='message.append-assistant'`).Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("deleted message retained private response: %d %v", retained, err)
	}
}

func TestMessageRewindAbandonsRecoveryDraftInSameTransaction(t *testing.T) {
	f := newMessageFixture(t, "rewind-journal")
	ctx := context.Background()
	user := f.append(t, "user-before-rewind", "question")
	turnID := ulid.Make().String()
	if err := f.store.PutChatTurn(ctx, f.sessionID, turnID, []byte(`{"status":"interrupted","persistDraft":"`+strings.Repeat("abandoned answer", 30000)+`","persistFailed":true,"persistUsage":{"outputTokens":17}}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.Rewind(ctx, "rewind-with-draft", "test", f.sessionID, user.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := f.store.PendingChatTurns(ctx, f.sessionID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("rewound draft can be resurrected: %s %v", pending, err)
	}
	raw, err := f.store.LatestChatTurn(ctx, f.sessionID)
	var cp map[string]any
	if err != nil || json.Unmarshal(raw, &cp) != nil || cp["persistDraft"] != nil || cp["persistUsage"] != nil || cp["status"] != "cancelled" {
		t.Fatalf("rewind lost tombstone or retained text: %s %v", raw, err)
	}
	var parts int
	if err = f.store.db.QueryRowContext(ctx, `SELECT count(*) FROM chat_turn_checkpoint_parts WHERE turn_id=?`, turnID).Scan(&parts); err != nil || parts != 0 {
		t.Fatalf("rewind retained parts: %d %v", parts, err)
	}
}
