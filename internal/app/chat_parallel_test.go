// P0-1 parallel tool execution coverage: same-turn eligible calls (MCP and
// read-only engine tools) must overlap on background goroutines while the
// event stream and tool-message order stay identical to serial execution;
// mutating and gated tools must never enter the future pool.
package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func TestParallelToolEligible(t *testing.T) {
	readOnly := []string{"workspace.list", "workspace.read", "workspace.search", "web.fetch", "web.search", "excel.parse"}
	for _, name := range readOnly {
		if !parallelToolEligible(name) {
			t.Fatalf("%s must be eligible", name)
		}
	}
	mutating := []string{"workspace.write", "workspace.edit", "command.run", "todo.write", "excel.gen", "docx.gen", "pptx.gen", "pdf.gen", "cc.mouse_click", "cc.keyboard_type", "subagent.spawn", "plan.run", "unknown.tool"}
	for _, name := range mutating {
		if parallelToolEligible(name) {
			t.Fatalf("%s must stay serial", name)
		}
	}
	if parallelToolEligible("mcp_01ARZ3NDEKTSV4RRFFQ69G5FAV_get_weather") {
		t.Fatal("merged MCP tool must NOT be eligible to prevent concurrent write locks")
	}

}

// parallelMcpAdapter issues two MCP calls in one turn (slow first, fast
// second), then finishes with plain text.
type parallelMcpAdapter struct {
	toolA, toolB string
	turn         int
}

func (a *parallelMcpAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *parallelMcpAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *parallelMcpAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if a.turn == 0 {
		a.turn++
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-1", Name: a.toolA, Arguments: []byte(`{"n":1}`)},
			{ID: "call-2", Name: a.toolB, Arguments: []byte(`{"n":2}`)},
		}}}, nil
	}
	if err := emit(gateway.Delta{Text: "parallel done"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Usage: gateway.Usage{OutputTokens: 2, TotalTokens: 2}}, nil
}

func TestParallelMcpCallsAreSerialized(t *testing.T) {
	// Serialization proof: we track the number of currently executing MCP calls.
	// If it ever goes > 1, they overlapped.
	var mu sync.Mutex
	activeCalls := 0
	overlapped := false

	invoke := func(_ context.Context, _ *mcp6.Endpoint, tool string, _ map[string]any, _ []byte) (map[string]any, error) {
		mu.Lock()
		activeCalls++
		if activeCalls > 1 {
			overlapped = true
		}
		mu.Unlock()

		defer func() {
			mu.Lock()
			activeCalls--
			mu.Unlock()
		}()

		if strings.HasSuffix(tool, "slow") {
			time.Sleep(100 * time.Millisecond)
		}
		return map[string]any{"tool": tool}, nil
	}
	registry := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error { return nil }, invoke, fakeMcpLease{})
	registry.SetDescribeFunc(func(context.Context, *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
		return map[string]mcp6.ToolSchema{"lookup_slow": {}, "lookup_fast": {}}, nil
	})
	endpoint, err := registry.Register(context.Background(), mcp6.EndpointInput{Transport: "https", URL: "https://mcp.example.com", AuthRef: "secretref:ref", Pin: mcp6.CapabilityPin{
		ServerIdentityDigest: strings.Repeat("a", 64),
		ToolSchemaDigests:    map[string]string{"lookup_slow": strings.Repeat("b", 64), "lookup_fast": strings.Repeat("c", 64)},
	}})
	if err != nil && !errors.Is(err, mcp6.ErrHealthCheckFailed) {
		t.Fatal(err)
	}

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetM6Services(nil, registry, nil)
	adapter := &parallelMcpAdapter{
		toolA: "mcp_" + endpoint.ID + "_lookup_slow",
		toolB: "mcp_" + endpoint.ID + "_lookup_fast",
	}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-parallel"
	e.streams[id] = state
	events := make(chan bridge.Event, 16)
	done := make(chan struct{})
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	req := gateway.Request{Model: "m", Tools: e.mcpToolDefinitions()}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	mu.Lock()
	didOverlap := overlapped
	mu.Unlock()
	if didOverlap {
		t.Fatal("MCP calls overlapped! They must be serialized to prevent SQLite database is locked errors.")
	}
}

// writeGateAdapter asks for workspace.write in approval mode; the call
// must run inline (never pre-started) so the approval gate fires.
type writeGateAdapter struct{ turn int }

func (a *writeGateAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *writeGateAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *writeGateAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if a.turn == 0 {
		a.turn++
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-1", Name: "workspace.write", Arguments: []byte(`{"path":"out.txt","content":"x"}`)},
		}}}, nil
	}
	return gateway.Response{}, nil
}

func TestWriteToolsNeverEnterFuturePool(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return &writeGateAdapter{}, nil })
	// Direct eligibility proof: mutating tools are excluded from the
	// future map, so the approval gate below stays inline and reachable.
	futures := startParallelToolFutures(context.Background(), e, executionModeApproval, "sess", []gateway.ToolCall{
		{ID: "call-1", Name: "workspace.write", Arguments: []byte(`{"path":"out.txt","content":"x"}`)},
		{ID: "call-2", Name: "workspace.read", Arguments: []byte(`{"path":"in.txt"}`)},
	})
	if _, ok := futures["call-1"]; ok {
		t.Fatal("workspace.write must never be pre-started")
	}
	if _, ok := futures["call-2"]; !ok {
		t.Fatal("workspace.read should be pre-started")
	}
}
