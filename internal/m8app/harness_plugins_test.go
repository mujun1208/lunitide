package m8app_test

import (
	"context"
	"runtime"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestHarnessPluginsRosterIsUniqueAndLarge(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range m8app.HarnessPlugins() {
		if spec.ID == "" || spec.Kind == "" || spec.Title == "" {
			t.Fatalf("incomplete spec %#v", spec)
		}
		if !m8core.ValidPluginKind(spec.Kind) {
			t.Fatalf("%s kind %q", spec.ID, spec.Kind)
		}
		if seen[spec.ID] {
			t.Fatalf("duplicate plugin id %q", spec.ID)
		}
		seen[spec.ID] = true
	}
	if len(seen) < 130 {
		t.Fatalf("roster size = %d, want >= 130", len(seen))
	}
}

func TestEnsureBuiltinPluginsSeedsAndIsIdempotent(t *testing.T) {
	svc, _ := openPluginService(t)
	ctx := context.Background()
	if err := m8app.EnsureBuiltinPlugins(ctx, svc); err != nil {
		t.Fatal(err)
	}
	first, err := svc.List(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Plugins) < 130 {
		t.Fatalf("seeded %d, want >= 130", len(first.Plugins))
	}
	var hmr, bash, pwsh *m8app.PluginListItem
	for i := range first.Plugins {
		p := &first.Plugins[i]
		switch p.PluginID {
		case "hmr":
			hmr = p
		case "tool-bash":
			bash = p
		case "tool-pwsh":
			pwsh = p
		}
	}
	if hmr == nil || hmr.State != m8core.InstallDisabled {
		t.Fatalf("hmr = %+v, want disabled", hmr)
	}
	if runtime.GOOS == "windows" {
		if bash == nil || bash.State != m8core.InstallDisabled {
			t.Fatalf("tool-bash on windows = %+v, want disabled", bash)
		}
		if pwsh == nil || pwsh.State != m8core.InstallEnabled {
			t.Fatalf("tool-pwsh on windows = %+v, want enabled", pwsh)
		}
	} else {
		if bash == nil || bash.State != m8core.InstallEnabled {
			t.Fatalf("tool-bash = %+v, want enabled", bash)
		}
		if pwsh == nil || pwsh.State != m8core.InstallDisabled {
			t.Fatalf("tool-pwsh = %+v, want disabled", pwsh)
		}
	}
	if err := m8app.EnsureBuiltinPlugins(ctx, svc); err != nil {
		t.Fatal(err)
	}
	second, err := svc.List(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Plugins) != len(first.Plugins) {
		t.Fatalf("idempotent seed grew %d → %d", len(first.Plugins), len(second.Plugins))
	}
}

func TestCreateAndMountEnablesChatPlugin(t *testing.T) {
	svc, _ := openPluginService(t)
	ctx := context.Background()
	res, err := svc.CreateAndMount(ctx, m8app.DevCreateInput{
		WorkspaceID: "chat",
		Entrypoint:  "builtin://chat/demo-reader",
		Manifest: map[string]any{
			"id":           "demo-reader",
			"semver":       "1.0.0",
			"publisher":    "chat",
			"kind":         "tool",
			"capabilities": []string{"chat.created"},
			"permissions":  map[string]any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != m8core.InstallEnabled {
		t.Fatalf("state = %s, want enabled", res.State)
	}
	again, err := svc.CreateAndMount(ctx, m8app.DevCreateInput{
		WorkspaceID: "chat",
		Entrypoint:  "builtin://chat/demo-reader",
		Manifest: map[string]any{
			"pluginId":     "demo-reader",
			"semver":       "1.0.0",
			"publisher":    "chat",
			"kind":         "tool",
			"capabilities": []string{"chat.created"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.InstallID != res.InstallID {
		t.Fatalf("remount created a second install %s vs %s", again.InstallID, res.InstallID)
	}
}
