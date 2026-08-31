package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTryClaimExpertTaskIsUniquePerThread(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "claims.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	threadID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	first := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	second := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	owner, created, err := store.TryClaimExpertTask(ctx, threadID, "周报封面", first)
	if err != nil || !created || owner != first {
		t.Fatalf("first claim owner=%q created=%v err=%v", owner, created, err)
	}
	owner, created, err = store.TryClaimExpertTask(ctx, threadID, "周报封面", second)
	if err != nil || created || owner != first {
		t.Fatalf("second claim owner=%q created=%v err=%v", owner, created, err)
	}
}
