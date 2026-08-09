package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/oklog/ulid/v2"
)

type providerRepositoryStub struct{}

func (providerRepositoryStub) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{}, provider.ErrNotFound
}
func (providerRepositoryStub) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return []provider.Provider{}, nil
}
func (providerRepositoryStub) CreateRequest(context.Context, string, string, any, provider.Provider) (provider.Provider, error) {
	return provider.Provider{}, nil
}
func (providerRepositoryStub) UpdateRequest(context.Context, string, string, any, string, int64, func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return provider.Provider{}, nil
}
func (providerRepositoryStub) DeleteRequest(context.Context, string, string, any, string, int64) (provider.Provider, error) {
	return provider.Provider{}, nil
}

type providerRepositoryWithLegacyID struct{}

func (providerRepositoryWithLegacyID) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{}, provider.ErrNotFound
}
func (providerRepositoryWithLegacyID) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	now := time.Now().UTC()
	return []provider.Provider{{ID: ulid.Make().String(), LegacyID: "must-not-cross-bridge", Name: "Provider", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.com", Models: []provider.Model{{ModelID: "model", DisplayName: "Model", IsDefault: true}}, Status: provider.StatusEnabled, CredentialState: provider.CredentialMissing, CreatedAt: now, UpdatedAt: now, Version: 1}}, nil
}
func (providerRepositoryWithLegacyID) CreateRequest(context.Context, string, string, any, provider.Provider) (provider.Provider, error) {
	return provider.Provider{}, nil
}
func (providerRepositoryWithLegacyID) UpdateRequest(context.Context, string, string, any, string, int64, func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return provider.Provider{}, nil
}
func (providerRepositoryWithLegacyID) DeleteRequest(context.Context, string, string, any, string, int64) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func validRequest(method string, payload string) bridge.Request {
	return bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: method, SentAt: time.Now().UTC(), Payload: json.RawMessage(payload), DeadlineMS: 3000,
	}
}

func TestHealthRejectsNullPayload(t *testing.T) {
	response := NewEngine(providerRepositoryStub{}, "test").Handle(context.Background(), validRequest("system.health", "null"))
	if response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestProviderListRejectsUnknownPayloadField(t *testing.T) {
	response := NewEngine(providerRepositoryStub{}, "test").Handle(context.Background(), validRequest("provider.list", `{"unexpected":true}`))
	if response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestRequestRejectsFutureTimestampAndReturnsSchemaValidIDs(t *testing.T) {
	request := validRequest("system.health", `{}`)
	request.SentAt = time.Now().UTC().Add(time.Hour)
	response := NewEngine(providerRepositoryStub{}, "test").Handle(context.Background(), request)
	if response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if _, err := ulid.ParseStrict(response.RequestID); err != nil {
		t.Fatalf("invalid response requestId: %v", err)
	}
	if _, err := ulid.ParseStrict(response.Error.CorrelationID); err != nil {
		t.Fatalf("invalid correlationId: %v", err)
	}
}

func TestInvalidRequestIDUsesGeneratedSchemaValidFallback(t *testing.T) {
	request := validRequest("system.health", `{}`)
	request.ID = "invalid"
	response := NewEngine(providerRepositoryStub{}, "test").Handle(context.Background(), request)
	if _, err := ulid.ParseStrict(response.RequestID); err != nil {
		t.Fatalf("invalid fallback requestId: %v", err)
	}
}

func TestProviderListExcludesLegacyIDFromBridgeJSON(t *testing.T) {
	response := NewEngine(providerRepositoryWithLegacyID{}, "test").Handle(context.Background(), validRequest("provider.list", `{}`))
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !response.OK || bytes.Contains(raw, []byte("legacyId")) || bytes.Contains(raw, []byte("must-not-cross-bridge")) {
		t.Fatalf("internal migration field crossed Bridge: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"status":"enabled"`)) {
		t.Fatalf("required ProviderDTO status missing: %s", raw)
	}
}
