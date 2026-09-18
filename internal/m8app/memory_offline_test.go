package m8app_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryOfflineDegradation(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	if _, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我住在杭州", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.HybridRecall(ctx, m8core.HybridRecallQuery{
		SubjectID: "local-user", ScopeKind: "user", Query: "杭州", TopK: 4, BudgetTokens: 400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Degraded != "no_embedding" {
		t.Fatalf("offline dense path must degrade, got %q", res.Degraded)
	}
	if adoptedRecallText(res) == "" || !strings.Contains(adoptedRecallText(res), "杭州") {
		t.Fatalf("FTS/keyword must still work: %+v", res.Hits)
	}
}

func TestMemoryProjection(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "projection.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	created, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢简洁的回答", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	kind, ref, _, _, digest, err := store.CanonicalEvidenceSpan(ctx, created.Item.FactID)
	if err != nil || kind != m8core.MemorySourceDirect || ref == "" || len(digest) != 64 {
		t.Fatalf("evidence span kind=%s ref=%s digest=%s err=%v", kind, ref, digest, err)
	}
}

func TestMemorySourceValidation(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	_, err = svc.CreateCanonicalItem(context.Background(), "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我喜欢简洁的回答", SourceKind: m8core.MemorySourceUserMessage,
		OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err == nil {
		t.Fatal("user_message without span must fail")
	}
}
