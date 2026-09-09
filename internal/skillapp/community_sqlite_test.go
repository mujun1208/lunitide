package skillapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestCommunityUpgradePreservesUserVersionsAndUnreviewedDraft(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "community.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := skillapp.New(store, store)
	legacy, err := store.CreateSkill(ctx, skill.Skill{Name: "skill-creator", DisplayName: "My creator", Description: "My existing custom version", Version: "1.0.0", Status: skill.SkillStatusPublished, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: `{"prompt":"Keep my original custom creator","triggers":["custom"]}`})
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := store.CreateSkill(ctx, skill.Skill{Name: "find-skills", DisplayName: "My draft", Description: "Never publish an unreviewed same-name draft", Version: "2.0.0", Status: skill.SkillStatusDraft, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: `{"prompt":"My unfinished draft","triggers":["custom"]}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.EnsureBundledSkills(ctx); err != nil {
		t.Fatal(err)
	}
	old, err := store.GetSkill(ctx, legacy.ID)
	if err != nil || old.ManifestJSON != legacy.ManifestJSON || old.Rev != legacy.Rev || old.Status != legacy.Status {
		t.Fatalf("old version changed: %+v %v", old, err)
	}
	draft, err := store.GetSkill(ctx, conflict.ID)
	if err != nil || draft.Status != skill.SkillStatusDraft || draft.ManifestJSON != conflict.ManifestJSON || draft.Rev != conflict.Rev {
		t.Fatalf("same-name draft overwritten/published: %+v %v", draft, err)
	}
	if _, err = service.EnsureCatalogPublished(ctx, "find-skills"); !errors.Is(err, skillapp.ErrSkillVersionConflict) {
		t.Fatalf("collision must remain explicit: %v", err)
	}
	creator, err := store.GetSkillByNameVersion(ctx, "skill-creator", "2.0.0")
	if err != nil || creator == nil || creator.Status != skill.SkillStatusPublished {
		t.Fatalf("new creator unavailable: %+v %v", creator, err)
	}
	files, err := skillapp.PackageFiles(*creator)
	if err != nil || len(files["upstream/skills/skill-creator/scripts/aggregate_benchmark.py"]) == 0 {
		t.Fatalf("stored skill lost resources: %v", err)
	}
	var manifest map[string]any
	if err = json.Unmarshal([]byte(creator.ManifestJSON), &manifest); err != nil || manifest["bundledPackage"] == nil {
		t.Fatal("source receipt not persisted")
	}
	n, err := service.EnsureBundledSkills(ctx)
	if err != nil || n != 0 {
		t.Fatalf("retry duplicates/re-publishes: %d %v", n, err)
	}
}

func TestCommunityPausedInstallPreservesOldVersionsAndDoesNotReinstall(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "paused.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := skillapp.New(store, store)
	for _, name := range []string{"find-skills", "brainstorming", "frontend-design"} {
		old, err := store.CreateSkill(ctx, skill.Skill{Name: name, DisplayName: "Existing " + name, Description: "Existing user installation", Version: "1.0.0", Status: skill.SkillStatusPublished, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: `{"prompt":"Keep existing content","triggers":["existing"]}`})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.EnsureBundledSkills(ctx); err != nil {
			t.Fatal(err)
		}
		kept, err := store.GetSkill(ctx, old.ID)
		if err != nil || kept.ManifestJSON != old.ManifestJSON || kept.Rev != old.Rev || kept.Status != old.Status {
			t.Fatalf("existing installation changed: %+v %v", kept, err)
		}
		if newer, err := store.GetSkillByNameVersion(ctx, name, "2.0.0"); err != nil || newer != nil {
			t.Fatalf("new community version installed without user choice: %s %+v %v", name, newer, err)
		}
		if err = store.DeleteSkill(ctx, old.ID); err != nil {
			t.Fatal(err)
		}
		if _, err = service.EnsureBundledSkills(ctx); err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"1.0.0", "2.0.0"} {
			if reinstalled, err := store.GetSkillByNameVersion(ctx, name, version); err != nil || reinstalled != nil {
				t.Fatalf("startup reinstalled removed skill %s@%s: %+v %v", name, version, reinstalled, err)
			}
		}
	}
}
