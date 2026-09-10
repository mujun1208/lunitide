package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/capabilitypack"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func packFixture(t *testing.T) (*Engine, *storage.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "packs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e := NewEngine(nil, "test")
	e.skills = skillapp.New(store, store)
	e.SetM8PluginService(m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user"))
	if err = m8app.EnsureBuiltinPlugins(ctx, e.m8plugin); err != nil {
		t.Fatal(err)
	}
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	registry := mcp6.NewRegistry(nil, nil, nil)
	registry.SetSecurityAdapters(func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
		return fn(mcp6.Credentials{Context: ctx})
	}, func(context.Context, *mcp6.Endpoint, mcp6.Credentials) (mcp6.Catalogue, error) {
		return mcp6.Catalogue{Identity: "fixture@1", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}, nil
	}, func(context.Context, *mcp6.Endpoint, string, map[string]any, mcp6.Credentials) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	registry.SetLaunchResolver(func(ctx context.Context, command string, args []string) ([]string, error) {
		return mcp.ResolveLaunchArgs(ctx, command, args, func(_ context.Context, target string) ([]byte, error) {
			// Match the upstream registry response shape. The old nonexistent npm
			// fetch/time presets must fail instead of passing a permissive fake probe.
			switch target {
			case "https://pypi.org/pypi/mcp-server-fetch/json":
				return []byte(`{"info":{"name":"mcp-server-fetch","version":"2026.8.18"}}`), nil
			case "https://pypi.org/pypi/mcp-server-time/json":
				return []byte(`{"info":{"name":"mcp-server-time","version":"2026.8.18"}}`), nil
			default:
				return nil, errors.New("package not found in fixture registry")
			}
		})
	})
	e.SetM6Services(nil, registry, nil)
	e.SetCapabilityPackStore(store.AgentRuntimeRepository())
	return e, store
}
func packGate(t *testing.T, e *Engine, id string) (string, string) {
	t.Helper()
	listed, err := e.m8plugin.List(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range listed.Plugins {
		if p.PluginID == id {
			return p.InstallID, p.State
		}
	}
	t.Fatal("missing gate " + id)
	return "", ""
}
func TestCapabilityPackSharedDependenciesAndRestart(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	gate, _ := packGate(t, e, "web-fetch")
	if _, err := e.m8plugin.Toggle(ctx, m8app.ToggleInput{InstallID: gate, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	a := capabilitypack.Spec{ID: "pack-a", Name: "A", Skills: []string{"docx-writer"}, McpPresetIDs: []string{"fetch"}, ToolGates: []string{"web-fetch"}}
	ra, err := e.capabilityPacks.Install(ctx, a, false)
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.ID = "pack-b"
	b.Name = "B"
	rb, err := e.capabilityPacks.Install(ctx, b, false)
	if err != nil {
		t.Fatal(err)
	}
	eps, err := e.m7mcp.List(ctx, "")
	if err != nil || len(eps) != 1 || !eps[0].Enabled {
		t.Fatalf("MCP %+v %v", eps, err)
	}
	e.SetCapabilityPackStore(store.AgentRuntimeRepository())
	if _, err = e.capabilityPacks.Uninstall(ctx, a.ID, ra.Version); err != nil {
		t.Fatal(err)
	}
	_, state := packGate(t, e, "web-fetch")
	ep, _ := e.m7mcp.Endpoint(ctx, eps[0].EndpointID)
	if state != "enabled" || !ep.Enabled {
		t.Fatal("uninstall A disabled B dependencies")
	}
	if _, err = e.capabilityPacks.Uninstall(ctx, b.ID, rb.Version); err != nil {
		t.Fatal(err)
	}
	_, state = packGate(t, e, "web-fetch")
	ep, _ = e.m7mcp.Endpoint(ctx, eps[0].EndpointID)
	if state != "disabled" || ep.Enabled {
		t.Fatal("last owner did not disable owned dependencies")
	}
	sk, err := store.GetSkillByNameVersion(ctx, "tpl-docx-writer", "1.0.0")
	if err != nil || string(sk.Status) != "published" {
		t.Fatalf("skill not retained %+v %v", sk, err)
	}
	if _, err = e.capabilityPacks.Install(ctx, a, false); err != nil {
		t.Fatalf("remount after uninstall: %v", err)
	}
}

type failingPackExecutor struct {
	capabilitypack.Executor
	failMount, failRelease bool
	beforeRelease          func()
}

func (x *failingPackExecutor) Mount(ctx context.Context, s capabilitypack.Spec) error {
	if x.failMount {
		x.failMount = false
		return errors.New("injected after dependency commits")
	}
	return x.Executor.Mount(ctx, s)
}
func (x *failingPackExecutor) Release(ctx context.Context, r capabilitypack.Resource) error {
	if x.beforeRelease != nil {
		fn := x.beforeRelease
		x.beforeRelease = nil
		fn()
	}
	if x.failRelease {
		x.failRelease = false
		return errors.New("injected uninstall failure")
	}
	return x.Executor.Release(ctx, r)
}

func TestCapabilityPackConcurrentReferencePreventsRelease(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	gate, _ := packGate(t, e, "web-fetch")
	if _, err := e.m8plugin.Toggle(ctx, m8app.ToggleInput{InstallID: gate, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	adapter := &failingPackExecutor{Executor: enginePackExecutor{e}}
	first := capabilitypack.New(store.AgentRuntimeRepository(), adapter)
	a := capabilitypack.Spec{ID: "pack-race-a", Name: "A", ToolGates: []string{"web-fetch"}}
	record, err := first.Install(ctx, a, false)
	if err != nil {
		t.Fatal(err)
	}
	adapter.beforeRelease = func() {
		b := a
		b.ID = "pack-race-b"
		b.Name = "B"
		if _, err := capabilitypack.New(store.AgentRuntimeRepository(), enginePackExecutor{e}).Install(ctx, b, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = first.Uninstall(ctx, a.ID, record.Version); err != nil {
		t.Fatal(err)
	}
	_, state := packGate(t, e, "web-fetch")
	if state != "enabled" {
		t.Fatal("reference added after release inspection was ignored by actual gate transaction")
	}
}
func TestCapabilityPackFailureResumeAndManualClaim(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	gate, _ := packGate(t, e, "web-fetch")
	if _, err := e.m8plugin.Toggle(ctx, m8app.ToggleInput{InstallID: gate, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	executor := &failingPackExecutor{Executor: enginePackExecutor{e}, failMount: true}
	service := capabilitypack.New(store.AgentRuntimeRepository(), executor)
	spec := capabilitypack.Spec{ID: "pack-resume", Name: "Resume", McpPresetIDs: []string{"fetch"}, ToolGates: []string{"web-fetch"}}
	record, err := service.Install(ctx, spec, false)
	if err == nil || record.State != "failed" {
		t.Fatalf("failure %+v %v", record, err)
	}
	service = capabilitypack.New(store.AgentRuntimeRepository(), executor)
	record, err = service.Install(ctx, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	eps, _ := e.m7mcp.List(ctx, "")
	if len(eps) != 1 {
		t.Fatal("retry duplicated endpoint")
	}
	replay, err := service.Install(ctx, spec, false)
	if err != nil || replay.Version != record.Version {
		t.Fatal("ACK replay changed committed version")
	}
	wrong := spec
	wrong.ToolGates = []string{"workspace"}
	if _, err = service.Install(ctx, wrong, false); !errors.Is(err, capabilitypack.ErrConflict) {
		t.Fatal("different manifest accepted")
	}
	// A manual disable then enable claims independent ownership in the actual
	// component transactions, even though a pack originally enabled it.
	for _, enabled := range []bool{false, true} {
		if _, err = e.m8plugin.Toggle(ctx, m8app.ToggleInput{InstallID: gate, Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
	}
	executor.failRelease = true
	record, err = service.Uninstall(ctx, spec.ID, record.Version)
	if err == nil || record.State != "failed" || record.Desired != "uninstalled" {
		t.Fatalf("uninstall failure lost %+v %v", record, err)
	}
	service = capabilitypack.New(store.AgentRuntimeRepository(), executor)
	record, err = service.Uninstall(ctx, spec.ID, record.Version)
	if err != nil || record.State != "uninstalled" {
		t.Fatalf("resume %+v %v", record, err)
	}
	_, state := packGate(t, e, "web-fetch")
	if state != "enabled" {
		t.Fatal("manual ownership was disabled")
	}
}

func TestPackFailureUserMessagesAreChinese(t *testing.T) {
	r := bridge.Request{ID: "pack-zh", Method: "plugin.pack.uninstall"}
	cases := []struct {
		err  error
		code string
	}{
		{capabilitypack.ErrNotFound, "PACK_NOT_FOUND"},
		{capabilitypack.ErrConflict, "PACK_CONFLICT"},
		{capabilitypack.ErrUnavailable, "PACK_OPERATION_FAILED"},
	}
	for _, tc := range cases {
		got := packFailure(r, tc.err)
		if got.OK || got.Error == nil || got.Error.Code != tc.code {
			t.Fatalf("%v: %+v", tc.err, got)
		}
		if strings.Contains(got.Error.Message, "capability pack") || !officeUserMessageHasHan(got.Error.Message) {
			t.Fatalf("%v leaked %q", tc.err, got.Error.Message)
		}
	}
}

func TestPluginPackListLocalizesStoredEnglishError(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	seed := capabilitypack.Record{
		Spec:      capabilitypack.Spec{ID: "pack-browser", Name: "浏览器工作包", Skills: []string{"web-search"}},
		Digest:    strings.Repeat("b", 64),
		State:     "failed",
		Desired:   "installed",
		Version:   1,
		Error:     "gate web-fetch is not installed",
		CreatedAt: "2026-09-10T00:00:00Z",
		UpdatedAt: "2026-09-10T00:00:00Z",
	}
	if err := store.AgentRuntimeRepository().TransactPack(ctx, func(tx capabilitypack.Tx) error {
		return tx.SavePack(seed, 0)
	}); err != nil {
		t.Fatal(err)
	}
	resp := handlePluginPackList(e, ctx, bridge.Request{ID: "pack-list-zh", Method: "plugin.pack.list"})
	if !resp.OK {
		t.Fatalf("list: %+v", resp.Error)
	}
	var body struct {
		Items []capabilitypack.Record `json:"items"`
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 {
		t.Fatalf("items=%d payload=%s", len(body.Items), raw)
	}
	got := body.Items[0].Error
	if strings.Contains(got, "is not installed") || strings.Contains(got, "gate web-fetch") || !officeUserMessageHasHan(got) {
		t.Fatalf("stored pack error must stay Chinese, got %q", got)
	}
}
