package app

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/talk"
	"testing"
)

type denyConversationScope struct{ err error }

func (d denyConversationScope) AuthorizeDataResource(context.Context, string, string, string) error {
	return d.err
}

func TestConversationLatePersistenceRechecksCurrentOrganizationScope(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	denied := errors.New("test: previous organization no longer bound")
	e.SetDataScopeStore(denyConversationScope{denied})
	if _, err := e.appendAssistantTurn(context.Background(), "old-turn", "chat", sessionID, "迟到模型结果", messageapp.AssistantUsage{}); !errors.Is(err, denied) {
		t.Fatalf("assistant: %v", err)
	}
	if err := e.saveTurnCheckpoint(sessionID, chatTurnCheckpoint{StreamID: "old-turn", PersistDraft: "旧组织草稿"}); !errors.Is(err, denied) {
		t.Fatalf("checkpoint: %v", err)
	}
	if _, err := e.pendingTurnCheckpoints(context.Background(), sessionID); !errors.Is(err, denied) {
		t.Fatalf("recovery read: %v", err)
	}
	if _, err := e.persistTalkTranscript(&talkSession{talkID: "old-talk", sessionID: sessionID}, talk.ServerEvent{Kind: "transcript", Final: true, Role: "user", ItemID: "old-final", Transcript: "旧组织语音"}); !errors.Is(err, denied) {
		t.Fatalf("talk: %v", err)
	}
	page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("scope rejection wrote messages: %#v %v", page, err)
	}
}
