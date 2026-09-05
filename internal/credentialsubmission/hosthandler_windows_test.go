//go:build windows

package credentialsubmission

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
	"path/filepath"
)

type emptyCleanupEngine struct {
	mu     sync.Mutex
	claims int
}

func (e *emptyCleanupEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	if r.Method == "internal.credential-cleanup.claim" {
		e.mu.Lock()
		e.claims++
		e.mu.Unlock()
		return bridge.Success(r.ID, []any{}), nil
	}
	return bridge.Success(r.ID, map[string]any{}), nil
}

type revealEngine struct{ ref secret.Ref }

func (e revealEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	if r.Method == "internal.provider.credential-binding.resolve" {
		return bridge.Success(r.ID, map[string]any{"configured": true, "credentialRef": e.ref.CredentialRef, "providerId": e.ref.ProviderID, "origin": e.ref.Origin, "protocol": e.ref.Protocol}), nil
	}
	return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "not found", false), nil
}

type countingSecrets struct {
	secret.Service
	reads int
}

func (s *countingSecrets) WithSecret(ctx context.Context, ref secret.Ref, fn func([]byte) error) error {
	s.reads++
	return s.Service.WithSecret(ctx, ref, fn)
}

func TestRevealCredentialAllowedUsesAuthoritativeBindingAndSecretService(t *testing.T) {
	c, store, _ := testCoordinator(t)
	ref := secret.Ref{CredentialRef: "ref", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Origin: "https://example.test", Protocol: "openai_compatible"}
	if err := store.Put(context.Background(), ref, []byte("saved-key")); err != nil {
		t.Fatal(err)
	}
	secrets := &countingSecrets{Service: store}
	var confirmedTarget RevealTarget
	h := &HostHandler{Coordinator: c, Engine: revealEngine{ref: ref}, Secrets: secrets, Confirmer: RevealConfirmFunc(func(_ context.Context, target RevealTarget) (bool, error) {
		confirmedTarget = target
		return true, nil
	})}
	payload, _ := json.Marshal(map[string]string{"providerId": ref.ProviderID})
	r := h.HandleHost(context.Background(), bridge.Request{ID: ref.ProviderID, TraceID: ref.ProviderID, Method: "provider.credential.reveal", Payload: payload})
	if !r.OK {
		t.Fatalf("reveal failed: %+v", r.Error)
	}
	b, _ := json.Marshal(r.Payload)
	if string(b) != `{"credential":"saved-key"}` {
		t.Fatalf("unexpected reveal payload: %s", b)
	}
	if secrets.reads != 1 {
		t.Fatalf("secret reads = %d, want 1", secrets.reads)
	}
	if confirmedTarget != (RevealTarget{ProviderID: ref.ProviderID, Protocol: ref.Protocol, Origin: ref.Origin}) {
		t.Fatalf("confirmation target = %#v", confirmedTarget)
	}
}

func TestRevealCredentialDeniedDoesNotReadSecret(t *testing.T) {
	c, store, _ := testCoordinator(t)
	ref := secret.Ref{CredentialRef: "ref", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Origin: "https://example.test", Protocol: "openai_compatible"}
	secrets := &countingSecrets{Service: store}
	h := &HostHandler{Coordinator: c, Engine: revealEngine{ref: ref}, Secrets: secrets, Confirmer: RevealConfirmFunc(func(context.Context, RevealTarget) (bool, error) { return false, nil })}
	payload, _ := json.Marshal(map[string]string{"providerId": ref.ProviderID})
	r := h.HandleHost(context.Background(), bridge.Request{ID: ref.ProviderID, TraceID: ref.ProviderID, Method: "provider.credential.reveal", Payload: payload})
	if r.OK || r.Error == nil || r.Error.Code != "CREDENTIAL_REVEAL_DENIED" || r.Error.Message != "未确认显示已保存的凭据" || r.Error.Retryable {
		t.Fatalf("unexpected denial: %+v", r)
	}
	if secrets.reads != 0 {
		t.Fatalf("denied reveal read secret %d times", secrets.reads)
	}
}

func TestCleanupWorkerReclaimsExpiredSubmissionsDuringLongRun(t *testing.T) {
	c, store, _ := testCoordinator(t)
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	c.now = func() time.Time { return now }
	input := draftInput(t, hash("worker-expiry"), []byte("short-lived"))
	input.TTL = time.Second
	if _, err := c.Submit(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	engine := &emptyCleanupEngine{}
	h := &HostHandler{Coordinator: c, Engine: engine, Secrets: store}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.StartCleanupWorker(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		remaining := len(c.entries)
		c.mu.Unlock()
		engine.mu.Lock()
		claims := engine.claims
		engine.mu.Unlock()
		if remaining == 0 && store.count() == 0 && claims > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic worker did not reclaim and drain: entries=%d secrets=%d claims=%d", remaining, store.count(), claims)
		}
		time.Sleep(time.Millisecond)
	}
}

type mutateSpyEngine struct {
	mu     sync.Mutex
	method string
	body   map[string]any
}

func (e *mutateSpyEngine) Call(_ context.Context, r bridge.Request) (bridge.Response, error) {
	e.mu.Lock()
	e.method = r.Method
	_ = json.Unmarshal(r.Payload, &e.body)
	e.mu.Unlock()
	if r.Method == "internal.provider.update.with-credential" || r.Method == "internal.provider.create.with-credential" {
		return bridge.Failure(r.ID, r.TraceID, "WRONG_BRANCH", "backup.add must not use create/update", false), nil
	}
	return bridge.Success(r.ID, map[string]any{"ok": true}), nil
}

type fixedProviderResolver struct{ item provider.Provider }

func (f fixedProviderResolver) ResolveProvider(context.Context, string) (provider.Provider, error) {
	return f.item, nil
}

func TestMutateBackupAddDoesNotTakeUpdateBranch(t *testing.T) {
	root, err := datadir.PrepareForTest(filepath.Join(t.TempDir(), "secure"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	item := provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://api.example.test", CredentialRef: "primary-ref",
	}
	store := newMemorySecrets()
	c, err := New(root, store, testReferenceResolver{}, fixedProviderResolver{item: item})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	canonical, _ := json.Marshal(map[string]any{"providerId": item.ID, "expectedVersion": 1})
	sub, err := c.Submit(context.Background(), SubmitInput{
		Scope: Existing(item.ID), Protocol: item.Protocol, Origin: "https://api.example.test",
		Request: canonical, Credential: []byte("backup-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	spy := &mutateSpyEngine{}
	h := &HostHandler{Coordinator: c, Engine: spy, Secrets: store}
	payload, _ := json.Marshal(map[string]any{
		"providerId": item.ID, "expectedVersion": 1, "credentialSubmissionId": sub.SubmissionID,
	})
	resp, err := h.Mutate(context.Background(), sub.SubmissionID, canonical, bridge.Request{
		ID: item.ID, TraceID: item.ID, Method: "provider.credential.backup.add", Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("D-F6 mutate failed: %+v", resp.Error)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.method != "internal.provider.backup.with-credential" {
		t.Fatalf("D-F6 method=%s body=%v", spy.method, spy.body)
	}
	if _, hasUpdate := spy.body["update"]; hasUpdate {
		t.Fatal("D-F6 took update branch")
	}
	if _, hasBackup := spy.body["backup"]; !hasBackup {
		t.Fatalf("D-F6 missing backup payload: %v", spy.body)
	}
}
