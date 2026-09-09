package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/talk"
)

func TestTalkUserFinalReusesLegacyPartReceiptAfterTypedLimitIncrease(t *testing.T) {
	e, _, sessionID, _ := messageEngine(t)
	sess := &talkSession{talkID: "legacy-final", sessionID: sessionID}
	text := strings.Repeat("保留🙂", 1500) + "尾部不要丢失"
	event := talk.ServerEvent{Kind: "transcript", Final: true, Role: "user", ItemID: "legacy-item", Transcript: text}
	key, err := talkTranscriptKey(sess, event)
	if err != nil {
		t.Fatal(err)
	}
	firstText := string([]rune(text)[:2048])
	hash := sha256.Sum256([]byte(text))
	request := struct {
		SessionID        string `json:"sessionId"`
		Text             string `json:"text"`
		TranscriptDigest string `json:"transcriptDigest,omitempty"`
	}{sessionID, firstText, hex.EncodeToString(hash[:])}
	legacy, err := e.messages.Append(context.Background(), key, "talk", request, message.Message{SessionID: sessionID, Text: firstText})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := e.persistTalkTranscript(sess, event)
	if err != nil || resumed.ID != legacy.ID {
		t.Fatalf("pre-upgrade final receipt no longer replays: %v", err)
	}
	page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sessionID, Direction: messageapp.Forward})
	if err != nil {
		t.Fatal(err)
	}
	var recovered strings.Builder
	for _, m := range page.Items {
		recovered.WriteString(m.Text)
	}
	if recovered.String() != text || len(page.Items) != 3 {
		t.Fatal("legacy retry duplicated or lost transcript")
	}
}

func TestTalkLargeFinalSplitsWithoutLossAndKeepsFirstHandoffIdentity(t *testing.T) {
	for _, role := range []string{"user", "assistant"} {
		t.Run(role, func(t *testing.T) {
			e, _, sessionID, _ := messageEngine(t)
			session := &talkSession{talkID: "large-final", sessionID: sessionID, providerProtocol: "openai_compatible", modelID: "realtime"}
			// Exceed both the independently bounded user and assistant parts.
			text := strings.Repeat("完整语音🙂正文。", 4500) + "尾段不能丢"
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
