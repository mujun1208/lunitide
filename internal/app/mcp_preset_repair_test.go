package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

func TestMcpLegacyPresetRepairPreservesReferencesAndCustomConfiguration(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	add := func(pkg string) string {
		out, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "npx", Args: []string{"-y", pkg}, RiskConfirmed: true})
		if err != nil {
			t.Fatal(err)
		}
		return out.EndpointID
	}
	// Use the settings-only prober to model a pre-upgrade descriptor. No server runs.
	e.m7mcp.SetProber(m7app.LocalMcpProber{})
	old := add("@modelcontextprotocol/server-fetch")
	custom := add("custom-server")
	if _, err := e.m7mcp.Toggle(ctx, old, true, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, old)
	if err != nil || ep.Command != "uvx" || ep.ArgsJSON != `["mcp-server-fetch"]` || !ep.Enabled || ep.State != m7flow.McpStateDegraded {
		t.Fatalf("legacy reference not repaired: %+v %v", ep, err)
	}
	if ep.PinnedDigest != "" {
		t.Fatal("old descriptor digest would quarantine repaired launch")
	}
	other, err := e.m7mcp.Endpoint(ctx, custom)
	if err != nil || other.Command != "npx" {
		t.Fatal("custom config changed")
	}
	var args []string
	_ = json.Unmarshal([]byte(other.ArgsJSON), &args)
	if len(args) != 2 || args[1] != "custom-server" {
		t.Fatal("custom args changed")
	}
	// A pack holding the old endpoint ID can now resume the same resource.
	fresh, err := e.m7mcp.Add(ctx, m7app.McpAddInput{EndpointID: old, Origin: "manual", Transport: "stdio", Command: "uvx", Args: []string{"mcp-server-fetch"}, RiskConfirmed: true})
	if err != nil || fresh.EndpointID != old {
		t.Fatalf("pack reference lost: %+v %v", fresh, err)
	}
}
