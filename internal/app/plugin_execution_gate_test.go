package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestPluginExecutionGateAllRuntimeEntrypoints(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e.SetToolRuntime(runtime) // both injection orders must install the gate
	svc := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
	e.SetM8PluginService(svc)
	if err := m8app.EnsureBuiltinPlugins(ctx, svc); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	installID := ""
	for _, p := range list.Plugins {
		if p.PluginID == "workspace" {
			installID = p.InstallID
		}
	}
	if installID == "" {
		t.Fatal("missing workspace plugin")
	}
	toggle := func(enabled bool) {
		raw, _ := json.Marshal(map[string]any{"installId": installID, "enabled": enabled})
		if res := handlePluginToggle(e, ctx, bridge.Request{Payload: raw}); !res.OK {
			t.Fatalf("toggle: %+v", res.Error)
		}
	}
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"path":"gate-proof.txt","content":"isolated fixture"}`)
	toggle(false)
	paths := map[string]func() error{
		"chat": func() error {
			_, err := e.executeUserTool(ctx, executionModeFullAccess, session, "workspace.write", args)
			return err
		},
		"delegate": func() error {
			_, err := runtime.Execute(ctx, toolruntime.FullAccess, session, "workspace.write", args, true)
			return err
		},
		"streaming": func() error {
			_, err := runtime.ExecuteStreaming(ctx, toolruntime.FullAccess, session, "workspace.write", args, true, nil)
			return err
		},
		"unconfined": func() error {
			_, err := runtime.ExecuteUnconfined(ctx, session, "workspace.write", args, true)
			return err
		},
		"unconfined_streaming": func() error {
			_, err := runtime.ExecuteUnconfinedStreaming(ctx, session, "workspace.write", args, true, nil)
			return err
		},
	}
	for name, call := range paths {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, m8app.ErrBindingInactive) {
				t.Fatalf("disabled capability executed: %v", err)
			}
		})
	}
	toggle(true)
	if err := paths["chat"](); err != nil {
		t.Fatalf("re-enabled capability did not recover: %v", err)
	}
	// A database outage must not become an implicit legacy grant.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := paths["delegate"](); err == nil {
		t.Fatal("storage outage bypassed the gate")
	}
}

func TestPluginExecutionGateAllowsUnseededLegacyBuiltin(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
	if err := svc.RequireEnabled(ctx, "workspace"); err != nil {
		t.Fatalf("unseeded legacy capability refused: %v", err)
	}
}
