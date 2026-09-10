package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/modelfit"
)

func TestChatMcpEndpointIDStripsSettingsPrefix(t *testing.T) {
	const ulid = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if got := chatMcpEndpointID("mcp-" + ulid); got != ulid {
		t.Fatalf("got %s", got)
	}
	if got := chatMcpEndpointID(ulid); got != ulid {
		t.Fatalf("passthrough %s", got)
	}
}

func TestSeedRecommendedMcpKitAddsDistinctNpxServers(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	e.SeedRecommendedMcpKit(context.Background())
	e.SeedRecommendedMcpKit(context.Background())
	eps, err := e.m7mcp.List(context.Background(), m7flow.McpTransportStdio)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 2 {
		t.Fatalf("want 2 kit endpoints, got %d (%+v)", len(eps), eps)
	}
	seen := map[string]bool{}
	for _, ep := range eps {
		if !ep.Enabled {
			t.Fatalf("kit endpoint disabled: %+v", ep)
		}
		seen[ep.ArgsJSON] = true
	}
	if len(seen) != 2 {
		t.Fatalf("stdio args collided: %+v", eps)
	}
}

func TestInvokeMcpToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: session, Task: session})
	_, err := e.invokeMcpTool(ctx, session, session, "weather", []byte(`{}`))
	if err == nil {
		t.Fatal("unready mcp tool must fail")
	}
	op := store.last()
	if !strings.HasPrefix(op.ToolName, "mcp_") || op.State == modelfit.OpSucceeded {
		t.Fatalf("mcp invoke must leave a receipt without inventing success: %+v", op)
	}
	if op.SessionID != session && op.OwnerScope != session {
		t.Fatalf("receipt must belong to the chat session: %+v", op)
	}
}

func TestCallMcpToolByNameRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ctx := withContinuityScope(context.Background(), continuityScope{Owner: session, Task: session})
	name := mcpToolPrefix + session + "_weather"
	args := []byte(`{"name":"` + name + `","arguments":{}}`)
	_, err := e.callMcpToolByName(ctx, session, args)
	if err == nil {
		t.Fatal("unready mcp.call must fail")
	}
	op := store.last()
	if !strings.HasPrefix(op.ToolName, "mcp_") || op.State == modelfit.OpSucceeded {
		t.Fatalf("mcp.call must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokeBrowserActRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokeBrowserAct(context.Background(), executionModeApproval, session, []byte(`{"op":"click"}`))
	if err == nil {
		t.Fatal("unready click must fail")
	}
	op := store.last()
	if op.ToolName != "browser.act" || op.State == modelfit.OpSucceeded {
		t.Fatalf("browser.act must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokeBrowserActGuidesInteractiveOps(t *testing.T) {
	e := NewEngine(nil, "test")
	_, err := e.invokeBrowserAct(context.Background(), executionModeApproval, "sess", []byte(`{"op":"click"}`))
	if err == nil || !strings.Contains(err.Error(), "BROWSER_") {
		t.Fatalf("unready click must error, got %v", err)
	}
	if strings.Contains(err.Error(), "已经在播") || strings.Contains(strings.ToLower(err.Error()), `"action":"play"`) {
		t.Fatalf("empty click must not fall through to media.play: %v", err)
	}
}

func TestInvokeBrowserActNavigateNeedsURL(t *testing.T) {
	e := NewEngine(nil, "test")
	_, err := e.invokeBrowserAct(context.Background(), executionModeApproval, "sess", []byte(`{"op":"navigate"}`))
	if err == nil || !strings.Contains(err.Error(), "needs url") {
		t.Fatalf("err = %v", err)
	}
}

func TestInvokeBrowserActNewOpsGuidePlaywright(t *testing.T) {
	e := NewEngine(nil, "test")
	for _, raw := range []string{`{"op":"scroll","direction":"down"}`, `{"op":"back"}`, `{"op":"tabs","tab":"list"}`} {
		_, err := e.invokeBrowserAct(context.Background(), executionModeApproval, "sess", []byte(raw))
		if err == nil || !strings.Contains(err.Error(), "BROWSER_") {
			t.Fatalf("op %s must error when MCP is missing, got %v", raw, err)
		}
		if strings.Contains(err.Error(), "evaluate") {
			t.Fatalf("op %s must not invent evaluate: %v", raw, err)
		}
	}
}
