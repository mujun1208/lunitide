// P1-2 app-level coverage: a chat turn whose model asks for command.run
// must stream bounded tool_output events between tool_started and
// tool_completed (full-access full-disk path, no approval gate).
package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// commandProgressAdapter asks for `go version` in one turn, then finishes.
type commandProgressAdapter struct{ turn int }

func (a *commandProgressAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *commandProgressAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *commandProgressAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if a.turn == 0 {
		a.turn++
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-cmd", Name: "command.run", Arguments: []byte(`{"argv":["go","version"]}`)},
		}}}, nil
	}
	if err := emit(gateway.Delta{Text: "command done"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Usage: gateway.Usage{OutputTokens: 2, TotalTokens: 2}}, nil
}

func TestCommandRunStreamsToolOutputEvents(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	// Full-disk opt-in: full-access mode runs go version without the
	// approval gate so the streaming path executes inline in one turn.
	if err = tools.SetCommandPolicyJSON([]byte(`{"commands":[],"fullAccess":true}`)); err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(tools)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return &commandProgressAdapter{}, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-cmd-progress"
	e.streams[id] = state
	events := make(chan bridge.Event, 32)
	var order []string
	done := make(chan struct{})
	go func() {
		for ev := range events {
			switch ev.Type {
			case bridge.EventToolStarted:
				order = append(order, "started")
			case bridge.EventToolOutput:
				order = append(order, "output:"+ev.Tool.Summary)
			case bridge.EventToolCompleted:
				order = append(order, "completed")
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	req := gateway.Request{Model: "m"}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error { events <- event; return nil }, "01ARZ3NDEKTSV4RRFFQ69G5FAV", executionModeFullAccess)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	joined := strings.Join(order, "|")
	if !strings.Contains(joined, "started") || !strings.Contains(joined, "completed") {
		t.Fatalf("event order missing start/complete: %v", order)
	}
	// At least one live output chunk carrying the command output must sit
	// strictly between started and completed.
	startIdx, outIdx, doneIdx := -1, -1, -1
	for i, s := range order {
		if s == "started" && startIdx < 0 {
			startIdx = i
		}
		if strings.HasPrefix(s, "output:") && strings.Contains(s, "go version") && outIdx < 0 {
			outIdx = i
		}
		if s == "completed" && doneIdx < 0 {
			doneIdx = i
		}
	}
	if startIdx < 0 || outIdx < 0 || doneIdx < 0 || !(startIdx < outIdx && outIdx < doneIdx) {
		t.Fatalf("tool_output not between started/completed: %v", order)
	}
}
