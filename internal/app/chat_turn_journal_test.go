package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type committedWithoutAck struct {
	MessageService
	loseAck bool
}

func (s *committedWithoutAck) AppendAssistant(ctx context.Context, turn, actor, session, text string, usage messageapp.AssistantUsage) (message.Message, error) {
	m, err := s.MessageService.AppendAssistant(ctx, turn, actor, session, text, usage)
	if err == nil && s.loseAck {
		s.loseAck = false
		return message.Message{}, errors.New("response lost after commit")
	}
	return m, err
}

func TestTurnJournalSurvivesRestartAndLostCommitAckWithoutDuplicates(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	store, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	e.SetChatTurnJournal(store)
	usage := messageapp.AssistantUsage{Provider: "openai_compatible", Model: "test-model", OutputTokens: 7}
	for i, id := range []string{"01ARZ3NDEKTSV4RRFFQ69G5FAA", "01ARZ3NDEKTSV4RRFFQ69G5FAB"} {
		text := []string{"first durable draft", "second durable draft"}[i]
		if err := e.saveTurnCheckpoint(sessionID, chatTurnCheckpoint{StreamID: id, Status: turnStatusInterrupted, PersistDraft: text, PersistFailed: true, PersistUsage: usage}); err != nil {
			t.Fatal(err)
		}
	}
	e.messages = &committedWithoutAck{MessageService: e.messages, loseAck: true}
	if _, err := e.retrySessionPersistDraft(context.Background(), sessionID); err == nil {
		t.Fatal("lost ACK was hidden")
	}
	pending, err := store.PendingChatTurns(context.Background(), sessionID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending drafts lost: %d %v", len(pending), err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	msgs, err := messageapp.New(reopened, reopened, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	next := NewEngine(nil, "test")
	next.messages = msgs
	next.SetChatTurnJournal(reopened)
	if id, err := next.retrySessionPersistDraft(context.Background(), sessionID); err != nil || id == "" {
		t.Fatalf("recovery: %q %v", id, err)
	}
	if id, err := next.retrySessionPersistDraft(context.Background(), sessionID); err != nil || id != "" {
		t.Fatalf("repeat recovery: %q %v", id, err)
	}
	res := next.Handle(context.Background(), validRequest("message.list", `{"sessionId":"`+sessionID+`"}`))
	if !res.OK {
		t.Fatalf("history: %#v", res)
	}
	page, err := msgs.List(context.Background(), messageapp.PageRequest{SessionID: sessionID})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("history duplicated or lost: %#v %v", page, err)
	}
	if page.Items[0].Text != "first durable draft" || page.Items[1].Text != "second durable draft" {
		t.Fatalf("turn order changed: %#v", page.Items)
	}
	latest := next.loadTurnCheckpoint(sessionID)
	if latest.PersistDraft != "" || latest.PersistFailed {
		t.Fatalf("completed draft still pending: %#v", latest)
	}
}

type unavailableTurnJournal struct{ ChatTurnJournal }

func TestWhitespaceRecoveryDraftIsClearedWithoutLooping(t *testing.T) {
	e, _, sessionID, path := messageEngine(t)
	store, err := storage.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e.SetChatTurnJournal(store)
	if err := e.saveTurnCheckpoint(sessionID, chatTurnCheckpoint{StreamID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", PersistDraft: " \n\t ", PersistFailed: true}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if id, err := e.retrySessionPersistDraft(ctx, sessionID); err != nil || id != "" {
		t.Fatalf("empty recovery blocked: %q %v", id, err)
	}
	if pending, err := store.PendingChatTurns(ctx, sessionID); err != nil || len(pending) != 0 {
		t.Fatalf("empty draft retained: %d %v", len(pending), err)
	}
}

func (unavailableTurnJournal) PendingChatTurns(context.Context, string) ([][]byte, error) {
	return nil, errors.New("storage unavailable")
}

func TestChatStartFailsBeforeModelWhenRecoveryStorageIsUnavailable(t *testing.T) {
	e := NewEngine(nil, "test")
	e.messages = &appendAssistantSpy{}
	e.SetChatTurnJournal(unavailableTurnJournal{})
	res := e.Handle(context.Background(), validRequest("chat.start", `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","sessionId":"`+chatAttachmentSessionID+`","messages":[{"role":"user","content":"next turn"}]}`))
	if res.OK || res.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("new turn bypassed failed recovery: %#v", res)
	}
	if !e.reserveChatSession(chatAttachmentSessionID) {
		t.Fatal("failed start leaked session reservation")
	}
}
