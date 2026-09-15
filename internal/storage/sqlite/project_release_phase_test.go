package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
)

func TestProjectReleasePhaseRequiresActualPublicationForAllProjectTypes(t *testing.T) {
	for _, typ := range []project.Type{project.TypeImplementation, project.TypeEnhancement, project.TypeOperations} {
		t.Run(string(typ), func(t *testing.T) {
			ctx := context.Background()
			store, service, p := phaseTestStore(t, typ)
			phase := project.ReleasePhase(typ)
			for n := 1; n < phase; n++ {
				seedPhaseTestEvidence(t, store, p, n)
				next, err := completeTestPhase(service, p, n)
				if err != nil {
					t.Fatal(err)
				}
				p = next
			}
			seedPhaseTestEvidence(t, store, p, phase)
			root := t.TempDir()
			store.SetProjectPublicationRoot(root)
			if _, err := completeTestPhase(service, p, phase); err == nil {
				t.Fatal("missing publication advanced stage")
			}
			release := m7app.NewReleaseService(store.AgentRuntimeRepository())
			release.SetProjectContent(store)
			revision, err := release.CreateRevision(ctx, "CR-"+p.ProjectCode, map[string]any{"projectId": p.ID, "authorId": "test", "summary": "Actual release"})
			if err != nil {
				t.Fatal(err)
			}
			pkg, err := release.BuildPackage(ctx, revision.ID, revision.Digest)
			if err != nil {
				t.Fatal(err)
			}
			publisher := m7app.NewPromotionService(store.AgentRuntimeRepository())
			publisher.SetLocalPublication(root)
			for _, env := range []string{"dev", "stage"} {
				if _, err = publisher.Promote(ctx, m7app.PromoteInput{PackageID: pkg.ID, TargetEnv: env, RequestID: "publish-" + env, PolicyContext: map[string]any{"requestedBy": "tester"}}); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(root, p.ID, "stage", pkg.BlobDigest, "members", "db_design.txt")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(file, []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = completeTestPhase(service, p, phase); err == nil {
				t.Fatal("tampered publication advanced stage")
			}
			if err = os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			next, err := completeTestPhase(service, p, phase)
			if err != nil {
				t.Fatal(err)
			}
			if next.Status == project.StatusLive {
				t.Fatal("local publication claimed external live deployment")
			}
			var status string
			if err = store.db.QueryRow(`SELECT status FROM stages WHERE project_id=? AND phase=?`, p.ID, phase).Scan(&status); err != nil || status != "completed" {
				t.Fatalf("stage %s %v", status, err)
			}
			replay, err := completeTestPhase(service, p, phase)
			if err != nil || replay.Version != next.Version {
				t.Fatalf("replay %+v %v", replay, err)
			}
			if _, err = service.Mutate(ctx, "unsupported-phase", "test", "project.advanceStatus", p.ID, next.Version, map[string]int{"phase": 9}, func(*project.Project) error { return nil }); err == nil {
				t.Fatalf("unsupported phase9 accepted for %s", typ)
			}
		})
	}
}

func TestProjectReleasePhaseAllowsSyncReceiptAfterRevision(t *testing.T) {
	ctx := context.Background()
	store, service, p := phaseTestStore(t, project.TypeImplementation)
	for n := 1; n < 8; n++ {
		seedPhaseTestEvidence(t, store, p, n)
		next, err := completeTestPhase(service, p, n)
		if err != nil {
			t.Fatal(err)
		}
		p = next
	}
	seedPhaseTestEvidence(t, store, p, 8)
	receiptPath := filepath.Join(p.RootPath, ".lunitide", "sync-receipt.json")
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store.SetProjectPublicationRoot(root)
	release := m7app.NewReleaseService(store.AgentRuntimeRepository())
	release.SetProjectContent(store)
	revision, err := release.CreateRevision(ctx, "CR-"+p.ProjectCode, map[string]any{"projectId": p.ID, "authorId": "test", "summary": "pack then sync"})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := release.BuildPackage(ctx, revision.ID, revision.Digest)
	if err != nil {
		t.Fatal(err)
	}
	publisher := m7app.NewPromotionService(store.AgentRuntimeRepository())
	publisher.SetLocalPublication(root)
	for _, env := range []string{"dev", "stage"} {
		if _, err = publisher.Promote(ctx, m7app.PromoteInput{PackageID: pkg.ID, TargetEnv: env, RequestID: "after-" + env, PolicyContext: map[string]any{"requestedBy": "tester"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(receiptPath, []byte(`{"version":1,"projectId":"`+p.ID+`","sourceRoot":"src","destPath":"dest","at":"2026-09-14T00:00:00Z","files":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = completeTestPhase(service, p, 8); err != nil {
		t.Fatalf("workbench pack then sync then 三关 must complete: %v", err)
	}
}
