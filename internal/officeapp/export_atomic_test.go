package officeapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOfficeSingleExportResumesAfterInterruptedStaging(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "source")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".office-export-interrupted.tmp"), []byte("partial content"), 0600); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := svc.Export(cancelled, task.ID, v.ID, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled export: %v", err)
	}
	path, err := svc.Export(ctx, task.ID, v.ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || digest(data) != v.SHA256 {
		t.Fatalf("exported incomplete bytes: %v", err)
	}
	replayed, err := svc.Export(ctx, task.ID, v.ID, dir)
	if err != nil || replayed != path {
		t.Fatalf("retry failed: %q %v", replayed, err)
	}
	receipts, err := store.ListOfficeStepReceipts(ctx, task.ID, "")
	if err != nil || len(receipts) != 1 {
		t.Fatalf("missing/duplicate receipt: %#v %v", receipts, err)
	}
	if err = os.WriteFile(path, []byte("external revision"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Export(ctx, task.ID, v.ID, dir); err == nil {
		t.Fatal("external revision overwritten")
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "external revision" {
		t.Fatalf("external revision lost: %q %v", data, err)
	}
}
