package attachmentapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportPathStoresTheFileAndExtractsAPrefix(t *testing.T) {
	prev := importExtractMax
	importExtractMax = len("客户")
	t.Cleanup(func() { importExtractMax = prev })

	dir := t.TempDir()
	src := filepath.Join(dir, "note.txt")
	body := "客户与商机TAILMARKER"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
	att, err := svc.ImportPath(context.Background(), mustULID(), "", src, "note.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if att.Size != int64(len(body)) {
		t.Fatalf("stored size = %d, want %d", att.Size, len(body))
	}
	if !strings.Contains(att.ParsedText, "客户") || strings.Contains(att.ParsedText, "TAILMARKER") {
		t.Fatalf("parsed text = %q", att.ParsedText)
	}
	saved, err := svc.fileStorage.ReadFile(context.Background(), att.FileRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != body {
		t.Fatalf("saved bytes = %q", saved)
	}
}

func TestImportPathKeepsAnOfficeFileWithoutParsingAPrefix(t *testing.T) {
	prev := importExtractMax
	importExtractMax = 4
	t.Cleanup(func() { importExtractMax = prev })

	dir := t.TempDir()
	src := filepath.Join(dir, "deck.docx")
	body := "PK\x03\x04TAILMARKER"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
	att, err := svc.ImportPath(context.Background(), mustULID(), "", src, "deck.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(att.ParsedText, "TAILMARKER") || strings.Contains(att.ParsedText, "PK") {
		t.Fatalf("parsed text = %q", att.ParsedText)
	}
	if !strings.Contains(att.ParsedText, "完整保存") {
		t.Fatalf("parsed text = %q", att.ParsedText)
	}
	saved, err := svc.fileStorage.ReadFile(context.Background(), att.FileRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != body {
		t.Fatalf("saved bytes = %q", saved)
	}
}
