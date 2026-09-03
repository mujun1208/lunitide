package m8app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestParseBodyIndexerSplitsMarkdownAndRejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "amm.md")
	body := "# ATA 32\n\nGear retraction fault isolation.\n\n# ATA 33\n\nLights."
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := m8core.KBDocument{
		DocumentID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1,
		MediaType: "text/markdown", ContentRef: path,
		SHA256: strings.Repeat("ab", 32), SourceLocator: path,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	chunks, err := m8app.ParseBodyIndexer(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("want heading split, got %d", len(chunks))
	}
	if strings.TrimSpace(chunks[0].Body) == "" {
		t.Fatal("body must not be empty")
	}
}

func TestParseBodyIndexerEmptyFileFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(path, []byte("   "), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := m8core.KBDocument{
		DocumentID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1,
		MediaType: "text/markdown", ContentRef: path,
		SHA256: strings.Repeat("cd", 32), SourceLocator: path,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := m8app.ParseBodyIndexer(context.Background(), doc); err == nil {
		t.Fatal("empty body must fail index")
	}
}

func TestParseBodyIndexerRelativePathFails(t *testing.T) {
	doc := m8core.KBDocument{
		DocumentID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1,
		MediaType: "text/plain", ContentRef: "relative.md",
		SHA256: strings.Repeat("ef", 32), SourceLocator: "relative.md",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := m8app.ParseBodyIndexer(context.Background(), doc); err == nil {
		t.Fatal("relative content_ref must fail")
	}
}
