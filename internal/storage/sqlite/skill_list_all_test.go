package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
	sqlite "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// ListSkills used to clamp every request to 100 rows while ordering newest
// first, so a library past 100 silently hid its OLDEST skills — the ones
// installed at first launch. Callers ask this list "is it installed?" and "what
// can be matched?", so a hidden row became a market card frozen on "+" and a
// skill that could never be invoked. Unbounded reads must stay unbounded.
func TestListSkillsReturnsEveryRowPastLegacyPageSize(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "list.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const total = 137
	first := ""
	for i := 0; i < total; i++ {
		created, err := store.CreateSkill(ctx, skill.Skill{
			Name:         fmt.Sprintf("skill-%03d", i),
			DisplayName:  fmt.Sprintf("Skill %03d", i),
			Description:  "row",
			Version:      "1.0.0",
			Status:       skill.SkillStatusPublished,
			Permissions:  []skill.PermissionLevel{skill.PermissionReadOnly},
			EntryPoint:   "SKILL.md",
			ManifestJSON: `{"prompt":"p","triggers":["t"]}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = created.ID
		}
	}

	all, err := store.ListSkills(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != total {
		t.Fatalf("ListSkills(limit 0) = %d rows, want %d", len(all), total)
	}
	// The first-created row is the one a newest-first page drops.
	found := false
	for _, s := range all {
		if s.ID == first {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the earliest-created skill is missing: a newest-first page is still hiding rows")
	}

	// An explicit page is still honoured for callers that genuinely want one.
	page, err := store.ListSkills(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 10 {
		t.Fatalf("ListSkills(limit 10) = %d rows, want 10", len(page))
	}
}
