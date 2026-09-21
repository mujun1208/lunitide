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

// The two catalog entry points differ on purpose. Startup may never delete: the
// version the user is running has to survive an app launch. An explicit market
// click must delete: two rows for one skill leave the card unable to reach
// "installed", so every later click on it reads as a dead button.
func TestExplicitInstallRetiresOlderRowWhileStartupKeepsIt(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "replace.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := skillapp.New(store, store)
	// "meeting-minutes" and "tpl-meeting-minutes" are one skill under the two
	// naming conventions this product ships (community bundle vs built-in
	// catalog); treating them as two is what allowed duplicate rows.
	older := skill.Skill{Name: "meeting-minutes", DisplayName: "Old minutes", Description: "Older row from a previous build", Version: "0.9.0", Status: skill.SkillStatusPublished, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: `{"prompt":"old","triggers":["minutes"]}`}
	old, err := store.CreateSkill(ctx, older)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.InstallFromCatalog(ctx, "meeting-minutes"); err != nil {
		t.Fatal(err)
	}
	if kept, err := store.GetSkill(ctx, old.ID); err != nil || kept == nil {
		t.Fatalf("startup install deleted the running version: %+v %v", kept, err)
	}
	if err = store.DeleteSkill(ctx, old.ID); err != nil {
		t.Fatal(err)
	}
	old, err = store.CreateSkill(ctx, older)
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := service.ReplaceFromCatalog(ctx, "meeting-minutes")
	if err != nil {
		t.Fatal(err)
	}
	if gone, err := store.GetSkill(ctx, old.ID); err == nil && gone != nil {
		t.Fatalf("explicit install left a second row for one skill: %+v", gone)
	}
	if replaced.ID == old.ID {
		t.Fatalf("explicit install did not write the new version: %+v", replaced)
	}
	// Installing the version already superseded must say so, not silently no-op.
	if err = store.DeleteSkill(ctx, replaced.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateSkill(ctx, skill.Skill{Name: "meeting-minutes", DisplayName: "Newer", Description: "Newer than the catalog", Version: "99.0.0", Status: skill.SkillStatusPublished, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: `{"prompt":"newer","triggers":["minutes"]}`}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReplaceFromCatalog(ctx, "meeting-minutes"); !errors.Is(err, skillapp.ErrTemplateSuperseded) {
		t.Fatalf("stale template install = %v, want ErrTemplateSuperseded", err)
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
