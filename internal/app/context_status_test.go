package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/token"
)

type contextStatusTokenRepo struct{ provider, model, revision string }

func (*contextStatusTokenRepo) UpsertTokenLedger(context.Context, token.LedgerEntry) error {
	return nil
}
func (*contextStatusTokenRepo) GetTokenLedger(context.Context, string, string, string, string) (*token.LedgerEntry, error) {
	return nil, nil
}
func (*contextStatusTokenRepo) ListTokenLedgerByMessage(context.Context, string) ([]token.LedgerEntry, error) {
	return nil, nil
}
func (r *contextStatusTokenRepo) SumTokenLedgerBySession(_ context.Context, _, provider, model, revision string) (int64, error) {
	r.provider, r.model, r.revision = provider, model, revision
	return 42, nil
}
func (*contextStatusTokenRepo) DeleteTokenLedgerByMessage(context.Context, string) error { return nil }

func TestContextStatusReportsCanonicalLogicalUsageWithoutRequestAlias(t *testing.T) {
	repo := &contextStatusTokenRepo{}
	e := NewEngine(nil, "test")
	e.tokenRepo = repo
	got, err := e.ContextStatus(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalLogicalTokens != 42 {
		t.Fatalf("canonical tokens = %d", got.CanonicalLogicalTokens)
	}
	if repo.provider != "" || repo.model != "" || repo.revision != token.CanonicalTokenizerRevision {
		t.Fatalf("sum identity = provider %q model %q revision %q", repo.provider, repo.model, repo.revision)
	}
	if got.CanonicalTokenizerID != token.CanonicalTokenizerID || got.CanonicalTokenizerRevision != token.CanonicalTokenizerRevision {
		t.Fatalf("reported tokenizer = %q/%q", got.CanonicalTokenizerID, got.CanonicalTokenizerRevision)
	}
}
