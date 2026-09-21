package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/oklog/ulid/v2"
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

func TestMcpAddResolvesFilesystemPlaceholder(t *testing.T) {
	e, _ := packFixture(t)
	e.m7mcp.SetProber(m7app.LocalMcpProber{})
	out, err := e.m7mcp.Add(context.Background(), m7app.McpAddInput{
		Origin: "manual", Transport: "stdio", Command: "npx",
		Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "{{dir}}"},
		RiskConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(context.Background(), out.EndpointID)
	if err != nil || strings.Contains(ep.ArgsJSON, "{{dir}}") || !strings.Contains(ep.ArgsJSON, "mcp/filesystem") {
		t.Fatalf("placeholder leaked into launch: %+v %v", ep, err)
	}
}

func TestMcpLegacyPresetRepairRemapsYoutubePackage(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	e.m7mcp.SetProber(m7app.LocalMcpProber{})
	old, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "npx", Args: []string{"-y", "youtube-transcript-mcp"}, RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, old.EndpointID)
	if err != nil || ep.Command != "npx" || ep.ArgsJSON != `["-y","@sinco-lab/mcp-youtube-transcript"]` || ep.State != m7flow.McpStateDegraded {
		t.Fatalf("youtube package not remapped: %+v %v", ep, err)
	}
}

func TestMcpLegacyPresetRepairRemapsNickclydeDuckduckgo(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	e.m7mcp.SetProber(m7app.LocalMcpProber{})
	old, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "npx", Args: []string{"-y", "@nickclyde/duckduckgo-mcp-server"}, RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, old.EndpointID)
	if err != nil || ep.Command != "uvx" || ep.ArgsJSON != `["duckduckgo-mcp-server"]` || ep.State != m7flow.McpStateDegraded {
		t.Fatalf("duckduckgo package not remapped: %+v %v", ep, err)
	}
}

func TestMcpLegacyPresetRepairSkipsQuarantined(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	e.m7mcp.SetProber(m7app.LocalMcpProber{})
	id := "mcp-" + ulid.Make().String()
	if err := store.AgentRuntimeRepository().TransactMcp(ctx, func(tx m7app.McpTx) error {
		return tx.PutMcpEndpoint(m7flow.McpEndpointConfig{
			EndpointID:  id,
			Transport:   m7flow.McpTransportStdio,
			Command:     "npx",
			ArgsJSON:    `["-y","youtube-transcript-mcp"]`,
			Origin:      m7flow.McpOriginManual,
			SourceTrust: m7flow.McpTrustVerified,
			Enabled:     true,
			State:       m7flow.McpStateQuarantined,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, id)
	if err != nil || ep.State != m7flow.McpStateQuarantined || ep.ArgsJSON != `["-y","youtube-transcript-mcp"]` {
		t.Fatalf("quarantined launch must stay sealed: %+v %v", ep, err)
	}
}

func TestMcpLegacyPresetRepairRemapsYoutubeAndDropsDuplicate(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	e.m7mcp.SetProber(m7app.LocalMcpProber{})
	keep, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "npx", Args: []string{"-y", "@sinco-lab/mcp-youtube-transcript"}, RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	old, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "npx", Args: []string{"-y", "youtube-transcript-mcp"}, RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	kept, err := e.m7mcp.Endpoint(ctx, keep.EndpointID)
	if err != nil || kept.State == m7flow.McpStateRevoked || !strings.Contains(kept.ArgsJSON, "@sinco-lab/mcp-youtube-transcript") {
		t.Fatalf("current youtube grant lost: %+v %v", kept, err)
	}
	dup, err := e.m7mcp.Endpoint(ctx, old.EndpointID)
	if err != nil || dup.State != m7flow.McpStateRevoked {
		t.Fatalf("old youtube remount should drop after remap collision: %+v %v", dup, err)
	}
}

func TestMcpLegacyPresetRepairSubstitutesFilesystemPlaceholder(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	id := "mcp-" + ulid.Make().String()
	if err := store.AgentRuntimeRepository().TransactMcp(ctx, func(tx m7app.McpTx) error {
		return tx.PutMcpEndpoint(m7flow.McpEndpointConfig{
			EndpointID:  id,
			Transport:   m7flow.McpTransportStdio,
			Command:     "npx",
			ArgsJSON:    `["-y","@modelcontextprotocol/server-filesystem","{{dir}}"]`,
			Origin:      m7flow.McpOriginManual,
			SourceTrust: m7flow.McpTrustVerified,
			Enabled:     true,
			State:       m7flow.McpStateDegraded,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RepairLegacyMcpPresetLaunches(ctx); err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, id)
	if err != nil || strings.Contains(ep.ArgsJSON, "{{dir}}") || !strings.Contains(ep.ArgsJSON, "mcp/filesystem") {
		t.Fatalf("filesystem placeholder not repaired: %+v %v", ep, err)
	}
}
