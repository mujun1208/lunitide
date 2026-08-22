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
	"github.com/lunitide/lunitide/internal/mcp6"
)

// toolsFallbackAdapter rejects the first stream attempt with an HTTP 400
// (function definitions unsupported) and answers plain text afterwards.
type toolsFallbackAdapter struct{ attempts int }

func (a *toolsFallbackAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *toolsFallbackAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *toolsFallbackAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.attempts++
	if a.attempts == 1 {
		return gateway.Response{}, &gateway.Error{Code: "HTTP_400", Stage: gateway.StageHTTP, HTTPStatus: 400, Message: "tools unsupported"}
	}
	if err := emit(gateway.Delta{Text: "plain answer"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Usage: gateway.Usage{OutputTokens: 2, TotalTokens: 2}}, nil
}

func TestToolsFallbackEmitsExplicitNotice(t *testing.T) {
	adapter := &toolsFallbackAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-notice"
	e.streams[id] = state
	events := make(chan bridge.Event, 16)
	done := make(chan struct{})
	var deltas []string
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas = append(deltas, ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	req := gateway.Request{Model: "m", Tools: engineToolDefinitions()}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error { events <- event; return nil }, "")
	// runStream returns after emitting the terminal event; give the consumer
	// goroutine a moment then close the loop check.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	joined := strings.Join(deltas, "")
	if !strings.Contains(joined, "纯对话模式") {
		t.Fatalf("degradation notice missing: %q", joined)
	}
	if !strings.Contains(joined, "plain answer") {
		t.Fatalf("retry answer missing: %q", joined)
	}
	if adapter.attempts != 2 {
		t.Fatalf("attempts = %d", adapter.attempts)
	}
}

func TestMcpToolNameRoundtrip(t *testing.T) {
	endpoint := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	name, ok := mcpToolName(endpoint, "get_weather")
	if !ok || name != "mcp_01ARZ3NDEKTSV4RRFFQ69G5FAV_get_weather" {
		t.Fatalf("name = %q ok=%v", name, ok)
	}
	gotEndpoint, gotTool, ok := parseMcpToolName(name)
	if !ok || gotEndpoint != endpoint || gotTool != "get_weather" {
		t.Fatalf("parse = %q %q %v", gotEndpoint, gotTool, ok)
	}
	if _, ok := mcpToolName(endpoint, strings.Repeat("t", 64)); ok {
		t.Fatal("over-budget name accepted")
	}
	if _, ok := mcpToolName(endpoint, "bad tool"); ok {
		t.Fatal("name with space accepted")
	}
	if _, _, ok := parseMcpToolName("workspace.read"); ok {
		t.Fatal("non-mcp tool parsed as mcp")
	}
}

// fakeMcpLease satisfies mcp6.SecretLease with an empty credential.
type fakeMcpLease struct{}

func (fakeMcpLease) WithLease(_ context.Context, _ string, fn func([]byte) error) error {
	return fn([]byte("token"))
}

// mcpDispatchAdapter asks for one merged MCP tool, then finishes.
type mcpDispatchAdapter struct {
	mcpToolName string
	toolCalls   int
}

func (a *mcpDispatchAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *mcpDispatchAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *mcpDispatchAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if a.toolCalls == 0 {
		a.toolCalls++
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-1", Name: a.mcpToolName, Arguments: []byte(`{"city":"Shanghai"}`)},
		}}}, nil
	}
	if err := emit(gateway.Delta{Text: "weather done"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Usage: gateway.Usage{OutputTokens: 3, TotalTokens: 3}}, nil
}

func TestMcpToolsMergedAndDispatched(t *testing.T) {
	var invoked []string
	var invokedArgs map[string]any
	invoke := func(_ context.Context, _ *mcp6.Endpoint, tool string, args map[string]any, _ []byte) (map[string]any, error) {
		invoked = append(invoked, tool)
		invokedArgs = args
		return map[string]any{"temp": 31}, nil
	}
	registry := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error { return nil }, invoke, fakeMcpLease{})
	registry.SetDescribeFunc(func(context.Context, *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
		return map[string]mcp6.ToolSchema{
			"get_weather": {Description: "Get current weather for a city", InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)},
		}, nil
	})
	endpoint, err := registry.Register(context.Background(), mcp6.EndpointInput{Transport: "https", URL: "https://mcp.example.com", AuthRef: "secretref:ref", Pin: mcp6.CapabilityPin{
		ServerIdentityDigest: strings.Repeat("a", 64),
		ToolSchemaDigests:    map[string]string{"get_weather": strings.Repeat("b", 64)},
	}})
	if err != nil && !errors.Is(err, mcp6.ErrHealthCheckFailed) {
		t.Fatal(err)
	}
	if endpoint.State != mcp6.StateReady {
		t.Fatalf("endpoint state = %s", endpoint.State)
	}

	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetM6Services(nil, registry, nil)
	merged := e.mcpToolDefinitions()
	var weatherDef *gateway.ToolDefinition
	for i := range merged {
		if strings.Contains(merged[i].Name, "get_weather") {
			weatherDef = &merged[i]
		}
	}
	if weatherDef == nil {
		t.Fatalf("get_weather not merged: %+v", merged)
	}
	if weatherDef.Description != "Get current weather for a city" {
		t.Fatalf("description = %q (real description must win)", weatherDef.Description)
	}
	if !strings.Contains(string(weatherDef.Schema), `"city"`) {
		t.Fatalf("real schema must be carried, got %s", weatherDef.Schema)
	}

	adapter := &mcpDispatchAdapter{mcpToolName: "mcp_" + endpoint.ID + "_get_weather"}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-mcp"
	e.streams[id] = state
	events := make(chan bridge.Event, 16)
	var toolSummaries []string
	done := make(chan struct{})
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventToolCompleted && ev.Tool != nil {
				toolSummaries = append(toolSummaries, ev.Tool.Summary)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	req := gateway.Request{Model: "m", Tools: merged}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	if len(invoked) != 1 || invoked[0] != "get_weather" {
		t.Fatalf("invoke = %v", invoked)
	}
	if invokedArgs["city"] != "Shanghai" {
		t.Fatalf("args = %v", invokedArgs)
	}
	if len(toolSummaries) != 1 || !strings.Contains(toolSummaries[0], `"temp":31`) {
		t.Fatalf("tool summaries = %v", toolSummaries)
	}
}

// Endpoints without a describe cache must fall back to the pass-through
// schema and the generic description.
func TestMcpToolsFallbackSchemaWithoutDescribe(t *testing.T) {
	registry := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error { return nil }, nil, fakeMcpLease{})
	endpoint, err := registry.Register(context.Background(), mcp6.EndpointInput{Transport: "https", URL: "https://mcp.example.com", AuthRef: "secretref:ref", Pin: mcp6.CapabilityPin{
		ServerIdentityDigest: strings.Repeat("a", 64),
		ToolSchemaDigests:    map[string]string{"get_weather": strings.Repeat("b", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetM6Services(nil, registry, nil)
	merged := e.mcpToolDefinitions()
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Description != "MCP tool get_weather on endpoint "+endpoint.ID+" (arguments pass through to the endpoint)" {
		t.Fatalf("fallback description = %q", merged[0].Description)
	}
	if string(merged[0].Schema) != `{"type":"object","additionalProperties":true}` {
		t.Fatalf("fallback schema = %s", merged[0].Schema)
	}
}

func TestEngineToolDefinitionsIncludeHTMLGen(t *testing.T) {
	found := false
	for _, d := range engineToolDefinitions() {
		if d.Name == "html.gen" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("html.gen missing from engine tools")
	}
	foundOpen := false
	for _, d := range engineToolDefinitions() {
		if d.Name == "desktop.open" {
			foundOpen = true
			break
		}
	}
	if !foundOpen {
		t.Fatal("desktop.open missing from engine tools")
	}
	for _, d := range readOnlyEngineToolDefinitions() {
		if d.Name == "html.gen" || d.Name == "desktop.open" {
			t.Fatal("subagents must not receive html.gen or desktop.open")
		}
	}
	if !strings.Contains(bundledWorkflowInjection(), "html.gen") || strings.Contains(bundledWorkflowInjection(), "桌面 HTML 小游戏：workspace.write") {
		t.Fatal("desktop game workflow must route through html.gen")
	}
	if !strings.Contains(bundledWorkflowInjection(), "desktop.open") || !strings.Contains(bundledWorkflowInjection(), "闭环") {
		t.Fatal("desktop open and closed-loop workflow missing")
	}
	if !strings.Contains(chatRichMarkdownInstruction(), "mermaid") || !strings.Contains(chatRichMarkdownInstruction(), "powershell") {
		t.Fatal("rich markdown instruction missing")
	}
}

func TestEngineToolDefinitionsIncludeBrowserAct(t *testing.T) {
	found := false
	for _, d := range engineToolDefinitions() {
		if d.Name == "browser.act" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("browser.act missing from engine tools")
	}
	for _, d := range readOnlyEngineToolDefinitions() {
		if d.Name == "browser.act" {
			t.Fatal("subagents must not receive browser.act")
		}
	}
}

func TestMcpToolDefinitionsSwitchToSearchWhenCatalogIsLarge(t *testing.T) {
	digests := map[string]string{}
	schemas := map[string]mcp6.ToolSchema{}
	for i := 0; i < mcpDirectToolCap+1; i++ {
		name := "tool_" + string(rune('a'+i))
		digests[name] = strings.Repeat("b", 64)
		schemas[name] = mcp6.ToolSchema{Description: "lookup " + name, InputSchema: []byte(`{"type":"object"}`)}
	}
	registry := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error { return nil }, nil, fakeMcpLease{})
	registry.SetDescribeFunc(func(context.Context, *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
		return schemas, nil
	})
	if _, err := registry.Register(context.Background(), mcp6.EndpointInput{
		Transport: "https", URL: "https://mcp.example.com", AuthRef: "secretref:ref",
		Pin: mcp6.CapabilityPin{ServerIdentityDigest: strings.Repeat("a", 64), ToolSchemaDigests: digests},
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetM6Services(nil, registry, nil)
	merged := e.mcpToolDefinitions()
	if len(merged) != 2 || merged[0].Name != "mcp.search" || merged[1].Name != "mcp.call" {
		t.Fatalf("want search gateway, got %+v", merged)
	}
	out, err := e.searchMcpTools([]byte(`{"query":"lookup tool_a"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tool_a") {
		t.Fatalf("search miss: %s", out)
	}
}
