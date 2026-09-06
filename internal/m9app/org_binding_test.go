package m9app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOrgBindingAtomicReplaceAndFailClosedInvalidData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "binding.json")
	b := NewFileBindingStore(path)
	for _, id := range []string{"01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW", ""} {
		if err := b.Save(ctx, id); err != nil {
			t.Fatal(err)
		}
		got, err := b.Load(ctx)
		if err != nil || got != id {
			t.Fatalf("got %q %v", got, err)
		}
	}
	for _, invalid := range []string{`{}`, `null`, `{"orgId":null}`, `{"orgId":"","extra":true}`, `{"orgId":""} trailing`, `{"orgId":`} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Load(ctx); err == nil {
			t.Fatalf("invalid binding accepted %q", invalid)
		}
	}
}
