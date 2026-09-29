package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeSidecar writes a sidecar file under root, creating parent dirs.
func writeSidecar(t *testing.T, root string, rel string, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// seedAllSidecars populates every global sidecar with valid JSON and returns
// the data root.
func seedAllSidecars(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSidecar(t, root, "workspace-root.json", `{"path":"C:\\ws"}`)
	writeSidecar(t, root, "conversations-root.json", `{"path":"C:\\conv"}`)
	writeSidecar(t, root, "datasource-secrets.json", `{"ref-a":"dsn-a","ref-b":"dsn-b"}`)
	writeSidecar(t, root, filepath.Join("org", "binding.json"), `{"orgId":"org-77"}`)
	writeSidecar(t, root, filepath.Join("tool-workspaces", "command-policy.json"), `{"commands":[],"fullAccess":true}`)
	writeSidecar(t, root, filepath.Join("tool-workspaces", "hooks-policy.json"), `{"hooks":[]}`)
	writeSidecar(t, root, "inbound-routes.json", `{"sess-1":{"kind":"email","sender":"a@b.c","conversationId":"cv-1"}}`)
	writeSidecar(t, root, "mcp-presets.json", `{"ep-1":"preset-1"}`)
	writeSidecar(t, root, filepath.Join(".turns", "preferred-chat.json"), `{"providerId":"prov-9","modelId":"model-9"}`)
	return root
}

// TestLoadAllSidecars asserts every field parses from a fully-populated root.
func TestLoadAllSidecars(t *testing.T) {
	root := seedAllSidecars(t)
	cfg, err := NewLoader(root).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.WorkspaceRoot != `C:\ws` {
		t.Fatalf("WorkspaceRoot = %q", cfg.WorkspaceRoot)
	}
	if cfg.ConversationsRoot != `C:\conv` {
		t.Fatalf("ConversationsRoot = %q", cfg.ConversationsRoot)
	}
	if got := cfg.DatasourceSecrets["ref-a"]; got != "dsn-a" {
		t.Fatalf("DatasourceSecrets[ref-a] = %q", got)
	}
	if got := cfg.DatasourceSecrets["ref-b"]; got != "dsn-b" {
		t.Fatalf("DatasourceSecrets[ref-b] = %q", got)
	}
	if cfg.OrgBindingID != "org-77" {
		t.Fatalf("OrgBindingID = %q", cfg.OrgBindingID)
	}
	if len(cfg.CommandPolicyRaw) == 0 || !json.Valid(cfg.CommandPolicyRaw) {
		t.Fatalf("CommandPolicyRaw invalid: %s", cfg.CommandPolicyRaw)
	}
	if len(cfg.HooksPolicyRaw) == 0 || !json.Valid(cfg.HooksPolicyRaw) {
		t.Fatalf("HooksPolicyRaw invalid: %s", cfg.HooksPolicyRaw)
	}
	route, ok := cfg.InboundRoutes["sess-1"]
	if !ok || len(route) == 0 {
		t.Fatalf("InboundRoutes[sess-1] missing")
	}
	var parsedRoute struct {
		Kind   string `json:"kind"`
		Sender string `json:"sender"`
	}
	if err := json.Unmarshal(route, &parsedRoute); err != nil {
		t.Fatalf("unmarshal route: %v", err)
	}
	if parsedRoute.Kind != "email" || parsedRoute.Sender != "a@b.c" {
		t.Fatalf("route parsed wrong: %+v", parsedRoute)
	}
	if got := cfg.MCPPresets["ep-1"]; got != "preset-1" {
		t.Fatalf("MCPPresets[ep-1] = %q", got)
	}
	if cfg.PreferredChatProviderID != "prov-9" || cfg.PreferredChatModel != "model-9" {
		t.Fatalf("preferred chat = %q/%q", cfg.PreferredChatProviderID, cfg.PreferredChatModel)
	}
}

// TestLoadMissingFilesAreZero asserts an empty root yields a zero-valued config
// with no error.
func TestLoadMissingFilesAreZero(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewLoader(root).Load()
	if err != nil {
		t.Fatalf("Load on empty root: %v", err)
	}
	if cfg.WorkspaceRoot != "" || cfg.ConversationsRoot != "" || cfg.OrgBindingID != "" {
		t.Fatalf("expected zero strings, got %+v", cfg)
	}
	if cfg.DatasourceSecrets != nil || cfg.MCPPresets != nil || cfg.InboundRoutes != nil {
		t.Fatalf("expected nil maps for missing files, got %+v", cfg)
	}
	if cfg.CommandPolicyRaw != nil || cfg.HooksPolicyRaw != nil {
		t.Fatalf("expected nil raw for missing files")
	}
	if cfg.PreferredChatProviderID != "" || cfg.PreferredChatModel != "" {
		t.Fatalf("expected empty preferred chat")
	}
}

// TestLoadBadJSONNamesFile asserts a corrupt sidecar yields an error naming the
// offending file, for each parsed shape.
func TestLoadBadJSONNamesFile(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		want string
	}{
		{"workspace", "workspace-root.json", "workspace-root.json"},
		{"conversations", "conversations-root.json", "conversations-root.json"},
		{"datasource", "datasource-secrets.json", "datasource-secrets.json"},
		{"binding", filepath.Join("org", "binding.json"), "binding.json"},
		{"command-policy", filepath.Join("tool-workspaces", "command-policy.json"), "command-policy.json"},
		{"hooks-policy", filepath.Join("tool-workspaces", "hooks-policy.json"), "hooks-policy.json"},
		{"inbound", "inbound-routes.json", "inbound-routes.json"},
		{"mcp", "mcp-presets.json", "mcp-presets.json"},
		{"preferred", filepath.Join(".turns", "preferred-chat.json"), "preferred-chat.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeSidecar(t, root, tc.rel, `{not json`)
			_, err := NewLoader(root).Load()
			if err == nil {
				t.Fatalf("expected error for corrupt %s", tc.rel)
			}
			if got := err.Error(); !containsSub(got, tc.want) {
				t.Fatalf("error %q must name %q", got, tc.want)
			}
		})
	}
}

// TestReloadNotifiesSubscribers asserts Reload signals a Subscribe channel and
// coalesces without blocking.
func TestReloadNotifiesSubscribers(t *testing.T) {
	root := seedAllSidecars(t)
	l := NewLoader(root)
	ch := l.Subscribe()

	if _, err := l.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("subscriber did not receive a reload signal")
	}

	// A second Reload with a full buffer must not block (coalesced).
	if _, err := l.Reload(); err != nil {
		t.Fatalf("Reload #2: %v", err)
	}
	if _, err := l.Reload(); err != nil {
		t.Fatalf("Reload #3: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("expected a coalesced signal to remain buffered")
	}
}

// TestSnapshotIdempotent asserts Snapshot lazily loads and returns stable,
// independent copies.
func TestSnapshotIdempotent(t *testing.T) {
	root := seedAllSidecars(t)
	l := NewLoader(root)

	// No Load called yet: Snapshot must trigger one.
	first := l.Snapshot()
	if first == nil || first.OrgBindingID != "org-77" {
		t.Fatalf("Snapshot lazy load failed: %+v", first)
	}
	second := l.Snapshot()
	if second.OrgBindingID != first.OrgBindingID || second.WorkspaceRoot != first.WorkspaceRoot {
		t.Fatalf("Snapshot not idempotent: %+v vs %+v", first, second)
	}

	// Mutating a returned copy must not affect subsequent snapshots.
	first.DatasourceSecrets["ref-a"] = "tampered"
	third := l.Snapshot()
	if third.DatasourceSecrets["ref-a"] != "dsn-a" {
		t.Fatalf("snapshot copy leaked mutation: %q", third.DatasourceSecrets["ref-a"])
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}