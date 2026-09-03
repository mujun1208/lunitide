package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestChatKBInjectAddsEvidenceQuote(t *testing.T) {
	e, ingest := newKBInjectEngine(t)
	expertID := ulid.Make().String()
	dir := t.TempDir()
	path := filepath.Join(dir, "amm.md")
	if err := os.WriteFile(path, []byte("# ATA 32\n\nGear retraction fault isolation.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.Ingest(context.Background(), ExpertKBIngestInput{
		ExpertID: expertID, Path: path,
		SourceLocator: "mro://AMM/42?ata=32&status=controlled&tail=B-1000",
	}); err != nil {
		t.Fatal(err)
	}
	pack := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "retraction", ExpertIDs: []string{expertID},
	})
	joined := joinEvidence(pack)
	if !strings.Contains(joined, "retraction") {
		t.Fatalf("evidence missing quote: %q", joined)
	}
	if !strings.Contains(joined, "修订") {
		t.Fatalf("evidence missing revision line: %q", joined)
	}
	if len(pack.KBCites) == 0 {
		t.Fatal("want KB cites for the gate")
	}
}

func TestChatKBInjectSkipsMissingCollection(t *testing.T) {
	e, _ := newKBInjectEngine(t)
	pack := e.prepareChatMemory(context.Background(), chatMemoryRequest{
		Query: "retraction", ExpertIDs: []string{ulid.Make().String()},
	})
	joined := joinEvidence(pack)
	if strings.Contains(joined, "修订") {
		t.Fatalf("missing collection must not inject fake evidence: %q", joined)
	}
	if len(pack.KBCites) != 0 {
		t.Fatalf("cites = %+v", pack.KBCites)
	}
}

func newKBInjectEngine(t *testing.T) (*Engine, *ExpertKBIngest) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "kb-inject.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	kb := m8app.NewKBService(repo, "local-user")
	e := NewEngine(nil, "test")
	e.SetM8SliceServices(kb, nil, nil)
	return e, NewExpertKBIngest(repo, "local-user")
}

func joinEvidence(pack chatMemoryPack) string {
	var b strings.Builder
	for _, src := range pack.Evidence {
		b.WriteString(src.Content)
		b.WriteByte('\n')
	}
	return b.String()
}
