package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcapp"
	"github.com/lunitide/lunitide/internal/mcp6"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type lifecycleNoSecretLease struct{}

func (lifecycleNoSecretLease) WithLease(_ context.Context, _ string, fn func([]byte) error) error {
	return fn(nil)
}

func newMcpLifecycleFixture(t *testing.T) (*Engine, *atomic.Bool, *atomic.Int64) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "lifecycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e := NewEngine(nil, "test")
	repo := store.AgentRuntimeRepository()
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(repo))
	e.SetMcMarketService(mcapp.New(repo))
	failing, calls := &atomic.Bool{}, &atomic.Int64{}
	registry := mcp6.NewRegistry(func(context.Context, *mcp6.Endpoint) error {
		if failing.Load() {
			return errors.New("isolated offline fixture")
		}
		return nil
	}, func(context.Context, *mcp6.Endpoint, string, map[string]any, []byte) (map[string]any, error) {
		calls.Add(1)
		return map[string]any{"ok": true}, nil
	}, lifecycleNoSecretLease{})
	registry.SetDescribeFunc(func(context.Context, *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
		return map[string]mcp6.ToolSchema{"fixture_tool": {InputSchema: json.RawMessage(`{"type":"object"}`)}}, nil
	})
	e.SetM6Services(nil, registry, nil)
	return e, failing, calls
}

func lifecyclePayload(t *testing.T, payload map[string]any) bridge.Request {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return bridge.Request{Payload: raw}
}

func addLifecycleEndpoint(t *testing.T, e *Engine) string {
	t.Helper()
	ep, err := e.m7mcp.Add(context.Background(), m7app.McpAddInput{Origin: "manual", Transport: "stdio", Command: "npx", Args: []string{"fixture-no-execution"}, RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	return ep.EndpointID
}

func TestMcpLifecycleUninstallRevokesChatAndDurableGrant(t *testing.T) {
	e, _, calls := newMcpLifecycleFixture(t)
	ctx := context.Background()
	id := addLifecycleEndpoint(t, e)
	if got := e.mcp6Registry.ReadyToolSnapshot(); len(got) != 0 {
		t.Fatal("disabled add admitted callable tools")
	}
	if res := handleMcpToggle(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id, "enabled": true})); !res.OK {
		t.Fatalf("enable: %+v", res.Error)
	}
	if _, err := e.mcp6Registry.Invoke(ctx, chatMcpEndpointID(id), "fixture_tool", nil); err != nil {
		t.Fatal(err)
	}
	token, _, err := e.mcmarket.IssueConfirmToken(ctx, mcapp.ConfirmMethodUninstall, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if res := handleMcConnectorUninstall(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id, "confirmToken": token})); !res.OK {
		t.Fatalf("uninstall: %+v", res.Error)
	}
	rows, err := e.m7mcp.List(ctx, "")
	if err != nil || len(rows) != 1 || rows[0].State != "revoked" || rows[0].Enabled {
		t.Fatalf("durable grant: %+v %v", rows, err)
	}
	if _, err := e.mcp6Registry.Invoke(ctx, chatMcpEndpointID(id), "fixture_tool", nil); !errors.Is(err, mcp6.ErrEndpointRevoked) {
		t.Fatalf("retained tool: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("uninstalled endpoint reached transport")
	}
}

func TestMcpLifecycleDirectSettingsDisableBlocksRetainedTool(t *testing.T) {
	e, _, calls := newMcpLifecycleFixture(t)
	ctx := context.Background()
	id := addLifecycleEndpoint(t, e)
	if res := handleMcpToggle(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id, "enabled": true})); !res.OK {
		t.Fatalf("enable: %+v", res.Error)
	}
	// Bypass the UI handler: execution must still consult committed grants.
	if _, err := e.m7mcp.Toggle(ctx, id, false, "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mcp6Registry.Invoke(ctx, chatMcpEndpointID(id), "fixture_tool", nil); !errors.Is(err, mcp6.ErrEndpointRevoked) {
		t.Fatalf("disabled call: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("disabled endpoint reached transport")
	}
}

func TestMcpLifecycleHealthObservesTransportAndRecovers(t *testing.T) {
	e, failing, _ := newMcpLifecycleFixture(t)
	ctx := context.Background()
	failing.Store(true)
	id := addLifecycleEndpoint(t, e)
	rows, err := e.m7mcp.List(ctx, "")
	if err != nil || rows[0].State != "degraded" {
		t.Fatalf("offline reported healthy: %+v %v", rows, err)
	}
	if res := handleMcpToggle(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id, "enabled": true})); res.OK {
		t.Fatal("offline enable reported success")
	}
	failing.Store(false)
	for i := 0; i < 2; i++ { // repeated ready health checks must persist, too
		res := handleMcpHealth(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id}))
		if !res.OK {
			t.Fatalf("health: %+v", res.Error)
		}
		rows, err = e.m7mcp.List(ctx, "")
		if err != nil || rows[0].State != "ready" || rows[0].LastHealthAt == "" {
			t.Fatalf("recovery not persisted: %+v %v", rows, err)
		}
	}
	if current, err := e.mcp6Registry.Get(chatMcpEndpointID(id)); err != nil || current.State != "ready" {
		t.Fatalf("gateway did not recover: %+v %v", current, err)
	}
}

func TestMcpLifecycleUpdateReplacesRuntimeTarget(t *testing.T) {
	e, _, _ := newMcpLifecycleFixture(t)
	ctx := context.Background()
	id := addLifecycleEndpoint(t, e)
	if res := handleMcpToggle(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id, "enabled": true})); !res.OK {
		t.Fatalf("enable: %+v", res.Error)
	}
	previous, err := e.mcp6Registry.Get(chatMcpEndpointID(id))
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := e.mcmarket.IssueConfirmToken(ctx, mcapp.ConfirmMethodUpdate, id, "")
	if err != nil {
		t.Fatal(err)
	}
	res := handleMcConnectorUpdate(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": id, "args": []string{"fixture-updated"}, "confirmToken": token}))
	if !res.OK {
		t.Fatalf("update: %+v", res.Error)
	}
	current, err := e.mcp6Registry.Get(chatMcpEndpointID(id))
	if err != nil || len(current.Args) != 1 || current.Args[0] != "fixture-updated" || current.Version <= previous.Version {
		t.Fatalf("runtime target not replaced: %+v %v", current, err)
	}
	if _, err := e.mcp6Registry.Invoke(ctx, chatMcpEndpointID(id), "fixture_tool", nil); err != nil {
		t.Fatalf("updated tool failed: %v", err)
	}
}
