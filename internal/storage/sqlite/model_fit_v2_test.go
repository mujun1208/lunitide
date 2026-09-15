package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/migrations"
)

func TestModelNativeV2MigrationLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0157_model_native_v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0157_model_native_v2.sql must be LF")
	}
}

func TestModelNativeV2TablesOnFreshOpen(t *testing.T) {
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "model-native-v2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, table := range []string{"model_profiles_v2", "model_targets_v2"} {
		var name string
		if err = store.db.QueryRow(`SELECT name FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("fresh OpenTemplated missing %s: %v", table, err)
		}
	}
}
