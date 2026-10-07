package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type turnStatsAdapter struct{}

func (turnStatsAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}
func (turnStatsAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (turnStatsAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if err := emit(llmadapter.Delta{Text: "统计回答"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Usage: llmadapter.Usage{InputTokens: 50, OutputTokens: 4, TotalTokens: 54, CacheUsageReported: true}}, nil
}

func statsListPayload(t *testing.T, e *Engine, sid string) []byte {
	t.Helper()
	list := e.Handle(context.Background(), validRequest("message.list", `{"sessionId":"`+sid+`"}`))
	if !list.OK {
		t.Fatalf("message.list failed: %+v", list)
	}
	raw, err := json.Marshal(list.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunStreamPublishesTurnDurationAndPersistsStats(t *testing.T) {
	storeEngine, _, sid, _ := messageEngine(t)
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.messages = storeEngine.messages
	e.sessions = storeEngine.sessions
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e.tools = tools
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return turnStatsAdapter{}, nil
	})
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	events := make(chan bridge.Event, 32)
	go e.runStream(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model"}, func(event bridge.Event) error {
		events <- event
		return nil
	}, sid)
	var usageEvent *bridge.UsageEvent
	var terminal bridge.Event
	for terminal.Type == "" {
		event := <-events
		switch event.Type {
		case bridge.EventUsage:
			usageEvent = event.Usage
		case bridge.EventCompleted, bridge.EventFailed, bridge.EventCancelled:
			terminal = event
		}
	}
	if terminal.Type != bridge.EventCompleted {
		t.Fatalf("terminal=%s", terminal.Type)
	}
	if usageEvent == nil {
		t.Fatal("turn completed without a usage event")
	}
	if usageEvent.DurationMs <= 0 {
		t.Fatalf("usage event lacks turn duration: %+v", usageEvent)
	}
	if usageEvent.InputTokens != 50 || usageEvent.OutputTokens != 4 || usageEvent.TotalTokens != 54 {
		t.Fatalf("usage tokens drifted: %+v", usageEvent)
	}
	raw := statsListPayload(t, e, sid)
	if !strings.Contains(string(raw), `"turnStats"`) {
		t.Fatalf("history page missing turnStats: %s", raw)
	}
	var page struct {
		Items []struct {
			ID        string `json:"id"`
			Role      string `json:"role"`
			TurnStats *struct {
				DurationMs   int64  `json:"durationMs"`
				InputTokens  int64  `json:"inputTokens"`
				OutputTokens int64  `json:"outputTokens"`
				TotalTokens  int64  `json:"totalTokens"`
				Model        string `json:"model"`
			} `json:"turnStats"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	var assistantID string
	for _, item := range page.Items {
		if item.Role != "assistant" {
			continue
		}
		assistantID = item.ID
		if item.TurnStats == nil {
			continue
		}
		if item.TurnStats.InputTokens != 50 || item.TurnStats.OutputTokens != 4 || item.TurnStats.TotalTokens != 54 {
			t.Fatalf("persisted stats tokens drifted: %+v", item.TurnStats)
		}
		if item.TurnStats.DurationMs <= 0 {
			t.Fatalf("persisted stats lack duration: %+v", item.TurnStats)
		}
	}
	if assistantID == "" {
		t.Fatalf("no assistant row in page: %s", raw)
	}
	if !strings.Contains(string(raw), `"turnStats"`) {
		t.Fatalf("assistant row missing turnStats: %s", raw)
	}
	// The wire DTO must not leak the file-only messageId field.
	if strings.Contains(string(raw), `"messageId"`) {
		t.Fatalf("turnStats leaked file-only fields: %s", raw)
	}
	sessionFolder, err := tools.SessionFolder(sid)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(sessionFolder, e.messageStatsPath(sid, assistantID))
	if !strings.HasPrefix(rel, "..") {
		t.Fatalf("stats file changed workspace digest: %s", rel)
	}
}

func TestEnrichMessageTurnStatsSkipsCorruptAndMissing(t *testing.T) {
	e, _, sid, _ := messageEngine(t)
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	fresh := NewEngine(nil, "test")
	fresh.tools = tools
	fresh.messages = e.messages
	fresh.sessions = e.sessions
	ctx := context.Background()
	clean, err := e.messages.AppendAssistant(ctx, "stats-clean", "test", sid, "干净回答", messageapp.AssistantUsage{})
	if err != nil {
		t.Fatal(err)
	}
	corrupt, err := e.messages.AppendAssistant(ctx, "stats-corrupt", "test", sid, "损坏回答", messageapp.AssistantUsage{})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := e.messages.AppendAssistant(ctx, "stats-plain", "test", sid, "普通回答", messageapp.AssistantUsage{})
	if err != nil {
		t.Fatal(err)
	}
	fresh.saveMessageTurnStats(sid, clean.ID, messageTurnStatsFromUsage(clean.ID, 3200, llmadapter.Usage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120}, "model"))
	statsPath := fresh.messageStatsPath(sid, corrupt.ID)
	if err := os.MkdirAll(filepath.Dir(statsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statsPath, []byte(`{"messageId":"`+corrupt.ID+`","durationMs":-5`), 0600); err != nil {
		t.Fatal(err)
	}
	raw := statsListPayload(t, fresh, sid)
	body := string(raw)
	if !strings.Contains(body, `"turnStats"`) || !strings.Contains(body, `"durationMs":3200`) {
		t.Fatalf("clean stats missing from page: %s", raw)
	}
	if strings.Contains(body, `"durationMs":-5`) {
		t.Fatalf("corrupt stats leaked into page: %s", raw)
	}
	var page struct {
		Items []struct {
			ID        string `json:"id"`
			TurnStats *struct {
				DurationMs int64 `json:"durationMs"`
			} `json:"turnStats"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range page.Items {
		if item.TurnStats != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one stats row, got %d: %s", count, raw)
	}
	if _, err := os.Stat(fresh.messageStatsPath(sid, plain.ID)); !os.IsNotExist(err) {
		t.Fatal("plain row should have no stats file")
	}
}
