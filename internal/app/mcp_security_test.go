package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMcpSecurityDurablePinCredentialRotationAndExplicitReview(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "mcp-security.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	catalog := mcp6.Catalogue{Identity: "fixture@1", Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	invocations := 0
	unauthorized := false
	newRegistry := func() *mcp6.Registry {
		r := mcp6.NewRegistry(nil, nil, nil)
		r.SetSecurityAdapters(func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
			return fn(mcp6.Credentials{Context: ctx})
		}, func(context.Context, *mcp6.Endpoint, mcp6.Credentials) (mcp6.Catalogue, error) { return catalog, nil }, func(_ context.Context, ep *mcp6.Endpoint, _ string, _ map[string]any, _ mcp6.Credentials) (map[string]any, error) {
			if unauthorized {
				return nil, mcp6.ErrCredentialRevoked
			}
			if err := catalog.Verify(ep.Pin); err != nil {
				return nil, err
			}
			invocations++
			return map[string]any{"ok": true}, nil
		})
		return r
	}
	e.SetM6Services(nil, newRegistry(), nil)
	added, err := e.m7mcp.Add(ctx, m7app.McpAddInput{Origin: "manual", Transport: "https", URL: "https://fixture.invalid", RiskConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, added.EndpointID)
	if err != nil || ep.Security.PinJSON == "" || ep.Security.Version != 1 {
		t.Fatalf("pin=%+v %v", ep.Security, err)
	}
	if result := handleMcpToggle(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": ep.EndpointID, "enabled": true})); !result.OK {
		t.Fatalf("toggle: %+v", result.Error)
	}
	runtimeID := chatMcpEndpointID(ep.EndpointID)
	if _, err = e.mcp6Registry.Invoke(ctx, runtimeID, "lookup", nil); err != nil {
		t.Fatal(err)
	}
	unauthorized = true
	if _, err = e.mcp6Registry.Invoke(ctx, runtimeID, "lookup", nil); !errors.Is(err, mcp6.ErrCredentialRevoked) {
		t.Fatalf("401=%v", err)
	}
	ep, err = e.m7mcp.Endpoint(ctx, ep.EndpointID)
	if err != nil || ep.State != "degraded" {
		t.Fatalf("401 persisted=%+v %v", ep, err)
	}
	if _, err = e.m7mcp.BindCredential(ctx, ep.EndpointID, ep.Security.Version, "secretref:mcp/"+runtimeID+"/new", ""); err != nil {
		t.Fatal(err)
	}
	unauthorized = false
	if err = e.syncSettingsMcp(ctx, ep.EndpointID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.mcp6Registry.Invoke(ctx, runtimeID, "lookup", nil); err != nil {
		t.Fatal(err)
	}
	catalog.Identity = "fixture@2"
	if _, err = e.mcp6Registry.Invoke(ctx, runtimeID, "lookup", nil); !errors.Is(err, mcp6.ErrCapabilityDrift) {
		t.Fatalf("identity drift=%v", err)
	}
	ep, err = e.m7mcp.Endpoint(ctx, ep.EndpointID)
	if err != nil || ep.State != "quarantined" {
		t.Fatalf("drift persisted=%+v %v", ep, err)
	}
	// A new engine registry must not silently approve the newly observed server.
	e.SetM6Services(nil, newRegistry(), nil)
	e.HydrateMcpGatewayFromSettings(ctx)
	if len(e.mcp6Registry.ReadyToolSnapshot()) != 0 {
		t.Fatal("restart discarded quarantine")
	}
	inspect := handleMcpSecurityReview(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": ep.EndpointID, "expectedVersion": ep.Security.Version, "action": "inspect"}))
	if !inspect.OK {
		t.Fatalf("inspect: %+v", inspect.Error)
	}
	data := inspect.Payload.(map[string]any)
	payload := map[string]any{"endpointId": ep.EndpointID, "expectedVersion": ep.Security.Version, "action": "accept", "observedDigest": data["observedDigest"], "confirmed": true}
	// Changed again after preview cannot be accepted using the earlier digest.
	catalog.Identity = "fixture@3"
	if res := handleMcpSecurityReview(e, ctx, lifecyclePayload(t, payload)); res.OK {
		t.Fatal("preview/accept race accepted drift")
	}
	catalog.Identity = "fixture@2"
	if res := handleMcpSecurityReview(e, ctx, lifecyclePayload(t, payload)); !res.OK {
		t.Fatalf("accept: %+v", res.Error)
	}
	if res := handleMcpSecurityReview(e, ctx, lifecyclePayload(t, payload)); !res.OK {
		t.Fatalf("accept ACK replay: %+v", res.Error)
	}
	if err = e.syncSettingsMcp(ctx, ep.EndpointID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.mcp6Registry.Invoke(ctx, runtimeID, "lookup", nil); err != nil {
		t.Fatal(err)
	}
	if invocations != 3 {
		t.Fatal(invocations)
	}
}
