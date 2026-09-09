package officeapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

func TestOfficeBundlePinsVersionsAndRetriesWithoutOverwriting(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v1 := generatedWord(t, svc, task, "one")
	v2 := generatedWord(t, svc, task, "two")
	if _, err := store.AcceptOfficeVersion(ctx, task.ID, v1.ArtifactID, v1.ID, 1); err != nil {
		t.Fatal(err)
	}
	bundle, err := svc.CreateBundle(ctx, task.ID, "周报交付", []string{v1.ID, v2.ID}, "bundle")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Files) != 2 || !bundle.Files[0].Accepted || bundle.Files[1].Accepted {
		t.Fatalf("manifest: %#v", bundle)
	}
	if _, err = svc.Patch(ctx, task.ID, v1.ID, 2, textPatchFor(t, v1, "后续修订"), "patch"); err != nil {
		t.Fatal(err)
	}
	retry, err := svc.CreateBundle(ctx, task.ID, "周报交付", []string{v1.ID, v2.ID}, "bundle")
	if err != nil || retry.ID != bundle.ID {
		t.Fatalf("manifest retry: %#v %v", retry, err)
	}
	dir := t.TempDir()
	out, err := svc.ExportBundle(ctx, task.ID, bundle.ID, dir)
	if err != nil || !out.Complete || len(out.Files) != 2 {
		t.Fatalf("export: %#v %v", out, err)
	}
	for i, file := range out.Files {
		data, e := os.ReadFile(file.Path)
		if e != nil || digest(data) != bundle.Files[i].SHA256 || file.Reused {
			t.Fatalf("fixed version not exported: %#v %v", file, e)
		}
	}
	manifest, err := os.ReadFile(out.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved domain.Bundle
	if err = json.Unmarshal(manifest, &saved); err != nil || saved.Files[0].VersionID != v1.ID {
		t.Fatalf("manifest changed: %#v %v", saved, err)
	}
	again, err := svc.ExportBundle(ctx, task.ID, bundle.ID, dir)
	if err != nil || !again.Complete || len(again.Files) != 2 || !again.Files[0].Reused || !again.Files[1].Reused {
		t.Fatalf("retry: %#v %v", again, err)
	}
	receipts, err := store.ListOfficeStepReceipts(ctx, task.ID, "")
	if err != nil || len(receipts) != 2 {
		t.Fatalf("receipts duplicate or missing: %#v %v", receipts, err)
	}
	// The only external edit is to this test's own temporary export fixture.
	if err = os.WriteFile(out.Files[0].Path, []byte("external edit must remain"), 0600); err != nil {
		t.Fatal(err)
	}
	failed, err := svc.ExportBundle(ctx, task.ID, bundle.ID, dir)
	if err == nil || failed.Complete {
		t.Fatalf("overwritten external edit: %#v %v", failed, err)
	}
	actual, err := os.ReadFile(out.Files[0].Path)
	if err != nil || string(actual) != "external edit must remain" {
		t.Fatalf("external change overwritten: %q %v", actual, err)
	}
}

func TestOfficeBundleAtomicPublicationHandlesRacingRetry(t *testing.T) {
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// A terminated older attempt may have left an unfinished staging file.
	// Its presence cannot be confused with a delivered artifact on retry.
	if err = os.WriteFile(filepath.Join(dir, ".office-export-interrupted.tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	body := []byte("the complete immutable artifact")
	const writers = 6
	var workers sync.WaitGroup
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, e := writeBundleFile(context.Background(), r, "artifact.docx", body)
			results <- e
		}()
	}
	workers.Wait()
	close(results)
	for err = range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(filepath.Join(dir, "artifact.docx"))
	if err != nil || string(actual) != string(body) {
		t.Fatalf("partial target: %q %v", actual, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("active staging leaked: %#v %v", files, err)
	}
}

func TestOfficeBundlePartialExportResumesAndPreservesScope(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v1 := generatedWord(t, svc, task, "one")
	v2 := generatedWord(t, svc, task, "two")
	bundle, err := svc.CreateBundle(ctx, task.ID, "测试交付", []string{v1.ID, v2.ID}, "bundle")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	folder := filepath.Join(dir, "bundle-"+bundle.ID)
	if err = os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	blockedPath := filepath.Join(folder, bundle.Files[1].Name)
	if err = os.WriteFile(blockedPath, []byte("fixture conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	partial, err := svc.ExportBundle(ctx, task.ID, bundle.ID, dir)
	if err == nil || partial.Complete || len(partial.Files) != 1 {
		t.Fatalf("partial result: %#v %v", partial, err)
	}
	if err = os.Remove(blockedPath); err != nil {
		t.Fatal(err)
	}
	complete, err := svc.ExportBundle(ctx, task.ID, bundle.ID, dir)
	if err != nil || !complete.Complete || len(complete.Files) != 2 || !complete.Files[0].Reused || complete.Files[1].Reused {
		t.Fatalf("resume: %#v %v", complete, err)
	}
	other, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: task.SessionID, Title: "其他任务"}, "other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateBundle(ctx, other.ID, "越界", []string{v1.ID}, "wrong-task"); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-task version: %v", err)
	}
	if _, err = svc.ExportBundle(ctx, other.ID, bundle.ID, dir); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-task bundle: %v", err)
	}
	if _, err = svc.CreateBundle(ctx, task.ID, "重复文件", []string{v1.ID, v1.ID}, "duplicate"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("duplicate artifact: %v", err)
	}
}
