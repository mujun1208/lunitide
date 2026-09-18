package m8app_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestMemoryLifecycleCorrection(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "lifecycle-correct.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	created, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "我住在上海", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	corrected, rev, err := svc.CorrectCanonicalItem(ctx, "local-user", created.Item.FactID, "我住在杭州", "迁居", ulid.Make().String(), ulid.Make().String(), created.Item.Revision, nil)
	if err != nil || corrected.Text != "我住在杭州" || rev < created.DatabaseRevision {
		t.Fatalf("correct %+v rev=%d err=%v", corrected, rev, err)
	}
	current, err := svc.GetCanonicalItem(ctx, "local-user", created.Item.FactID)
	if err != nil || current.Text != "我住在杭州" || current.Forgotten {
		t.Fatalf("current %+v err=%v", current, err)
	}
	history, _, err := svc.HistoryCanonicalItem(ctx, "local-user", created.Item.FactID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var sawShanghai, sawHangzhou bool
	for _, row := range history {
		if strings.Contains(row.Text, "上海") {
			sawShanghai = true
		}
		if strings.Contains(row.Text, "杭州") {
			sawHangzhou = true
		}
	}
	if !sawShanghai || !sawHangzhou {
		t.Fatalf("history must keep both cities: %+v", history)
	}
	listed, _, err := svc.ListCanonicalItems(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(listed) != 1 || listed[0].Text != "我住在杭州" {
		t.Fatalf("list current %+v err=%v", listed, err)
	}
}

func TestMemoryForgetAndPurge(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "lifecycle-forget.db"))
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
	if err := svc.ForgetCanonicalItem(ctx, "local-user", created.Item.FactID, ulid.Make().String(), created.Item.Revision); err != nil {
		t.Fatal(err)
	}
	listed, _, err := svc.ListCanonicalItems(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(listed) != 0 {
		t.Fatalf("forgotten still listed %+v err=%v", listed, err)
	}
	forgotten, err := svc.GetCanonicalItem(ctx, "local-user", created.Item.FactID)
	if err != nil || !forgotten.Forgotten || forgotten.Text != "" {
		t.Fatalf("forgotten body leaked %+v err=%v", forgotten, err)
	}

	second, err := svc.CreateCanonicalItem(ctx, "local-user", m8app.CreateCanonicalItemInput{
		ScopeKind: "user", Text: "回答默认使用中文", OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := svc.PrepareMemoryPurge(ctx, "local-user", "user", "", ulid.Make().String(), ulid.Make().String(), second.DatabaseRevision)
	if err != nil || grant.ConfirmationToken == "" || grant.Counts.Facts < 1 {
		t.Fatalf("prepare %+v err=%v", grant, err)
	}
	if _, _, err := svc.ConsumeMemoryPurge(ctx, "local-user", grant.ConfirmationToken, grant.SnapshotDigest, ulid.Make().String(), ulid.Make().String(), grant.DatabaseRevision); err != nil {
		t.Fatal(err)
	}
	after, _, err := svc.ListCanonicalItems(ctx, "local-user", "user", "local-user", "", 20)
	if err != nil || len(after) != 0 {
		t.Fatalf("purged list %+v err=%v", after, err)
	}
	purged, err := svc.GetCanonicalItem(ctx, "local-user", second.Item.FactID)
	if err != nil || !purged.Forgotten || purged.Text != "" {
		t.Fatalf("purged body leaked %+v err=%v", purged, err)
	}
}
