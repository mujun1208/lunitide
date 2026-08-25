package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestProviderBridgeCRUDIdempotencyAndCAS(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "provider-bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := NewEngine(providerapp.New(store, store), "test")

	createPayload := `{"name":"Example","protocol":"openai_compatible","baseUrl":"HTTPS://EXAMPLE.COM:443/v1/","models":[{"modelId":"model-1","displayName":"Model One","isDefault":true,"contextWindow":128000}]}`
	create := validRequest("provider.create", createPayload)
	create.IdempotencyKey = "create-1"
	createdResponse := engine.Handle(context.Background(), create)
	created := decodeProviderPayload(t, createdResponse)
	if !createdResponse.OK || created.ID == "" || created.BaseURL != "https://example.com/v1" || created.Status != provider.StatusEnabled || created.CredentialState != provider.CredentialMissing || created.Version != 1 || created.Models[0].ContextWindow != 128000 {
		t.Fatalf("unexpected create: %#v %#v", createdResponse, created)
	}

	replay := validRequest("provider.create", createPayload)
	replay.IdempotencyKey = create.IdempotencyKey
	replayed := decodeProviderPayload(t, engine.Handle(context.Background(), replay))
	if replayed.ID != created.ID || replayed.Version != created.Version {
		t.Fatalf("create replay changed result: %#v != %#v", replayed, created)
	}

	get := validRequest("provider.get", `{"id":"`+created.ID+`"}`)
	got := decodeProviderPayload(t, engine.Handle(context.Background(), get))
	if got.ID != created.ID || got.Name != "Example" {
		t.Fatalf("unexpected get: %#v", got)
	}

	updatePayload := `{"id":"` + created.ID + `","name":"Renamed","models":[{"modelId":"model-1","displayName":"Model One","isDefault":true,"contextWindow":200000}],"expectedVersion":1}`
	update := validRequest("provider.update", updatePayload)
	update.IdempotencyKey = "update-1"
	updated := decodeProviderPayload(t, engine.Handle(context.Background(), update))
	if updated.Name != "Renamed" || updated.Version != 2 || len(updated.Models) != 1 || updated.Models[0].ContextWindow != 200000 {
		t.Fatalf("unexpected patch update: %#v", updated)
	}

	updateReplay := validRequest("provider.update", updatePayload)
	updateReplay.IdempotencyKey = update.IdempotencyKey
	replayedUpdate := decodeProviderPayload(t, engine.Handle(context.Background(), updateReplay))
	if replayedUpdate.ID != updated.ID || replayedUpdate.Version != 2 || replayedUpdate.Name != "Renamed" {
		t.Fatalf("update replay changed result: %#v", replayedUpdate)
	}

	stale := validRequest("provider.update", `{"id":"`+created.ID+`","name":"Stale","expectedVersion":1}`)
	stale.IdempotencyKey = "update-stale"
	staleResponse := engine.Handle(context.Background(), stale)
	if staleResponse.OK || staleResponse.Error == nil || staleResponse.Error.Code != "PROVIDER_VERSION_CONFLICT" {
		t.Fatalf("unexpected stale update response: %#v", staleResponse)
	}

	remove := validRequest("provider.delete", `{"id":"`+created.ID+`","expectedVersion":2}`)
	remove.IdempotencyKey = "delete-1"
	removed := engine.Handle(context.Background(), remove)
	if !removed.OK {
		t.Fatalf("delete failed: %#v", removed)
	}
	removeReplay := validRequest("provider.delete", `{"id":"`+created.ID+`","expectedVersion":2}`)
	removeReplay.IdempotencyKey = remove.IdempotencyKey
	if response := engine.Handle(context.Background(), removeReplay); !response.OK {
		t.Fatalf("delete replay failed: %#v", response)
	}
	if response := engine.Handle(context.Background(), get); response.OK || response.Error == nil || response.Error.Code != "PROVIDER_NOT_FOUND" {
		t.Fatalf("deleted provider remained visible: %#v", response)
	}
	updateAfterDeleteReplay := validRequest("provider.update", updatePayload)
	updateAfterDeleteReplay.IdempotencyKey = update.IdempotencyKey
	if replayedAfterDelete := decodeProviderPayload(t, engine.Handle(context.Background(), updateAfterDeleteReplay)); replayedAfterDelete.ID != updated.ID || replayedAfterDelete.Version != 2 {
		t.Fatalf("update replay after delete changed result: %#v", replayedAfterDelete)
	}
}

func TestProviderBridgeWriteBoundaryFailures(t *testing.T) {
	engine := NewEngine(providerRepositoryStub{}, "test")
	request := validRequest("provider.create", `{"name":"Example","protocol":"openai_compatible","baseUrl":"https://example.com","models":[{"modelId":"m","displayName":"M","isDefault":true}]}`)
	if response := engine.Handle(context.Background(), request); response.OK || response.Error == nil || response.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("missing idempotency key accepted: %#v", response)
	}
	request.IdempotencyKey = "create-secret"
	request.Payload = json.RawMessage(`{"name":"Example","protocol":"openai_compatible","baseUrl":"https://example.com","models":[{"modelId":"m","displayName":"M","isDefault":true}],"credentialSubmissionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`)
	if response := engine.Handle(context.Background(), request); response.OK || response.Error == nil || response.Error.Code != "CREDENTIAL_SUBMISSION_NOT_ENABLED" {
		t.Fatalf("credential token was silently accepted: %#v", response)
	}
	for name, payload := range map[string]string{
		"explicit empty status":           `{"name":"Example","protocol":"openai_compatible","baseUrl":"https://example.com","models":[{"modelId":"m","displayName":"M","isDefault":true}],"status":""}`,
		"explicit empty credential token": `{"name":"Example","protocol":"openai_compatible","baseUrl":"https://example.com","models":[{"modelId":"m","displayName":"M","isDefault":true}],"credentialSubmissionId":""}`,
		"missing model default":           `{"name":"Example","protocol":"openai_compatible","baseUrl":"https://example.com","models":[{"modelId":"m","displayName":"M"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			invalid := validRequest("provider.create", payload)
			invalid.IdempotencyKey = "invalid-" + name
			if response := engine.Handle(context.Background(), invalid); response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
				t.Fatalf("schema-invalid payload accepted: %#v", response)
			}
		})
	}
}

func decodeProviderPayload(t *testing.T, response interface{}) providerDTO {
	t.Helper()
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK      bool        `json:"ok"`
		Payload providerDTO `json:"payload"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK {
		t.Fatalf("expected successful provider response: %s", raw)
	}
	return envelope.Payload
}
