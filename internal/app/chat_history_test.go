package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestCompanionVisitKeepsCurrentTurnsWithoutReplayingEarlierVisit(t *testing.T) {
	for _, includeCurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(includeCurrent), func(t *testing.T) {
			requests := make(chan llmadapter.Request, 1)
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			rows := []contextapp.Message{
				{ID: "old-u", Sequence: 1, Role: "user", Content: "上次的私人话题不应主动带入"},
				{ID: "old-a", Sequence: 2, Role: "assistant", Content: "上次的旧回答"},
			}
			if includeCurrent {
				rows = append(rows, contextapp.Message{ID: "new-u", Sequence: 3, Role: "user", Content: "本次准备去图书馆"}, contextapp.Message{ID: "new-a", Sequence: 4, Role: "assistant", Content: "本次图书馆安排"})
			}
			e.messageReader = decisionHistoryReader{rows: rows}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
				return chatAttachmentAdapter{requests: requests}, nil
			})
			payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","companion":true,"companionHistoryAfter":2,"messages":[{"role":"user","content":"你好，现在聊一下"}]}`
			response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
			if !response.OK {
				t.Fatalf("fresh voice entry: %+v", response.Error)
			}
			req := capturedChatRequest(t, requests)
			all := ""
			for _, message := range req.Messages {
				all += message.Content
			}
			if strings.Contains(all, "上次的私人话题") || strings.Contains(all, "上次的旧回答") {
				t.Fatal("previous visit leaked into voice context")
			}
			if !strings.Contains(all, "你好，现在聊一下") {
				t.Fatal("current prompt lost")
			}
			if includeCurrent && !strings.Contains(all, "本次准备去图书馆") {
				t.Fatal("current visit continuity lost")
			}
		})
	}
}

type decisionHistoryReader struct {
	emptyCompanionReader
	rows []contextapp.Message
}

func (r decisionHistoryReader) ListMessages(_ context.Context, _ string, direction string, _ int) ([]contextapp.Message, error) {
	rows := append([]contextapp.Message(nil), r.rows...)
	if direction == "backward" {
		slices.Reverse(rows)
	}
	return rows, nil
}

func TestChatStartDecisionHistoryReachesProvider(t *testing.T) {
	for _, failedReply := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval before assistant persisted", true: "interrupted assistant records"}[failedReply], func(t *testing.T) {
			requests := make(chan llmadapter.Request, 1)
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			rows := []contextapp.Message{{ID: "first", Sequence: 1, Role: "user", Content: "帮我确定部署方案"}}
			if failedReply {
				rows = append(rows, contextapp.Message{ID: "a1", Sequence: 2, Role: "assistant", Content: "正在确认需求"}, contextapp.Message{ID: "a2", Sequence: 3, Role: "assistant", Content: "上次回答中断"})
			}
			rows = append(rows, contextapp.Message{ID: "tool", Sequence: 4, Role: "tool", Content: "[tool-result callId=ask argsDigest=private resultDigest=private]\n用户已提交决策。"}, contextapp.Message{ID: "decision", Sequence: 5, Role: "user", Content: "部署方式：容器化，请继续实施"})
			e.messageReader = decisionHistoryReader{rows: rows}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
				return chatAttachmentAdapter{requests: requests}, nil
			})
			payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `"}`
			response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
			if !response.OK {
				t.Fatalf("decision continuation: %+v", response.Error)
			}
			req := capturedChatRequest(t, requests)
			var all string
			for _, m := range req.Messages {
				all += m.Content
				if m.Role == llmadapter.RoleTool {
					t.Fatal("orphan protocol tool row")
				}
			}
			if strings.Contains(all, "argsDigest=") || !strings.Contains(all, "容器化") || !strings.Contains(all, "用户已提交决策") {
				t.Fatalf("decision context lost or bookkeeping leaked: %s", all)
			}
		})
	}
}
