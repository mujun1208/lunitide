package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryCaptureModeMigrationPreservesOffAndPersistsChoice(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory-settings.db")
	raw := legacyBeforeMigration(t, path, "0142_")
	if _, err := raw.Exec(`INSERT INTO memory_settings(subject_id,memory_enabled,auto_nominate,growth_days,created_at,updated_at) VALUES('local-user',0,1,30,?,?)`, rfc(rtAt), rfc(rtAt)); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, path)
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
