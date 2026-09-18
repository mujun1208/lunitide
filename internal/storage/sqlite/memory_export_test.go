package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func TestMemoryExportConcurrentSnapshot(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "export.db")
	store, err := OpenTemplated(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	if _, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "local-user", ScopeKind: "user", Text: "我喜欢绿茶",
		OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	var exported m8core.MemoryFabricExport
	wg.Add(2)
	go func() {
		defer wg.Done()
		out, expErr := store.ExportCanonicalMemoryArchive(ctx, "local-user")
		exported = out
		errCh <- expErr
	}()
	go func() {
		defer wg.Done()
		_, createErr := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
			SubjectID: "local-user", ScopeKind: "user", Text: "我喜欢红茶",
			OperationID: ulid.Make().String(), IdempotencyKey: ulid.Make().String(),
		})
		errCh <- createErr
	}()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(dbPath), "memory-cas", exported.ArtifactID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Records []json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(raw, &archive); err != nil {
		t.Fatal(err)
	}
	if exported.Counts.Records != len(archive.Records) {
		t.Fatalf("snapshot counts=%d records=%d", exported.Counts.Records, len(archive.Records))
	}
	if exported.Counts.Records != exported.Counts.Current+exported.Counts.Tombstones {
		t.Fatalf("inconsistent counts %+v", exported.Counts)
	}
}
