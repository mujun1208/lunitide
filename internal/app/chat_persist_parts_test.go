package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

type failSecondAssistantPart struct {
	MessageService
	calls int
}

func (s *failSecondAssistantPart) AppendAssistant(ctx context.Context, key, actor, session, text string, usage messageapp.AssistantUsage) (message.Message, error) {
	s.calls++
	if s.calls == 2 {
		return message.Message{}, errors.New("disk temporarily unavailable")
	}
	return s.MessageService.AppendAssistant(ctx, key, actor, session, text, usage)
}

func TestOversizedTurnRecoversAllPartsAfterReopenAndCountsUsageOnce(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	ctx := context.Background()
	store, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	e.SetChatTurnJournal(store)
	text := strings.Repeat("🙂", 33000) + "\r\n最后的文字"
	turnID := ulid.Make().String()
	usage := messageapp.AssistantUsage{Provider: "openai_compatible", Model: "parts-test", OutputTokens: 37}
	if err = e.saveTurnCheckpoint(sessionID, chatTurnCheckpoint{StreamID: turnID, Status: turnStatusInterrupted, PersistDraft: text, PersistUsage: usage, PersistFailed: true}); err != nil {
		t.Fatal(err)
	}
	e.messages = &failSecondAssistantPart{MessageService: e.messages}
	if _, err = e.retrySessionPersistDraft(ctx, sessionID); err == nil {
		t.Fatal("partial commit failure hidden")
	}
	pending, err := store.PendingChatTurns(ctx, sessionID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("source lost after partial commit: %d %v", len(pending), err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	messages, err := messageapp.New(reopened, reopened, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	next := NewEngine(nil, "test")
	next.messages = messages
	next.SetChatTurnJournal(reopened)
	lastID, err := next.retrySessionPersistDraft(ctx, sessionID)
	if err != nil || lastID == "" {
		t.Fatalf("oversize recovery blocked: %q %v", lastID, err)
	}
	page, err := messages.List(ctx, messageapp.PageRequest{SessionID: sessionID, Direction: messageapp.Forward, Limit: 64, ByteBudget: messageapp.MaxByteBudget})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.Items[2].ID != lastID {
		t.Fatalf("parts missing/duplicated: %d", len(page.Items))
	}
	var joined strings.Builder
	for _, m := range page.Items {
		if utf8.RuneCountInString(m.Text) > message.MaxRunesAssistant || len(m.Text) > message.MaxBytesAssistant {
			t.Fatal("part exceeds frozen storage limit")
		}
		joined.WriteString(m.Text)
	}
	if joined.String() != strings.ReplaceAll(text, "\r\n", "\n") {
		t.Fatal("reply truncated or reordered")
	}
	if _, err = next.retrySessionPersistDraft(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, tokens int
	if err = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(token_count),0) FROM token_ledger WHERE provider='openai_compatible' AND model='parts-test'`).Scan(&count, &tokens); err != nil {
		t.Fatal(err)
	}
	if count != 1 || tokens != 37 {
		t.Fatalf("provider usage duplicated across parts: %d %d", count, tokens)
	}
	pending, err = reopened.PendingChatTurns(ctx, sessionID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("session remains blocked: %d %v", len(pending), err)
	}
}
