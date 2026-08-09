//go:build windows && integration

package credentialsubmission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/electronsafestorage"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type integrationEngineCaller struct{ engine *app.Engine }

func (c integrationEngineCaller) Call(ctx context.Context, request bridge.Request) (bridge.Response, error) {
	return c.engine.Handle(ctx, request), nil
}

func TestElectronSafeStorageAdoptionE2E(t *testing.T) {
	corpus := os.Getenv("LUNITIDE_ELECTRON_CORPUS")
	canary := os.Getenv("LUNITIDE_ELECTRON_CORPUS_CANARY")
	if corpus == "" || canary == "" {
		t.Fatal("authentic Electron corpus environment is not configured; run npm run test:electron-adoption:e2e")
	}
	before, err := os.ReadFile(corpus)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := sha256.Sum256(before)
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "migration.db")
	store, err := storage.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if status, err := store.RunElectronProviderMetadata(ctx, corpus); err != nil || status.Imported != 1 {
		t.Fatalf("metadata migration status=%#v err=%v", status, err)
	}
	var sourceCandidates []storage.ElectronCredentialCandidate
	if err := storage.VisitElectronCredentialFileForIntegration(ctx, corpus, func(candidate storage.ElectronCredentialCandidate) error {
		sourceCandidates = append(sourceCandidates, candidate)
		return nil
	}); err != nil || len(sourceCandidates) != 1 {
		t.Fatalf("credential candidates=%d err=%v", len(sourceCandidates), err)
	}
	if err := electronsafestorage.WithAPIKeyAndEncryptedKey(sourceCandidates[0].EncryptedBlob, sourceCandidates[0].OSCryptEncryptedKey, sourceCandidates[0].Origin, sourceCandidates[0].LegacyProtocol, func(value []byte) error {
		if !bytes.Equal(value, []byte(canary)) {
			t.Fatal("Electron decrypt returned the wrong credential")
		}
		return nil
	}); err != nil {
		t.Fatalf("authentic Electron safeStorage decrypt failed: %v", err)
	}
	providers := providerapp.New(store, store)
	engine := app.NewEngine(providers, "integration")
	caller := integrationEngineCaller{engine: engine}
	root, err := datadir.PrepareForTest(filepath.Join(t.TempDir(), "secure"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	secrets, err := secret.NewDPAPIService(root)
	if err != nil {
		t.Fatal(err)
	}
	resolver := RPCResolver{Engine: caller}
	coordinator, err := New(root, secrets, resolver, resolver)
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	handler := &HostHandler{Coordinator: coordinator, Engine: caller, Secrets: secrets}
	discover := func(ctx context.Context, visit func(storage.ElectronCredentialCandidate) error) error {
		return storage.VisitElectronCredentialFileForIntegration(ctx, corpus, visit)
	}
	if err := handler.RunElectronCredentialAdoptionForIntegration(ctx, discover); err != nil {
		t.Fatal(err)
	}
	items, err := providers.List(ctx, provider.Filter{})
	if err != nil || len(items) != 1 {
		t.Fatalf("providers=%d err=%v", len(items), err)
	}
	p := items[0]
	if p.CredentialState != provider.CredentialConfigured || p.CredentialRef == "" || p.Protocol != provider.ProtocolOpenAICompatible {
		t.Fatalf("credential was not adopted: %#v", p)
	}
	ref := secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ID, Origin: "https://example.test", Protocol: string(p.Protocol)}
	if err := secrets.WithSecret(ctx, ref, func(value []byte) error {
		if !bytes.Equal(value, []byte(canary)) {
			t.Fatalf("unexpected adopted credential")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := handler.RunElectronCredentialAdoptionForIntegration(ctx, discover); err != nil {
		t.Fatal(err)
	}
	replayed, err := providers.Get(ctx, p.ID)
	if err != nil || replayed.CredentialRef != p.CredentialRef || replayed.Version != p.Version {
		t.Fatalf("restart replay changed provider: %#v err=%v", replayed, err)
	}
	after, err := os.ReadFile(corpus)
	if err != nil || sha256.Sum256(after) != beforeHash {
		t.Fatal("source Electron corpus was modified")
	}
	database, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(database, []byte(canary)) || bytes.Contains(database, before) {
		t.Fatal("credential material leaked into SQLite")
	}
}
