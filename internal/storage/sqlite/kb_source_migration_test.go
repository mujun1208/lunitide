package sqlite

import (
	"context"
	"fmt"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKBSourceMigrationKeepsHistoryButRequiresLegacyRefresh(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "legacy.md")
	var body strings.Builder
	for i := 0; i < 513; i++ {
		fmt.Fprintf(&body, "# Legacy section %d\nLegacy source content\n", i)
	}
	raw := []byte(body.String())
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	expert := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	doc := m8core.KBDocument{DocumentID: ulid.Make().String(), CollectionID: coll.CollectionID, Version: 1, MediaType: "text/markdown", ContentRef: file, SHA256: m8app.SourceDigest(raw), SourceLocator: file, IndexState: "ready", CreatedAt: "2026-09-06T00:00:00Z"}
	err = store.AgentRuntimeRepository().TransactKB(ctx, func(tx m8app.KBTx) error {
		if e := tx.PutKBDocument(doc); e != nil {
			return e
		}
		next := doc
		next.Version = 2
		return tx.PutKBDocument(next)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db := openRaw(t, path)
	if _, err = db.Exec(`DROP TABLE kb_source_documents; DROP TABLE kb_source_versions; DROP TABLE kb_sources;`); err != nil {
		t.Fatal(err)
	}
	migration, err := migrations.Files.ReadFile("0138_kb_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc = m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	stats, err := svc.KnowledgeGet(ctx, expert)
	if err != nil || len(stats.Sources) != 1 || stats.Sources[0].State != "stale" || stats.ReadyCount != 0 {
		t.Fatalf("legacy projection: %+v %v", stats, err)
	}
	refreshed, err := svc.IngestLocalSource(ctx, m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file})
	if err != nil || refreshed.Source.Version != 2 || len(refreshed.Documents) != 2 || refreshed.Documents[0].DocumentID != doc.DocumentID || refreshed.Documents[0].Version != 3 || refreshed.Documents[0].DocumentID == refreshed.Documents[1].DocumentID {
		t.Fatalf("legacy refresh: %+v %v", refreshed, err)
	}
	err = store.AgentRuntimeRepository().TransactKB(ctx, func(tx m8app.KBTx) error {
		documents, e := tx.ListKBDocumentsByCollection(coll.CollectionID)
		if e == nil && len(documents) != 4 {
			t.Fatalf("history lost: %+v", documents)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}
