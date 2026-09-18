package m8app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryGeneration(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "generation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	created, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我住在杭州", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := svc.BuildMemoryGeneration(ctx, "local-user", "user", "", "", ulid.Make().String(), ulid.Make().String())
	if err != nil || ready.GenerationID == "" || ready.State != "ready" {
		t.Fatalf("build %+v err=%v", ready, err)
	}
	preview, changes, _, err := svc.PreviewMemoryGeneration(ctx, "local-user", ready.GenerationID, "", 20)
	if err != nil || preview.GenerationID != ready.GenerationID {
		t.Fatalf("preview %+v err=%v", preview, err)
	}
	_ = changes
	discarded, _, err := svc.DiscardMemoryGeneration(ctx, "local-user", ready.GenerationID, ulid.Make().String(), ulid.Make().String(), 1)
	if err != nil || discarded.State == "active" {
		t.Fatalf("discard must not activate %+v err=%v", discarded, err)
	}
	listed, _, _, err := svc.ListMemoryGenerations(ctx, "local-user", "user", "", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, gen := range listed {
		if gen.GenerationID == ready.GenerationID && gen.State == "active" {
			t.Fatal("discarded generation became active")
		}
	}
	current, err := svc.GetCanonicalItem(ctx, "local-user", created.Item.FactID)
	if err != nil || current.Text != "我住在杭州" {
		t.Fatalf("discard must leave source %+v err=%v", current, err)
	}
}

func TestGenerationCannotRewriteEvidence(t *testing.T) {
	TestMemoryGeneration(t)
}
