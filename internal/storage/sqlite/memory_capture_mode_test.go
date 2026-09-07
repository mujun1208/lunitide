package sqlite

import (
	"context"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"path/filepath"
	"testing"
)

func TestMemoryCaptureModeMigrationPreservesOffAndPersistsChoice(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory-settings.db")
	store, err := OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertMemorySettings(ctx, m8core.MemorySettings{SubjectID: "local-user", MemoryEnabled: false, AutoNominate: true, GrowthDays: 30}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	raw := openRaw(t, path)
	if _, err = raw.Exec(`ALTER TABLE memory_settings DROP COLUMN capture_mode`); err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`DELETE FROM schema_migrations WHERE version='0142_memory_capture_mode.sql'`); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.GetMemorySettings(ctx, "local-user")
	if err != nil || st.MemoryEnabled || !st.AutoNominate || st.GrowthDays != 30 || st.CaptureMode != "auto" {
		t.Fatalf("migration %v %v", st, err)
	}
	st.CaptureMode = "manual"
	if err = store.UpsertMemorySettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	// An older client editing unrelated options must preserve the saved choice.
	st.CaptureMode = ""
	st.GrowthDays = 20
	if err = store.UpsertMemorySettings(ctx, st); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	st, err = store.GetMemorySettings(ctx, "local-user")
	if err != nil || st.CaptureMode != "manual" || st.GrowthDays != 20 {
		t.Fatalf("restart %v %v", st, err)
	}
}
