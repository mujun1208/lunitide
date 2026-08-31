package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExpertSkillBindingsReplaceAndSeed(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "expert-skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	expertID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	keys, err := store.ListExpertSkillKeys(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("empty bindings = %#v", keys)
	}
	if err := store.SeedExpertSkillsIfEmpty(ctx, expertID, []string{"slide-builder", "web-researcher"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedExpertSkillsIfEmpty(ctx, expertID, []string{"ignored"}); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListExpertSkillKeys(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "slide-builder" || got[1] != "web-researcher" {
		t.Fatalf("seeded = %#v", got)
	}
	if err := store.ReplaceExpertSkillKeys(ctx, expertID, []string{"mermaid-diagrams"}); err != nil {
		t.Fatal(err)
	}
	got, err = store.ListExpertSkillKeys(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "mermaid-diagrams" {
		t.Fatalf("replaced = %#v", got)
	}
}
