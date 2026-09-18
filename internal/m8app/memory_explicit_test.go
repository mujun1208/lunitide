package m8app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestSaveExplicitMemoryConfirmsWithoutPendingBanner(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "explicit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	if _, err := svc.SaveExplicitMemory(ctx, "local-user", "你好"); !errors.Is(err, m8app.ErrMemorySourceInvalid) {
		t.Fatalf("greeting: %v", err)
	}
	got, err := svc.SaveExplicitMemory(ctx, "local-user", "我喜欢简洁的回答")
	if err != nil || got.State != "confirmed" {
		t.Fatalf("explicit save %+v err=%v", got, err)
	}
	pending, err := svc.ListPendingCandidates(ctx, 20)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after explicit save: %v err=%v", pending, err)
	}
	prefs, err := svc.PersonalPreferenceSnapshot(ctx, "local-user", m8app.LearningScope, 8, 4096)
	if err != nil || len(prefs) != 1 || prefs[0] != "我喜欢简洁的回答" {
		t.Fatalf("prefs=%v err=%v", prefs, err)
	}
	facts, err := store.ListCanonicalMemoryItems(ctx, "local-user", "user", "local-user", 8)
	if err != nil || len(facts) != 1 || facts[0] != "我喜欢简洁的回答" {
		t.Fatalf("canonical=%v err=%v", facts, err)
	}
}

func TestConfirmCandidateMirrorsCanonical(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "confirm-canonical.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	prop, err := svc.ProposeCandidate(ctx, m8app.ProposeInput{
		SubjectID: "local-user",
		Doc: m8core.PayloadDoc{
			Content: "回答默认使用中文", ScopeID: m8app.LearningScope, Sensitivity: "private",
			Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://run-1/evidence-a", Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		},
		Trust: "untrusted", Actor: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmCandidateFor(ctx, "local-user", m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken, Action: "confirm", RequestID: "confirm-canonical",
	}); err != nil {
		t.Fatal(err)
	}
	facts, err := store.ListCanonicalMemoryItems(ctx, "local-user", "user", "local-user", 8)
	if err != nil || len(facts) != 1 || facts[0] != "回答默认使用中文" {
		t.Fatalf("canonical=%v err=%v", facts, err)
	}
}

func TestCreateCanonicalItemIdempotentAndMismatch(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "canonical-create.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	first, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢简洁的回答", OperationID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", IdempotencyKey: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	})
	if err != nil || first.Item.FactID == "" || first.Item.Text != "我喜欢简洁的回答" {
		t.Fatalf("create %+v err=%v", first, err)
	}
	replay, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢简洁的回答", OperationID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", IdempotencyKey: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	})
	if err != nil || replay.Item.FactID != first.Item.FactID {
		t.Fatalf("replay %+v err=%v", replay, err)
	}
	_, err = svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "回答默认使用中文", OperationID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", IdempotencyKey: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
	})
	if !errors.Is(err, m8core.ErrOperationReplayMismatch) {
		t.Fatalf("mismatch: %v", err)
	}
}
