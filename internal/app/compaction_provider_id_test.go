package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
)

const compactionProviderID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

type compactionProviderService struct{ providerRepositoryStub }

func (compactionProviderService) Get(context.Context, string) (provider.Provider, error) {
	return compactionTestProvider(), nil
}

func (compactionProviderService) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return []provider.Provider{compactionTestProvider()}, nil
}

func compactionTestProvider() provider.Provider {
	return provider.Provider{
		ID: compactionProviderID, Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com", CredentialRef: "credential-ref",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{{ModelID: "model", ContextWindow: 128000}},
	}
}

type compactionCheckpointStore struct{ latest *compaction.Checkpoint }

func (*compactionCheckpointStore) CreateCheckpoint(_ context.Context, cp compaction.Checkpoint) (compaction.Checkpoint, error) {
	return cp, nil
}
func (s *compactionCheckpointStore) GetLatestCheckpoint(context.Context, string) (*compaction.Checkpoint, error) {
	return s.latest, nil
}
func (*compactionCheckpointStore) CountCheckpointsBySession(context.Context, string) (int64, error) {
	return 0, nil
}
func (*compactionCheckpointStore) ListCheckpointsByStatus(context.Context, compaction.Status, int) ([]compaction.Checkpoint, error) {
	return nil, nil
}

type compactionMessageReader struct{}

func (compactionMessageReader) ListMessages(context.Context, string, string, int64, int64, int) ([]compactionapp.MessageInfo, int64, bool, error) {
	return nil, 0, false, nil
}

type compactionTokenLookup struct{ provider string }

func (*compactionTokenLookup) UpsertTokenLedger(context.Context, token.LedgerEntry) error { return nil }
func (*compactionTokenLookup) GetTokenLedger(context.Context, string, string, string, string) (*token.LedgerEntry, error) {
	return nil, nil
}
func (*compactionTokenLookup) ListTokenLedgerByMessage(context.Context, string) ([]token.LedgerEntry, error) {
	return nil, nil
}
func (s *compactionTokenLookup) SumTokenLedgerBySession(_ context.Context, _, providerID, _, _ string) (int64, error) {
	s.provider = providerID
	return 0, nil
}
func (*compactionTokenLookup) DeleteTokenLedgerByMessage(context.Context, string) error { return nil }

func TestChatPreTurnCompactionUsesProviderIDForTokenLookup(t *testing.T) {
	tokens := &compactionTokenLookup{}
	e := NewEngineWithContextReader(compactionProviderService{}, nil, nil, nil, chatAttachmentReader{}, tokens, "test", streamTestLease{})
	e.compactionTrigger = compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), tokens, &compactionCheckpointStore{}, compactionMessageReader{})
	e.compactionExecutor = &compactionapp.Executor{}

	e.HandleStreaming(context.Background(), validRequest("chat.start", `{"providerId":"`+compactionProviderID+`","modelId":"model","sessionId":"`+chatAttachmentSessionID+`","messages":[{"role":"user","content":"hello"}]}`), func(bridge.Event) error { return nil })
	if tokens.provider != compactionProviderID {
		t.Fatalf("pre-turn token lookup provider = %q, want real provider ID %q", tokens.provider, compactionProviderID)
	}
}

func TestDeriveCompactionContextMatchesCheckpointByProviderID(t *testing.T) {
	checkpointStore := &compactionCheckpointStore{latest: &compaction.Checkpoint{Provider: compactionProviderID, Model: "model"}}
	e := NewEngine(compactionProviderService{}, "test")
	e.compactionTrigger = compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), &compactionTokenLookup{}, checkpointStore, compactionMessageReader{})

	providerID, modelID, contextWindow := deriveCompactionContext(e, context.Background(), "session")
	if providerID != compactionProviderID || modelID != "model" || contextWindow != 128000 {
		t.Fatalf("derived context = (%q, %q, %d), want (%q, model, 128000)", providerID, modelID, contextWindow, compactionProviderID)
	}
}

func TestDeriveCompactionContextFallbackUsesProviderID(t *testing.T) {
	e := NewEngine(compactionProviderService{}, "test")

	providerID, modelID, contextWindow := deriveCompactionContext(e, context.Background(), "session")
	if providerID != compactionProviderID || modelID != "model" || contextWindow != 128000 {
		t.Fatalf("fallback context = (%q, %q, %d), want (%q, model, 128000)", providerID, modelID, contextWindow, compactionProviderID)
	}
}

func TestDeriveCompactionContextResolvesHistoricalProtocol(t *testing.T) {
	checkpointStore := &compactionCheckpointStore{latest: &compaction.Checkpoint{Provider: string(provider.ProtocolOpenAICompatible), Model: "model"}}
	e := NewEngine(compactionProviderService{}, "test")
	e.compactionTrigger = compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), &compactionTokenLookup{}, checkpointStore, compactionMessageReader{})

	providerID, modelID, contextWindow := deriveCompactionContext(e, context.Background(), "session")
	if providerID != compactionProviderID || modelID != "model" || contextWindow != 128000 {
		t.Fatalf("historical protocol context = (%q, %q, %d)", providerID, modelID, contextWindow)
	}
}

type legacyCompactionProviderService struct{ providerRepositoryStub }

func (legacyCompactionProviderService) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	p := compactionTestProvider()
	p.LegacyID = "legacy-provider"
	return []provider.Provider{p}, nil
}

func TestDeriveCompactionContextResolvesLegacyProviderID(t *testing.T) {
	checkpointStore := &compactionCheckpointStore{latest: &compaction.Checkpoint{Provider: "legacy-provider", Model: "model"}}
	e := NewEngine(legacyCompactionProviderService{}, "test")
	e.compactionTrigger = compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), &compactionTokenLookup{}, checkpointStore, compactionMessageReader{})

	providerID, modelID, contextWindow := deriveCompactionContext(e, context.Background(), "session")
	if providerID != compactionProviderID || modelID != "model" || contextWindow != 128000 {
		t.Fatalf("legacy provider context = (%q, %q, %d)", providerID, modelID, contextWindow)
	}
}
