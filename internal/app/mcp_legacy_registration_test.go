package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func legacyRegistryFixture(catalog mcp6.Catalogue, invoke mcp6.VerifiedInvokeFunc) *mcp6.Registry {
	r := mcp6.NewRegistry(nil, nil, nil)
	r.SetSecurityAdapters(func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
		return fn(mcp6.Credentials{Context: ctx})
	}, func(context.Context, *mcp6.Endpoint, mcp6.Credentials) (mcp6.Catalogue, error) { return catalog, nil }, invoke)
	r.SetLaunchResolver(func(_ context.Context, _ string, args []string) ([]string, error) {
		out := append([]string(nil), args...)
		if out[0] == "fixture-server" {
			out[0] = "fixture-server@1.2.3"
		}
		return out, nil
	})
	r.SetLaunchVerifier(func(context.Context, mcp6.EndpointInput) (string, error) { return strings.Repeat("b", 64), nil })
	return r
}

func TestLegacyMcpPersistsLockedDescriptorAndRevocationAcrossRestart(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	catalog := mcp6.Catalogue{Identity: "fixture-server@1.2.3", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	pin, err := catalog.Pin()
	if err != nil {
		t.Fatal(err)
	}
	var block atomic.Bool
	started := make(chan struct{}, 1)
	invoke := func(ctx context.Context, _ *mcp6.Endpoint, _ string, _ map[string]any, _ mcp6.Credentials) (map[string]any, error) {
		if block.Load() {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return map[string]any{"ok": true}, nil
	}
	e.SetM6Services(nil, legacyRegistryFixture(catalog, invoke), nil)
	payload := map[string]any{"endpoint": map[string]any{"transport": "stdio", "command": "npx", "args": []string{"fixture-server"}}, "capabilityPin": pin}
	registered := handleMcp6Register(e, ctx, lifecyclePayload(t, payload))
	if !registered.OK {
		t.Fatalf("register: %+v", registered.Error)
	}
	var result struct {
		EndpointID string `json:"endpointId"`
	}
	if err = json.Unmarshal(mustJSON(registered.Payload), &result); err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, "mcp-"+result.EndpointID)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Command != "npx" || ep.ArgsJSON != `["fixture-server"]` || !strings.Contains(ep.Security.LaunchArgsJSON, "fixture-server@1.2.3") || ep.Security.PinJSON == "" || ep.Security.Version != 1 || !ep.Enabled {
		t.Fatalf("incomplete durable descriptor: %+v", ep)
	}
	e.SetM6Services(nil, legacyRegistryFixture(catalog, invoke), nil)
	e.HydrateMcpGatewayFromSettings(ctx)
	if _, err = e.mcp6Registry.Invoke(ctx, result.EndpointID, "lookup", nil); err != nil {
		t.Fatalf("restart invoke: %v", err)
	}
	if retry := handleMcp6Register(e, ctx, lifecyclePayload(t, payload)); !retry.OK {
		t.Fatalf("ACK replay: %+v", retry.Error)
	}
	block.Store(true)
	done := make(chan error, 1)
	go func() { _, err := e.mcp6Registry.Invoke(ctx, result.EndpointID, "lookup", nil); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("invoke did not start")
	}
	revoked := handleMcp6Revoke(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": result.EndpointID, "reason": "manual"}))
	if !revoked.OK {
		t.Fatalf("revoke: %+v", revoked.Error)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("revoked invoke succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revocation did not cancel IO")
	}
	block.Store(false)
	e.SetM6Services(nil, legacyRegistryFixture(catalog, invoke), nil)
	e.HydrateMcpGatewayFromSettings(ctx)
	if len(e.mcp6Registry.ReadyToolSnapshot()) != 0 {
		t.Fatal("revoked endpoint rehydrated")
	}
	if retry := handleMcp6Register(e, ctx, lifecyclePayload(t, payload)); retry.OK {
		t.Fatal("old registration revived revoked endpoint")
	}
}

type legacyAuditFault struct{ m7app.McpUnitOfWork }
type legacyAuditTx struct{ m7app.McpTx }

func (tx legacyAuditTx) AppendAuditEvent(audit.Event) (audit.Event, error) {
	return audit.Event{}, errors.New("fixture audit refused")
}
func (tx legacyAuditTx) PutMcpSecurity(id string, expected int64, s m7flow.McpEndpointSecurity) error {
	return tx.McpTx.(interface {
		PutMcpSecurity(string, int64, m7flow.McpEndpointSecurity) error
	}).PutMcpSecurity(id, expected, s)
}
func (s legacyAuditFault) TransactMcp(ctx context.Context, fn func(m7app.McpTx) error) error {
	return s.McpUnitOfWork.TransactMcp(ctx, func(tx m7app.McpTx) error { return fn(legacyAuditTx{tx}) })
}

func TestLegacyMcpPersistenceFailureHasNoCallableGhost(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "legacy-fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(legacyAuditFault{store.AgentRuntimeRepository()}))
	catalog := mcp6.Catalogue{Identity: "fixture", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	pin, err := catalog.Pin()
	if err != nil {
		t.Fatal(err)
	}
	e.SetM6Services(nil, legacyRegistryFixture(catalog, nil), nil)
	result := handleMcp6Register(e, ctx, lifecyclePayload(t, map[string]any{"endpoint": map[string]any{"transport": "stdio", "command": "npx", "args": []string{"fixture-server"}}, "capabilityPin": pin}))
	if result.OK {
		t.Fatal("failed audit reported success")
	}
	if len(e.mcp6Registry.ReadyToolSnapshot()) != 0 {
		t.Fatal("failed commit left executable ghost")
	}
	rows, err := e.m7mcp.List(ctx, "")
	if err != nil || len(rows) != 0 {
		t.Fatalf("failed commit persisted rows: %+v %v", rows, err)
	}
}

func TestLegacyMcpConcurrentRegistrationCreatesOneGrant(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "legacy-concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	catalog := mcp6.Catalogue{Identity: "fixture", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	pin, err := catalog.Pin()
	if err != nil {
		t.Fatal(err)
	}
	e.SetM6Services(nil, legacyRegistryFixture(catalog, nil), nil)
	request := lifecyclePayload(t, map[string]any{"endpoint": map[string]any{"transport": "stdio", "command": "npx", "args": []string{"fixture-server"}}, "capabilityPin": pin})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			response := handleMcp6Register(e, ctx, request)
			if !response.OK {
				t.Errorf("concurrent registration: %+v", response.Error)
			}
		}()
	}
	wg.Wait()
	rows, err := e.m7mcp.List(ctx, "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("grants=%d %v", len(rows), err)
	}
}

func TestLegacyMcpDegradedRecoveryAndRestartPinDrift(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "legacy-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	catalog := mcp6.Catalogue{Identity: "fixture", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	pin, err := catalog.Pin()
	if err != nil {
		t.Fatal(err)
	}
	offline := legacyRegistryFixture(catalog, nil)
	offline.SetSecurityAdapters(func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
		return fn(mcp6.Credentials{Context: ctx})
	}, func(context.Context, *mcp6.Endpoint, mcp6.Credentials) (mcp6.Catalogue, error) {
		return mcp6.Catalogue{}, errors.New("isolated offline fixture")
	}, nil)
	e.SetM6Services(nil, offline, nil)
	request := lifecyclePayload(t, map[string]any{"endpoint": map[string]any{"transport": "stdio", "command": "npx", "args": []string{"fixture-server"}}, "capabilityPin": pin})
	response := handleMcp6Register(e, ctx, request)
	if response.OK {
		t.Fatal("offline registration falsely ready")
	}
	rows, err := e.m7mcp.List(ctx, "")
	if err != nil || len(rows) != 1 || rows[0].State != "degraded" {
		t.Fatalf("offline state: %+v %v", rows, err)
	}
	e.SetM6Services(nil, legacyRegistryFixture(catalog, nil), nil)
	e.HydrateMcpGatewayFromSettings(ctx)
	actual, err := e.m7mcp.Endpoint(ctx, rows[0].EndpointID)
	if err != nil || actual.State != "ready" || len(e.mcp6Registry.ReadyToolSnapshot()) != 1 {
		t.Fatalf("recovery: %+v %v", actual, err)
	}
	catalog.Identity = "changed-server"
	e.SetM6Services(nil, legacyRegistryFixture(catalog, nil), nil)
	e.HydrateMcpGatewayFromSettings(ctx)
	actual, err = e.m7mcp.Endpoint(ctx, rows[0].EndpointID)
	if err != nil || actual.State != "quarantined" || len(e.mcp6Registry.ReadyToolSnapshot()) != 0 {
		t.Fatalf("drift rehydrated: %+v %v", actual, err)
	}
}

func TestLegacyMcpUsesExistingApprovedCredentialIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "legacy-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	catalog := mcp6.Catalogue{Identity: "fixture", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	pin, err := catalog.Pin()
	if err != nil {
		t.Fatal(err)
	}
	r := legacyRegistryFixture(catalog, nil)
	e.SetM6Services(nil, r, nil)
	added, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "https", URL: "https://8.8.8.8/isolated-fixture", RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, added.EndpointID)
	if err != nil {
		t.Fatal(err)
	}
	ref := "secretref:mcp/" + chatMcpEndpointID(ep.EndpointID) + "/fixture"
	if _, err = e.m7mcp.BindCredential(ctx, ep.EndpointID, ep.Security.Version, ref, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = e.m7mcp.Toggle(ctx, ep.EndpointID, true, "fixture"); err != nil {
		t.Fatal(err)
	}
	response := handleMcp6Register(e, ctx, lifecyclePayload(t, map[string]any{"endpoint": map[string]any{"transport": "https", "url": ep.URL, "authRef": ref}, "capabilityPin": pin}))
	if !response.OK {
		t.Fatalf("approved identity rejected: %+v", response.Error)
	}
	var result struct {
		EndpointID string `json:"endpointId"`
	}
	if err = json.Unmarshal(mustJSON(response.Payload), &result); err != nil {
		t.Fatal(err)
	}
	if result.EndpointID != chatMcpEndpointID(ep.EndpointID) {
		t.Fatal("created a different credential identity")
	}
	rows, err := e.m7mcp.List(ctx, "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("duplicate registration: %+v %v", rows, err)
	}
}
