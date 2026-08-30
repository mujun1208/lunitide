package mcp6

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// c3-mcp: the shipped catalog must survive the unchanged M6-MCP-004
// admission gate (fail-closed m7flow whitelist) both as-shipped and after
// placeholder resolution.
func presetPackageAllowed(spec string) bool {
	if strings.HasPrefix(spec, "@modelcontextprotocol/") {
		return true
	}
	switch spec {
	case "@playwright/mcp", "@upstash/context7-mcp":
		return true
	default:
		return false
	}
}

func TestPresetCatalogPassesWhitelist(t *testing.T) {
	if err := ValidatePresetCatalog(); err != nil {
		t.Fatalf("preset catalog invalid: %v", err)
	}
	all := Presets()
	if len(all) != 8 {
		t.Fatalf("expected 8 live presets, got %d", len(all))
	}
	for _, p := range all {
		if p.Transport != "stdio" {
			t.Fatalf("preset %s: transport = %q, want stdio", p.ID, p.Transport)
		}
		if !m7flow.McpStdioCommandAllowed(p.Command) {
			t.Fatalf("preset %s: command %q not whitelisted", p.ID, p.Command)
		}
		if len(p.Args) == 0 || len(p.Args) > 16 {
			t.Fatalf("preset %s: args count %d outside 1..16", p.ID, len(p.Args))
		}
		if !m7flow.McpArgsSafe(p.Args) {
			t.Fatalf("preset %s: template args contain metacharacters", p.ID)
		}
		if !presetPackageAllowed(p.Args[1]) {
			t.Fatalf("preset %s: args[1] = %q, want a curated free server spec", p.ID, p.Args[1])
		}
	}
}

// Every preset registers through the real registry gate (probe disabled)
// once its placeholder is resolved to a benign value; the gate itself stays
// the authority — this proves the catalog needs no whitelist relaxation.
func TestPresetsRegisterThroughRegistryGate(t *testing.T) {
	r := newTestRegistry(nil, nil)
	for _, p := range Presets() {
		args := p.ResolveArgs("C:/Users/demo/projects/sample")
		e, err := r.Register(context.Background(), EndpointInput{
			Transport: "stdio", Command: p.Command, Args: args, Pin: validPin(),
		})
		if err != nil || e.State != StateReady {
			t.Fatalf("preset %s rejected by registry: %v state=%s", p.ID, err, e.State)
		}
		if e.URL != "stdio://"+p.Command {
			t.Fatalf("preset %s: url = %q", p.ID, e.URL)
		}
	}
}

// Fail-closed proof: user-supplied hostile placeholder values must be
// refused by the unchanged whitelist (m7flow.McpArgsSafe), never laundered
// by the preset machinery.
func TestPresetHostilePlaceholderStillRefused(t *testing.T) {
	r := newTestRegistry(nil, nil)
	preset, ok := PresetByID("filesystem")
	if !ok {
		t.Fatal("filesystem preset missing")
	}
	for _, hostile := range []string{
		"E:/x; calc",
		"E:/x && whoami",
		"E:/x$(whoami)",
		"E:/x`whoami`",
		"E:\\x\\y", // backslash is a metacharacter on the wire
		"E:/x|rm -rf /",
		"E:/x>passwd",
	} {
		args := preset.ResolveArgs(hostile)
		if m7flow.McpArgsSafe(args) {
			t.Fatalf("hostile input %q unexpectedly passed the whitelist", hostile)
		}
		if _, err := r.Register(context.Background(), EndpointInput{
			Transport: "stdio", Command: preset.Command, Args: args, Pin: validPin(),
		}); !errors.Is(err, ErrStdioDisabled) {
			t.Fatalf("registry accepted hostile input %q: %v", hostile, err)
		}
	}
	// the benign normalized shape (forward slashes, spaces allowed) passes
	benign := preset.ResolveArgs("E:/my projects/demo repo")
	if !m7flow.McpArgsSafe(benign) {
		t.Fatalf("benign path %v refused", benign)
	}
	if _, err := r.Register(context.Background(), EndpointInput{
		Transport: "stdio", Command: preset.Command, Args: benign, Pin: validPin(),
	}); err != nil {
		t.Fatalf("benign path rejected: %v", err)
	}
}

// The needsArgs contract: only filesystem still needs a sandbox path.
// Archived git/sqlite presets were removed. Presets() hands out copies.
func TestPresetNeedsArgsContract(t *testing.T) {
	wantNeedsArgs := map[string]bool{
		"everything": false, "filesystem": true, "fetch": false, "memory": false,
		"sequentialthinking": false, "playwright": false, "time": false, "context7": false,
	}
	for id, want := range wantNeedsArgs {
		p, ok := PresetByID(id)
		if !ok {
			t.Fatalf("preset %s missing from catalog", id)
		}
		if p.NeedsArgs != want {
			t.Fatalf("preset %s needsArgs = %v, want %v", id, p.NeedsArgs, want)
		}
	}
	fs, _ := PresetByID("filesystem")
	resolved := fs.ResolveArgs("E:/repos/lunitide")
	if resolved[len(resolved)-1] != "E:/repos/lunitide" {
		t.Fatalf("filesystem resolve produced %v", resolved)
	}
	for _, archived := range []string{"git", "github", "puppeteer", "sqlite"} {
		if _, ok := PresetByID(archived); ok {
			t.Fatalf("archived preset %s must not ship", archived)
		}
	}
	// defensive copies: mutating the returned rows must not corrupt the catalog
	got := Presets()
	got[0].Args[1] = "tampered"
	fresh, _ := PresetByID(got[0].ID)
	if fresh.Args[1] == "tampered" {
		t.Fatal("Presets() leaked the backing catalog")
	}
	// unknown lookup
	if _, ok := PresetByID("no-such-preset"); ok {
		t.Fatal("unknown preset id resolved")
	}
}

func TestPrepareSandboxUsesForwardSlashAppDir(t *testing.T) {
	path := PrepareSandbox("filesystem")
	if path == "" || strings.Contains(path, `\`) {
		t.Fatalf("sandbox path must be non-empty forward slashes, got %q", path)
	}
	if !strings.Contains(path, "Lunitide/mcp/filesystem") && !strings.Contains(path, "Lunitide/mcp\\filesystem") {
		if !strings.Contains(path, "/mcp/filesystem") {
			t.Fatalf("sandbox path = %q, want Lunitide/mcp/filesystem", path)
		}
	}
	sqlite := PrepareSandbox("sqlite")
	if !strings.HasSuffix(sqlite, "lunitide.db") || strings.Contains(sqlite, `\`) {
		t.Fatalf("sqlite sandbox = %q", sqlite)
	}
}
