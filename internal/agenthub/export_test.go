package agenthub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyThreadExportAllowsPptxAndPdf(t *testing.T) {
	workspace := t.TempDir()
	exportDir := t.TempDir()
	pptx := filepath.Join(workspace, "slides", "deck.pptx")
	pdf := filepath.Join(workspace, "notes.pdf")
	writeFile(t, pptx, "pptx-body")
	writeFile(t, pdf, "pdf-body")

	if err := CopyThreadExport(workspace, exportDir); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(exportDir, "deck.pptx"), "pptx-body")
	assertFile(t, filepath.Join(exportDir, "notes.pdf"), "pdf-body")
}

func TestCopyThreadExportSkipsSourceExtensions(t *testing.T) {
	workspace := t.TempDir()
	exportDir := t.TempDir()
	for _, name := range []string{"main.go", "app.ts", "view.tsx", "index.js", "script.py"} {
		writeFile(t, filepath.Join(workspace, name), name)
	}

	if err := CopyThreadExport(workspace, exportDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("source files were copied: %v", names(entries))
	}
}

func TestCopyThreadExportEmptyDirIsNoop(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "empty-export-noop.pptx"), "pptx-body")

	if err := CopyThreadExport(workspace, ""); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(cwd, "empty-export-noop.pptx")); !os.IsNotExist(statErr) {
		t.Fatalf("empty export_dir must not copy into cwd: %v", statErr)
	}
}

func TestCopyThreadExportRejectsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	exportDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.pptx")
	writeFile(t, outside, "secret")

	if PathAllowed(workspace, exportDir, outside) {
		t.Fatalf("outside file must fail PathAllowed: %s", outside)
	}
	if err := CopyThreadExport(workspace, exportDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(exportDir, "secret.pptx")); !os.IsNotExist(err) {
		t.Fatalf("outside workspace file was copied: %v", err)
	}
}

func TestExportOnSuccessCopiesAllowlisted(t *testing.T) {
	db := openThreadDB(t)
	store := NewThreadStore(db)
	workspace := t.TempDir()
	exportDir := t.TempDir()
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "loopback", "Export", false)
	thread.WorkspaceRoot = workspace
	thread.ExportDir = exportDir
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "deck.pptx"), "pptx-body")

	if err := setThreadStatus(store, thread.ID, "success"); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(exportDir, "deck.pptx"), "pptx-body")
	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("status = %q, want success", got.Status)
	}
}

func TestExportFailureDoesNotRevertSuccess(t *testing.T) {
	db := openThreadDB(t)
	store := NewThreadStore(db)
	workspace := t.TempDir()
	exportDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(exportDir, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	thread := sampleThread("01ARZ3NDEKTSV4RRFFQ69G5FAE", "loopback", "Export", false)
	thread.WorkspaceRoot = workspace
	thread.ExportDir = exportDir
	if err := store.Insert(thread); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, "deck.pptx"), "pptx-body")

	if err := setThreadStatus(store, thread.ID, "success"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("status = %q, want success after export failure", got.Status)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, body string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("%s = %q, want %q", path, got, body)
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}
