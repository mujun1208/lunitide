package mcp6

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/mcp"
)

// c3-mcp: the shipped catalog must survive the unchanged M6-MCP-004
// admission gate (fail-closed m7flow whitelist) both as-shipped and after
// placeholder resolution.
func TestPresetCatalogPassesWhitelist(t *testing.T) {
	if err := ValidatePresetCatalog(); err != nil {
		t.Fatalf("preset catalog invalid: %v", err)
	}
	all := Presets()
	if len(all) < 30 || len(all) > 48 {
		t.Fatalf("expected a curated ~40 live presets, got %d", len(all))
	}
	for _, p := range all {
		if p.Transport == "https" {
			if err := mcp.ValidateBaseURL(p.URL); err != nil {
				t.Fatal(err)
			}
			continue
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
		if p.Command == "node" {
			if !p.NeedsArgs || p.SetupURL == "" {
				t.Fatal("source-built server needs an explicit entry path and upstream guide")
			}
			continue
		}
		spec := PresetLaunchPackage(p)
		if !PresetPackageAllowed(spec) {
			t.Fatalf("preset %s: launch package %q is not curated", p.ID, spec)
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
			Transport: p.Transport, URL: p.URL, Command: p.Command, Args: args, Pin: validPin(),
		})
		if err != nil || e.State != StateReady {
			t.Fatalf("preset %s rejected by registry: %v state=%s", p.ID, err, e.State)
		}
		if p.Transport == "stdio" && e.URL != "stdio://"+p.Command {
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
		"chrome-devtools": false, "postgres": true, "tavily": false,
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
	for _, p := range Presets() {
		if p.NeedsArgs && p.ArgPlaceholder == "" {
			t.Fatalf("preset %s needsArgs without placeholder", p.ID)
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

func TestOfficeDocumentPresetsAreCurated(t *testing.T) {
	want := map[string]struct {
		category string
		command  string
		args     []string
		pkg      string
	}{
		"excel-mcp":  {category: "办公", command: "uvx", args: []string{"excel-mcp-server", "stdio"}, pkg: "excel-mcp-server"},
		"word-mcp":   {category: "办公", command: "uvx", args: []string{"--from", "office-word-mcp-server", "word_mcp_server"}, pkg: "office-word-mcp-server"},
		"ppt-mcp":    {category: "办公", command: "uvx", args: []string{"office-ppt-mcp-server"}, pkg: "office-ppt-mcp-server"},
		"pdf-mcp":    {category: "办公", command: "npx", args: []string{"-y", "pdfnative-mcp"}, pkg: "pdfnative-mcp"},
		"markitdown": {category: "办公", command: "uvx", args: []string{"markitdown-mcp"}, pkg: "markitdown-mcp"},
	}
	for id, spec := range want {
		p, ok := PresetByID(id)
		if !ok {
			t.Fatalf("missing office preset %s", id)
		}
		if p.Category != spec.category || p.Command != spec.command || strings.Join(p.Args, " ") != strings.Join(spec.args, " ") {
			t.Fatalf("%s = %s %v category=%q", id, p.Command, p.Args, p.Category)
		}
		if got := PresetLaunchPackage(p); got != spec.pkg || !PresetPackageAllowed(got) {
			t.Fatalf("%s launch package %q not allowed", id, got)
		}
		if _, err := mcp.ResolveLaunchArgs(context.Background(), p.Command, p.Args, officePresetLockFetch); err != nil {
			t.Fatalf("%s launch lock rejected catalog args: %v", id, err)
		}
	}
	if PresetLaunchPackage(Preset{Command: "uvx", Args: []string{"--from", "office-word-mcp-server", "word_mcp_server"}}) != "office-word-mcp-server" {
		t.Fatal("uvx --from package resolution failed")
	}
}

func officePresetLockFetch(_ context.Context, target string) ([]byte, error) {
	switch {
	case strings.Contains(target, "excel-mcp-server"):
		return []byte(`{"info":{"name":"excel-mcp-server","version":"0.1.0"}}`), nil
	case strings.Contains(target, "office-word-mcp-server"):
		return []byte(`{"info":{"name":"office-word-mcp-server","version":"1.1.0"}}`), nil
	case strings.Contains(target, "office-ppt-mcp-server"):
		return []byte(`{"info":{"name":"office-ppt-mcp-server","version":"2.0.7"}}`), nil
	case strings.Contains(target, "office-powerpoint-mcp-server"):
		return []byte(`{"info":{"name":"office-powerpoint-mcp-server","version":"2.0.0"}}`), nil
	case strings.Contains(target, "markitdown-mcp"):
		return []byte(`{"info":{"name":"markitdown-mcp","version":"0.0.1"}}`), nil
	case strings.Contains(target, "pdfnative-mcp"):
		return []byte(`{"name":"pdfnative-mcp","version":"1.6.0"}`), nil
	default:
		return nil, errors.New(target)
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
