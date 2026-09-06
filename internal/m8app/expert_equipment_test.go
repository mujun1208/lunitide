package m8app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestExpertEquipmentSnapshotsSurviveEditsAndSeed(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	created, err := svc.Create(ctx, m8app.CreateInput{Source: m8core.ExpertSourceLocal, Frontmatter: fm("snapshot-fixture"), SixSection: sixBody("snapshot"), RequestID: "fixture", SkillKeys: []string{"docx-writer", "mcp:fetch", "brain:codex"}})
	if err != nil {
		t.Fatal(err)
	}
	original, err := svc.Equipment(ctx, created.ExpertID, created.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := svc.ReplaceBoundSkillsVersioned(ctx, created.ExpertID, created.VersionID, []string{"slide-builder"})
	if err != nil || changed.VersionID == created.VersionID {
		t.Fatalf("update %+v %v", changed, err)
	}
	if _, err = svc.ReplaceBoundSkillsVersioned(ctx, created.ExpertID, created.VersionID, []string{"web-researcher"}); !errors.Is(err, m8app.ErrExpertVersionConflict) {
		t.Fatalf("stale overwrite: %v", err)
	}
	historical, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID, VersionID: created.VersionID})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(historical.Expert["boundSkills"], original.SkillKeys) || historical.Expert["boundSkillsKnown"] != true {
		t.Fatalf("historical equipment %+v", historical.Expert)
	}
	if err = store.MergeExpertSkillKeys(ctx, created.ExpertID, []string{"web-researcher"}); err != nil {
		t.Fatal(err)
	}
	current, err := svc.Equipment(ctx, created.ExpertID, "")
	if err != nil || current.VersionID == changed.VersionID {
		t.Fatalf("seed bypassed version %+v %v", current, err)
	}
	old, err := svc.Equipment(ctx, created.ExpertID, changed.VersionID)
	if err != nil || !reflect.DeepEqual(old.SkillKeys, []string{"slide-builder"}) {
		t.Fatalf("seed changed historical snapshot %+v %v", old, err)
	}
	foreign := createExpert(t, svc, "foreign-equipment")
	if _, err = svc.Equipment(ctx, foreign.ExpertID, created.VersionID); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatal("foreign version exposed")
	}
}
func TestExpertPersonaGCProtectsReferencesAndRecoversOrphans(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	root := t.TempDir()
	bodies := m8app.NewFilePersonaStore(root)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", bodies)
	created := createExpert(t, svc, "gc-retained")
	detail, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil {
		t.Fatal(err)
	}
	_ = detail
	used := fm("gc-retained").PersonaRef(sixBody("gc-retained"))
	old := time.Now().Add(-48 * time.Hour)
	// The body written inside a rolled-back equipment transaction is orphaned.
	failing := m8app.NewExpertService(failingEquipmentUOW{base: store.AgentRuntimeRepository()}, "local-user", bodies)
	orphanBody := sixBody("gc-orphan")
	_, err = failing.Create(ctx, m8app.CreateInput{Source: m8core.ExpertSourceLocal, Frontmatter: fm("gc-orphan"), SixSection: orphanBody, RequestID: "orphan", SkillKeys: []string{"docx-writer"}})
	if err == nil {
		t.Fatal("injected equipment failure succeeded")
	}
	orphan := fm("gc-orphan").PersonaRef(orphanBody)
	for _, ref := range []string{used, orphan} {
		path := filepath.Join(root, ref[:2], ref+".json")
		if err = os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := svc.CollectOrphanBodies(ctx, time.Now().Add(-24*time.Hour), 256)
	if err != nil || removed != 1 {
		t.Fatalf("GC removed=%d %v", removed, err)
	}
	if _, ok, err := bodies.Get(used); err != nil || !ok {
		t.Fatalf("referenced body removed: %v", err)
	}
	if _, ok, err := bodies.Get(orphan); err != nil || ok {
		t.Fatalf("orphan not collected %v", err)
	}
	if _, err = svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID}); err != nil {
		t.Fatal(err)
	}
}
