package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/oklog/ulid/v2"
)

func TestTemplateMaintenanceReclaimsOnlyOldUnreferencedOwnedFiles(t *testing.T) {
	ctx := context.Background()
	e, _, store := agentRunEngine(t)
	e.SetAssetStorage(store)
	root, stage := t.TempDir(), t.TempDir()
	files := attachmentapp.NewDirFileStorage(root)
	e.SetTemplateFileStorage(files)
	e.SetTemplateStageDirectory(stage)
	now := time.Now()
	old := now.Add(-25 * time.Hour)
	orphan, used, recent := ulid.Make().String(), ulid.Make().String(), ulid.Make().String()
	for _, name := range []string{orphan, used, recent, "operator-note.txt", ".lunitide-ws-1234"} {
		if err := files.WriteFile(ctx, name, []byte(name)); err != nil {
			t.Fatal(err)
		}
		if name != recent {
			if err := os.Chtimes(filepath.Join(root, name), old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := store.CreateAssetTemplate(ctx, asset.AssetTemplate{Name: "Retained", TemplateType: asset.TemplateTypeDocument, FilePath: used}); err != nil {
		t.Fatal(err)
	}
	abandoned := "up-" + ulid.Make().String() + "-1234"
	if err := os.WriteFile(filepath.Join(stage, abandoned), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(stage, abandoned), old, old); err != nil {
		t.Fatal(err)
	}
	active := ulid.Make().String()
	if out := assetUpgradeStage(t, e, active, 0, true, "active"); !out.OK {
		t.Fatal(out.Error)
	}
	activePath := e.templateStage().uploads[active].path
	if err := os.Chtimes(activePath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := e.ReconcileTemplateFiles(ctx, now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{orphan, ".lunitide-ws-1234"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("orphan remained %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stage, abandoned)); !os.IsNotExist(err) {
		t.Fatal("abandoned upload remained")
	}
	for _, name := range []string{used, recent, "operator-note.txt"} {
		if _, err := files.ReadFile(ctx, name); err != nil {
			t.Fatalf("retained %s: %v", name, err)
		}
	}
	if _, err := e.consumeTemplateStage(active); err != nil {
		t.Fatal("active upload removed", err)
	}
	stop := e.StartTemplateMaintenance(ctx)
	stop()
	stop()
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Fatal("shutdown retained active upload")
	}
	if err := e.ReconcileTemplateFiles(ctx, now); err != nil {
		t.Fatal("second reconciliation", err)
	}
}

type unavailableTemplateReferences struct{ AssetTemplateStore }

func (unavailableTemplateReferences) AssetTemplateFileReferenced(context.Context, string) (bool, error) {
	return false, errors.New("reference database unavailable")
}
func TestTemplateMaintenanceDBFailureRetainsCandidate(t *testing.T) {
	ctx := context.Background()
	e, _, store := agentRunEngine(t)
	e.SetAssetStorage(unavailableTemplateReferences{store})
	root := t.TempDir()
	files := attachmentapp.NewDirFileStorage(root)
	e.SetTemplateFileStorage(files)
	name := ulid.Make().String()
	if err := files.WriteFile(ctx, name, []byte("ambiguous commit")); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, name), old, old); err != nil {
		t.Fatal(err)
	}
	if err := e.ReconcileTemplateFiles(ctx, time.Now()); err == nil {
		t.Fatal("reference failure swallowed")
	}
	if _, err := files.ReadFile(ctx, name); err != nil {
		t.Fatal("ambiguous file deleted", err)
	}
}

type failingTemplateDelete struct{ attachmentapp.FileStorage }

func (failingTemplateDelete) DeleteFile(context.Context, string) error {
	return errors.New("sharing violation")
}
func TestTemplateDeleteReportsPendingFileCleanup(t *testing.T) {
	ctx := context.Background()
	e, _, store := agentRunEngine(t)
	e.SetAssetStorage(store)
	e.SetTemplateFileStorage(failingTemplateDelete{attachmentapp.NewDirFileStorage(t.TempDir())})
	tpl, err := store.CreateAssetTemplate(ctx, asset.AssetTemplate{Name: "Delete", TemplateType: asset.TemplateTypeDocument, FilePath: ulid.Make().String()})
	if err != nil {
		t.Fatal(err)
	}
	r := validRequest("template.delete", string(mustJSON(map[string]any{"id": tpl.ID, "expectedVersion": 1})))
	out := handleTemplateDelete(e, ctx, r)
	if !out.OK {
		t.Fatal(out.Error)
	}
	if out.Payload.(map[string]any)["cleanupPending"] != true {
		t.Fatal("file failure hidden", out.Payload)
	}
	if _, err = store.GetAssetTemplate(ctx, tpl.ID); !errors.Is(err, asset.ErrNotFound) {
		t.Fatal("metadata not deleted", err)
	}
}
