package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/mcapp"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func mcInstallToken(t *testing.T, service *mcapp.Service, url string) string {
	t.Helper()
	sum := sha256.Sum256([]byte("https||" + url))
	token, _, err := service.IssueConfirmToken(context.Background(), mcapp.ConfirmMethodInstall, "fp:"+hex.EncodeToString(sum[:]), "")
	if err != nil {
		t.Fatal(err)
	}
	return token
}
func TestMcMarketUsesAuthenticatedPinsAndPreservesQuarantine(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	e.SetMcMarketService(mcapp.New(store.AgentRuntimeRepository()))
	unauthorized := false
	registry := mcp6.NewRegistry(nil, nil, nil)
	registry.SetSecurityAdapters(func(ctx context.Context, _ *mcp6.Endpoint, fn func(mcp6.Credentials) error) error {
		return fn(mcp6.Credentials{Context: ctx})
	}, func(_ context.Context, ep *mcp6.Endpoint, _ mcp6.Credentials) (mcp6.Catalogue, error) {
		if unauthorized {
			return mcp6.Catalogue{}, mcp6.ErrCredentialRevoked
		}
		return mcp6.Catalogue{Identity: "fixture|" + ep.URL, Tools: map[string]mcp6.ToolSchema{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}}, nil
	}, func(context.Context, *mcp6.Endpoint, string, map[string]any, mcp6.Credentials) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	e.SetM6Services(nil, registry, nil)
	url := "https://8.8.8.8/market-fixture"
	installed, _, err := e.mcmarket.Install(ctx, mcapp.InstallInput{Origin: "manual", Transport: "https", URL: url, ConfirmToken: mcInstallToken(t, e.mcmarket, url)})
	if err != nil || installed.State != "ready" {
		t.Fatalf("install %+v %v", installed, err)
	}
	ep, err := e.m7mcp.Endpoint(ctx, installed.EndpointID)
	if err != nil || ep.Security.PinJSON == "" {
		t.Fatalf("market bypassed pin persistence %+v %v", ep, err)
	}
	if response := handleMcpToggle(e, ctx, lifecyclePayload(t, map[string]any{"endpointId": ep.EndpointID, "enabled": true})); !response.OK {
		t.Fatal(response.Error)
	}
	token, _, err := e.mcmarket.IssueConfirmToken(ctx, mcapp.ConfirmMethodUpdate, ep.EndpointID, "")
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := e.mcmarket.Update(ctx, mcapp.UpdateInput{EndpointID: ep.EndpointID, URL: "https://8.8.8.8/changed-fixture", ConfirmToken: token})
	if !errors.Is(err, mcp6.ErrCapabilityDrift) || updated.State != "quarantined" {
		t.Fatalf("changed identity falsely ready: %+v %v", updated, err)
	}
	actual, err := e.m7mcp.Endpoint(ctx, ep.EndpointID)
	if err != nil || actual.State != "quarantined" || actual.Security.Version <= ep.Security.Version {
		t.Fatalf("quarantine/epoch not persisted %+v %v", actual, err)
	}
	if _, err = registry.Invoke(ctx, chatMcpEndpointID(ep.EndpointID), "lookup", nil); err == nil {
		t.Fatal("updated unreviewed target remained executable")
	}
	unauthorized = true
	url = "https://8.8.8.8/unauthorized-fixture"
	failed, _, err := e.mcmarket.Install(ctx, mcapp.InstallInput{Origin: "manual", Transport: "https", URL: url, ConfirmToken: mcInstallToken(t, e.mcmarket, url)})
	if !errors.Is(err, mcp6.ErrCredentialRevoked) || failed.State != "degraded" {
		t.Fatalf("401 falsely successful %+v %v", failed, err)
	}
}

type mcReadFaultStore struct {
	mcapp.UnitOfWork
	fail *bool
}
type mcReadFaultTx struct {
	mcapp.Tx
	fail *bool
}

func (tx mcReadFaultTx) GetMcpEndpoint(id string) (m7flow.McpEndpointConfig, error) {
	if *tx.fail {
		return m7flow.McpEndpointConfig{}, errors.New("fixture read failure after probe")
	}
	return tx.Tx.GetMcpEndpoint(id)
}
func (s mcReadFaultStore) TransactMc(ctx context.Context, fn func(mcapp.Tx) error) error {
	return s.UnitOfWork.TransactMc(ctx, func(tx mcapp.Tx) error { return fn(mcReadFaultTx{tx, s.fail}) })
}

type mcProbeFunc func(context.Context, m7flow.McpEndpointConfig) (string, error)

func (f mcProbeFunc) Probe(ctx context.Context, ep m7flow.McpEndpointConfig) (string, error) {
	return f(ctx, ep)
}
func TestMcMarketReadFailureCannotReturnOldReady(t *testing.T) {
	e, store := packFixture(t)
	ctx := context.Background()
	fail := false
	service := mcapp.New(mcReadFaultStore{store.AgentRuntimeRepository(), &fail})
	url := "https://8.8.8.8/storage-fixture"
	installed, _, err := service.Install(ctx, mcapp.InstallInput{Origin: "manual", Transport: "https", URL: url, ConfirmToken: mcInstallToken(t, service, url)})
	if err != nil {
		t.Fatal(err)
	}
	service.SetProber(mcProbeFunc(func(context.Context, m7flow.McpEndpointConfig) (string, error) {
		fail = true
		return "", errors.New("probe unavailable")
	}))
	token, _, err := service.IssueConfirmToken(ctx, mcapp.ConfirmMethodUpdate, installed.EndpointID, "")
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := service.Update(ctx, mcapp.UpdateInput{EndpointID: installed.EndpointID, URL: "https://8.8.8.8/changed-storage-fixture", ConfirmToken: token})
	if err == nil || updated.State == "ready" {
		t.Fatalf("storage failure reported old ready %+v %v", updated, err)
	}
	_ = e
}
