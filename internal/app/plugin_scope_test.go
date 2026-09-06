package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestPluginScopeCancelsActualRuntimeAndRejectsLateChatResult(t *testing.T) {
	for _, path := range []string{"runtime", "chat-hook"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "scope.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtime, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			e := NewEngine(nil, "test")
			svc := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
			e.SetM8PluginService(svc)
			e.SetToolRuntime(runtime)
			if err = m8app.EnsureBuiltinPlugins(ctx, svc); err != nil {
				t.Fatal(err)
			}
			list, err := svc.List(ctx, "", "")
			if err != nil {
				t.Fatal(err)
			}
			id := ""
			for _, item := range list.Plugins {
				if item.PluginID == "web-fetch" {
					id = item.InstallID
				}
			}
			started := make(chan struct{})
			seenCancel := make(chan struct{})
			done := make(chan error, 1)
			block := func(op context.Context) { close(started); <-op.Done(); close(seenCancel) }
			runtime.SetWebFetcher(func(op context.Context, url string) (networkpolicy.FetchResult, error) {
				block(op)
				return networkpolicy.FetchResult{ContentType: "text/plain", Body: []byte("late result")}, nil
			})
			e.toolExecHook = func(op context.Context, _ executionMode, _, _ string, _ json.RawMessage) (toolruntime.Result, error) {
				block(op)
				return toolruntime.Result{Output: "late result"}, nil
			}
			go func() {
				var out toolruntime.Result
				var err error
				if path == "runtime" {
					out, err = runtime.Execute(ctx, toolruntime.FullAccess, "fixture", "web.fetch", json.RawMessage(`{"url":"https://fixture.invalid"}`), true)
				} else {
					out, err = e.executeUserTool(ctx, executionModeFullAccess, "fixture", "web.fetch", json.RawMessage(`{"url":"https://fixture.invalid"}`))
				}
				if out.Output != "" {
					done <- errors.New("late revoked result published")
					return
				}
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("operation not started")
			}
			if _, err = svc.Toggle(ctx, m8app.ToggleInput{InstallID: id, Enabled: false}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-seenCancel:
			case <-time.After(time.Second):
				t.Fatal("actual adapter context not cancelled")
			}
			select {
			case err = <-done:
				if !errors.Is(err, m8app.ErrBindingInactive) {
					t.Fatalf("wrong revoked outcome: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("operation stranded")
			}
		})
	}
}
