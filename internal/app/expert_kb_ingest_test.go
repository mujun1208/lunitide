package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/officetools"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestExpertKBIngestSplitsMarkdownAndFailsEmpty(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "ingest.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	kb := m8app.NewKBService(repo, "local-user")
	ingest := NewExpertKBIngest(repo, "local-user")
	ctx := context.Background()
	expertID := ulid.Make().String()

	dir := t.TempDir()
	path := filepath.Join(dir, "amm.md")
	if err := os.WriteFile(path, []byte("# ATA 32\n\nGear retraction fault isolation.\n\n# ATA 33\n\nLights."), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ingest.Ingest(ctx, ExpertKBIngestInput{
		ExpertID: expertID, Path: path,
		SourceLocator: "mro://AMM/42?ata=32&status=controlled&tail=B-1000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Documents) != 1 || res.Documents[0].IndexState != m8core.KBIndexReady {
		t.Fatalf("ingest = %+v", res)
	}
	hit, err := kb.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "retraction"})
	if err != nil || hit.Explanation.Missing || len(hit.Hits) == 0 {
		t.Fatalf("search after ingest = %+v %v", hit, err)
	}
	if !strings.Contains(hit.Hits[0].Quote, "retraction") {
		t.Fatalf("quote = %q", hit.Hits[0].Quote)
	}

	empty := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(empty, []byte("   "), 0o644); err != nil {
		t.Fatal(err)
	}
	failed, err := ingest.Ingest(ctx, ExpertKBIngestInput{ExpertID: expertID, Path: empty})
	if !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("empty ingest err = %v", err)
	}
	if len(failed.Documents) != 1 || failed.Documents[0].IndexState != m8core.KBIndexFailed {
		t.Fatalf("empty ingest state = %+v", failed)
	}
}

func TestExpertKBIngestDocxBecomesSearchable(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "ingest-docx.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	kb := m8app.NewKBService(repo, "local-user")
	ingest := NewExpertKBIngest(repo, "local-user")
	ctx := context.Background()
	expertID := ulid.Make().String()

	data, err := officetools.GenDocx("AMM 起落架", []officetools.DocxBlock{
		{Type: "heading", Text: "起落架排故"},
		{Type: "paragraph", Text: "Hydraulic actuator retraction isolation procedure for the landing gear system per AMM revision record."},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "amm.docx")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ingest.Ingest(ctx, ExpertKBIngestInput{
		ExpertID: expertID, Path: path,
		SourceLocator: "mro://AMM/42?ata=32&status=controlled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Documents) != 1 || res.Documents[0].IndexState != m8core.KBIndexReady {
		t.Fatalf("docx ingest = %+v", res)
	}
	hit, err := kb.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "retraction"})
	if err != nil || hit.Explanation.Missing || len(hit.Hits) == 0 {
		t.Fatalf("search after docx ingest = %+v %v", hit, err)
	}
}

func TestExpertKBIngestParksBinaryAsFailed(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "ingest-bin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ingest := NewExpertKBIngest(store.AgentRuntimeRepository(), "local-user")
	path := filepath.Join(t.TempDir(), "scan.bin")
	if err := os.WriteFile(path, []byte("\x00\x01\x02\x03 not text \x00\xff payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ingest.Ingest(context.Background(), ExpertKBIngestInput{
		ExpertID: ulid.Make().String(), Path: path,
	})
	if !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("binary ingest err = %v want ErrKBIndexFailed", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].IndexState != m8core.KBIndexFailed {
		t.Fatalf("binary ingest state = %+v", res)
	}
	if strings.TrimSpace(res.Documents[0].FailReason) == "" {
		t.Fatalf("binary ingest must carry a fail reason: %+v", res.Documents[0])
	}
}

func TestExpertKBIngestSplitsOverChunkCap(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "ingest-cap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ingest := NewExpertKBIngest(store.AgentRuntimeRepository(), "local-user")
	var b strings.Builder
	for i := 0; i < m8core.MaxKBChunksPerVersion+1; i++ {
		fmt.Fprintf(&b, "# Heading %d\n\nbody text for chunk %d about landing gear.\n\n", i, i)
	}
	path := filepath.Join(t.TempDir(), "huge.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ingest.Ingest(context.Background(), ExpertKBIngestInput{
		ExpertID: ulid.Make().String(), Path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Documents) < 2 {
		t.Fatalf("want multiple document ids, got %d", len(res.Documents))
	}
	ids := map[string]struct{}{}
	for _, d := range res.Documents {
		if d.IndexState != m8core.KBIndexReady {
			t.Fatalf("doc %+v", d)
		}
		ids[d.DocumentID] = struct{}{}
	}
	if len(ids) < 2 {
		t.Fatal("split must use distinct document ids")
	}
}
