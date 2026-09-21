package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

// budgetPartialAdapter scripts a turn that does real tool work, then runs out
// of generation allowance, then is asked to wrap up.
type budgetPartialAdapter struct {
	calls        int
	sawReserve   bool
	reserveReply string
}

func (a *budgetPartialAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}

func (a *budgetPartialAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}

func (a *budgetPartialAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, budgetSummaryNudgeText) {
			a.sawReserve = true
			if a.reserveReply == "" {
				return llmadapter.Response{}, errors.New("reserve pass refused")
			}
			if err := emit(llmadapter.Delta{Text: a.reserveReply}); err != nil {
				return llmadapter.Response{}, err
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: a.reserveReply}}, nil
		}
	}
	a.calls++
	if a.calls == 1 {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	}
	// Spend the whole reply allowance so the loop is cut off mid-job.
	for range turnGenerationHardBytes/4096 + 1 {
		if err := emit(llmadapter.Delta{Text: strings.Repeat("x", 4096)}); err != nil {
			return llmadapter.Response{}, err
		}
	}
	return llmadapter.Response{}, nil
}

func runBudgetPartialTurn(t *testing.T, adapter *budgetPartialAdapter) (bridge.Event, string) {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-budget-partial"
	e.streams[id] = state
	events := make(chan bridge.Event, 64)
	done := make(chan struct{})
	var terminal bridge.Event
	var deltas strings.Builder
	go func() {
		defer close(done)
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas.WriteString(ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				terminal = ev
				return
			}
		}
	}()
	req := llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "把这批技能装好"}}}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	return terminal, deltas.String()
}

// Running out of allowance part-way through real work used to end the turn as a
// bare failure, so the user was told a limit was hit and nothing about the
// files that had already been written. The reserve buys one closing report.
func TestBudgetExhaustedAfterToolWorkReportsWhatLanded(t *testing.T) {
	adapter := &budgetPartialAdapter{reserveReply: "已经装好前 3 个技能，剩下 2 个没装。"}
	terminal, deltas := runBudgetPartialTurn(t, adapter)
	if !adapter.sawReserve {
		t.Fatal("no closing summary pass after the allowance ran out")
	}
	if terminal.Type != bridge.EventCompleted {
		t.Fatalf("turn with real finished work ended as %s (%#v)", terminal.Type, terminal.Error)
	}
	if !strings.Contains(deltas, "已经装好前 3 个技能") {
		t.Fatalf("closing report missing from the reply: %q", lastRunes(deltas, 240))
	}
	// The partial state stays visible: this is not passed off as a finished job.
	if !strings.Contains(deltas, "本轮生成预算已用完") || !strings.Contains(deltas, "继续") {
		t.Fatalf("partial-turn notice missing: %q", lastRunes(deltas, 240))
	}
}

// If even the reserve pass cannot produce a report, the turn must still fail
// loudly rather than close as if the job were done.
func TestBudgetExhaustedStillFailsWhenNoReportCanBeProduced(t *testing.T) {
	adapter := &budgetPartialAdapter{}
	terminal, _ := runBudgetPartialTurn(t, adapter)
	if !adapter.sawReserve {
		t.Fatal("closing summary was never attempted")
	}
	if terminal.Type != bridge.EventFailed {
		t.Fatalf("unreported budget exhaustion ended as %s", terminal.Type)
	}
	if terminal.Error == nil || terminal.Error.Code != "TURN_GENERATION_BUDGET_EXCEEDED" {
		t.Fatalf("terminal error: %#v", terminal.Error)
	}
}

func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}
