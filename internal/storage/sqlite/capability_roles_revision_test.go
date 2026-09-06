package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestCapabilityRolesRevisionSerializesEditorsAndRollsBack(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "roles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	roles := []CapabilityRoleBinding{{Role: "chat"}, {Role: "flash"}, {Role: "vision"}, {Role: "embed"}, {Role: "judge"}, {Role: "gui"}}
	initial := CapabilityRolesRevision(nil)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.CompareAndReplaceCapabilityRoles(ctx, roles, initial)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrCapabilityRevisionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	before, err := store.ListCapabilityRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	revision := CapabilityRolesRevision(before)
	// The second row fails after DELETE and the first INSERT. The old six rows
	// and revision must survive the entire transaction.
	_, err = store.CompareAndReplaceCapabilityRoles(ctx, []CapabilityRoleBinding{{Role: "chat"}, {Role: "invalid"}}, revision)
	if err == nil {
		t.Fatal("invalid role committed")
	}
	after, err := store.ListCapabilityRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if CapabilityRolesRevision(after) != revision || len(after) != 6 {
		t.Fatal("partial settings escaped failed transaction")
	}
	// Saving identical content still changes revision: A -> B -> A is visible.
	replaced, err := store.CompareAndReplaceCapabilityRoles(ctx, roles, revision)
	if err != nil {
		t.Fatal(err)
	}
	if CapabilityRolesRevision(replaced) == revision {
		t.Fatal("ABA edit reused revision")
	}
	if _, err = store.CompareAndReplaceCapabilityRoles(ctx, roles, revision); !errors.Is(err, ErrCapabilityRevisionConflict) {
		t.Fatalf("stale editor: %v", err)
	}
}
