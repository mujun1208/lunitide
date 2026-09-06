package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/talk"
)

func TestTalkLargeFinalSplitsWithoutLossAndKeepsFirstHandoffIdentity(t *testing.T) {
	for _, role := range []string{"user", "assistant"} {
		t.Run(role, func(t *testing.T) {
			e, _, sessionID, _ := messageEngine(t)
			session := &talkSession{talkID: "large-final", sessionID: sessionID, providerProtocol: "openai_compatible", modelID: "realtime"}
			text := strings.Repeat("完整语音🙂正文。", 3500) + "尾段不能丢"
			event := talk.ServerEvent{Kind: "transcript", Final: true, Role: role, ItemID: "long-source-item", Transcript: text}
			first, err := e.persistTalkTranscript(session, event)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := e.persistTalkTranscript(session, event)
			if err != nil || replay.ID != first.ID {
				t.Fatalf("replay: %v", err)
			}
			page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID, Direction: messageapp.Forward, Limit: 64, ByteBudget: messageapp.MaxByteBudget})
			if err != nil {
				t.Fatal(err)
			}
			assembled := ""
			for _, m := range page.Items {
				assembled += m.Text
			}
			if assembled != text || len(page.Items) < 2 || page.Items[0].ID != first.ID {
				t.Fatalf("lost/split/duplicated transcript: bytes=%d parts=%d", len(assembled), len(page.Items))
			}
			event.Transcript = text + "同标识追加伪造内容"
			if _, err = e.persistTalkTranscript(session, event); !errors.Is(err, messageapp.ErrIdempotencyConflict) {
				t.Fatalf("changed full identity accepted: %v", err)
			}
		})
	}
}
