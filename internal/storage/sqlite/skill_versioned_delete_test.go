package sqlite

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

func TestDeleteSkillVersionAllowsPublished(t *testing.T) {
	store := newSkillCASStore(t)
	sk := seedSkillForCAS(t, store)
	ctx := context.Background()
	if err := store.UpdateSkillStatus(ctx, sk.ID, string(skill.SkillStatusPublished), sk.Rev); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSkill(ctx, sk.ID)
	if err != nil || got == nil {
		t.Fatalf("published skill missing: %v", err)
	}
	if err := store.DeleteSkillVersion(ctx, got.ID, got.Rev); err != nil {
		t.Fatalf("published delete: %v", err)
	}
	after, err := store.GetSkill(ctx, sk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after != nil {
		t.Fatalf("published skill still present: %+v", after)
	}
}
