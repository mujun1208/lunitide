package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

type cancelledDraftAdapter struct {
	partialThenFailAdapter
	cancel func()
}

func (a cancelledDraftAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if err := emit(llmadapter.Delta{Text: "这段回复还没有播出而用户已经取消"}); err != nil {
		return llmadapter.Response{}, err
	}
	a.cancel()
	return llmadapter.Response{}, context.Canceled
}

func TestCancelledLiveDraftDoesNotReappearAfterReopen(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	journal, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	e.SetChatTurnJournal(journal)
	e.leases = streamTestLease{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := ulid.Make().String()
	state := &streamState{cancel: cancel, state: streamRunning, companion: true, sessionID: sessionID}
	e.streams[id] = state
	adapter := cancelledDraftAdapter{cancel: func() { e.cancelStream(id) }}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	e.runStream(ctx, id, state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "你好"}}}, func(bridge.Event) error { return nil }, sessionID)
	cp := e.loadTurnCheckpoint(sessionID)
	if cp.Status != turnStatusCancelled || cp.PersistDraft != "" || cp.PersistFailed || cp.PersistUsage != (messageapp.AssistantUsage{}) {
		t.Fatalf("cancelled draft retained: %#v", cp)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	e.SetChatTurnJournal(reopened)
	if id, err := e.retrySessionPersistDraft(context.Background(), sessionID); err != nil || id != "" {
		t.Fatalf("cancel restored: %q %v", id, err)
	}
	page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("unspoken history: %d %v", len(page.Items), err)
	}
}

type countingTurnJournal struct {
	ChatTurnJournal
	writes int
}

func (s *countingTurnJournal) PutChatTurn(context.Context, string, string, []byte, bool) error {
	s.writes++
	return nil
}

func TestLiveDraftWritesBoundByNewBytesOrElapsedTime(t *testing.T) {
	journal := &countingTurnJournal{}
	e := NewEngine(nil, "test")
	e.SetChatTurnJournal(journal)
	cp := chatTurnCheckpoint{StreamID: ulid.Make().String()}
	var last time.Time
	text := strings.Repeat("a", 100)
	if err := e.noteLiveTurnDraft(chatAttachmentSessionID, &cp, text, &last); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		text += "a"
		if err := e.noteLiveTurnDraft(chatAttachmentSessionID, &cp, text, &last); err != nil {
			t.Fatal(err)
		}
	}
	if journal.writes != 1 {
		t.Fatalf("per-delta full writes: %d", journal.writes)
	}
	text += strings.Repeat("b", 4096)
	if err := e.noteLiveTurnDraft(chatAttachmentSessionID, &cp, text, &last); err != nil {
		t.Fatal(err)
	}
	if journal.writes != 2 || cp.PersistDraft != text {
		t.Fatal("size checkpoint missing")
	}
	last = time.Now().Add(-time.Second)
	text += "tail"
	if err := e.noteLiveTurnDraft(chatAttachmentSessionID, &cp, text, &last); err != nil {
		t.Fatal(err)
	}
	if journal.writes != 3 || cp.PersistDraft != text {
		t.Fatal("time checkpoint missing")
	}
}

func TestDeletedSessionCannotExposeOrReimportLegacyTurnFile(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	ctx := context.Background()
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e.SetToolRuntime(runtime)
	cp := chatTurnCheckpoint{StreamID: ulid.Make().String(), Status: turnStatusInterrupted, PersistDraft: "旧版私密回复", PersistFailed: true}
	if err = e.saveTurnCheckpoint(sessionID, cp); err != nil {
		t.Fatal(err)
	}
	legacy := e.turnCheckpointPath(sessionID)
	store, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e.SetChatTurnJournal(store)
	if _, err = e.pendingTurnCheckpoints(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteSession(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(legacy); err != nil {
		t.Fatal("test requires legacy file still present")
	}
	response := e.Handle(ctx, validRequest("chat.turn.get", `{"sessionId":"`+sessionID+`"}`))
	if response.OK || response.Error == nil || response.Error.Code != "SESSION_NOT_FOUND" {
		t.Fatalf("deleted turn exposed: %#v", response)
	}
	if got := e.loadLegacyTurnCheckpoint(sessionID); got.PersistDraft != "" {
		t.Fatal("direct legacy fallback exposed deleted text")
	}
	if _, err = e.pendingTurnCheckpoints(ctx, sessionID); err == nil {
		t.Fatal("deleted legacy record imported")
	}
	raw, err := store.LatestChatTurn(ctx, sessionID)
	if err != nil || len(raw) != 0 {
		t.Fatalf("journal resurrected: %s %v", raw, err)
	}
}
