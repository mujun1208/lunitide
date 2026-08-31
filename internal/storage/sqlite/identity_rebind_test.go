package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestRebindLegacyMemorySettingsMergesConflict(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "rebind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	to := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := store.UpsertMemorySettings(ctx, m8core.MemorySettings{
		SubjectID: to, MemoryEnabled: true, AutoNominate: false, GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := store.UpsertMemorySettings(ctx, m8core.MemorySettings{
		SubjectID: "local-user", MemoryEnabled: false, AutoNominate: true, GrowthDays: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RebindLegacySubject(ctx, "local-user", to); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetMemorySettings(ctx, to)
	if err != nil || got.MemoryEnabled || !got.AutoNominate || got.GrowthDays != 30 {
		t.Fatalf("merged = %+v err=%v", got, err)
	}
	legacy, err := store.GetMemorySettings(ctx, "local-user")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.CreatedAt != "" {
		t.Fatalf("legacy row should be gone, got %+v", legacy)
	}
}
