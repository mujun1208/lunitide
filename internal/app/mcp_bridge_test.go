package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
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

func TestInvokeBrowserActGuidesInteractiveOps(t *testing.T) {
	e := NewEngine(nil, "test")
	out, err := e.invokeBrowserAct(context.Background(), executionModeApproval, "sess", []byte(`{"op":"click"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "Playwright") && !strings.Contains(out.Output, "media.play") {
		t.Fatalf("output = %q", out.Output)
	}
}
