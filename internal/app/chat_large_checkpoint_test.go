package app

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/messageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestLegacyCheckpointAboveOneMiBRecoversAllTextAfterReopen(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	ctx := context.Background()
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e.SetToolRuntime(runtime)
	text := strings.Repeat("🙂 &正文\n", 100000) + "最后一段"
	cp := chatTurnCheckpoint{StreamID: ulid.Make().String(), Status: turnStatusInterrupted, PersistDraft: text, PersistFailed: true, PersistUsage: messageapp.AssistantUsage{Provider: "openai_compatible", Model: "legacy-large", OutputTokens: 19}}
	if err = e.saveTurnCheckpoint(sessionID, cp); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	e.SetChatTurnJournal(store)
	pending, err := e.pendingTurnCheckpoints(ctx, sessionID)
	if err != nil || len(pending) != 1 || pending[0].PersistDraft != text {
		t.Fatalf("large legacy import: %d %v", len(pending), err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e.SetChatTurnJournal(store)
	if _, err = e.retrySessionPersistDraft(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.retrySessionPersistDraft(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var restored string
	if err = db.QueryRow(`SELECT group_concat(text,'') FROM (SELECT p.text FROM messages m JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=? AND m.role='assistant' ORDER BY m.sequence,p.ordinal)`, sessionID).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != text {
		t.Fatalf("large reply truncated/duplicated: got %d want %d", len(restored), len(text))
	}
	var parts, usage int
	if err = db.QueryRow(`SELECT count(*) FROM chat_turn_checkpoint_parts WHERE turn_id=?`, cp.StreamID).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM token_ledger WHERE model='legacy-large' AND provider='openai_compatible'`).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if parts != 0 || usage != 1 {
		t.Fatalf("cleanup/usage: parts=%d usage=%d", parts, usage)
	}
	if pending, err = e.pendingTurnCheckpoints(ctx, sessionID); err != nil || len(pending) != 0 {
		t.Fatalf("next turn blocked: %d %v", len(pending), err)
	}
}
