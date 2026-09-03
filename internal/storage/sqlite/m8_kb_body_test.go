package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestPutKBChunksRoundTripBody(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "kb-body.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo := store.AgentRuntimeRepository()
	collID := ulid.Make().String()
	docID := ulid.Make().String()
	chunkID := ulid.Make().String()
	now := "2026-09-03T00:00:00Z"
	err = repo.TransactKB(ctx, func(tx m8app.KBTx) error {
		if err := tx.PutKBCollectionIfAbsent(m8app.KBCollection{
			CollectionID: collID, SubjectID: "local-user", ScopeID: "expert:" + collID,
			AuthPolicy: "local-owner", CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := tx.PutKBDocument(m8core.KBDocument{
			DocumentID: docID, CollectionID: collID, Version: 1,
			MediaType: "text/markdown", ContentRef: "blob://x",
			SHA256: strings.Repeat("ab", 32), SourceLocator: "file://x.md",
			IndexState: m8core.KBIndexReady, CreatedAt: now,
		}); err != nil {
			return err
		}
		return tx.PutKBChunks([]m8core.KBChunk{{
			ChunkID: chunkID, DocumentID: docID, DocumentVersion: 1, Ordinal: 0,
			ContentDigest: strings.Repeat("cd", 32),
			LocatorJSON:   `{"documentId":"` + docID + `","version":1,"ordinal":0,"ata":"32"}`,
			Body:          "Gear retraction fault isolation.",
			CreatedAt:     now,
		}})
	})
	if err != nil {
		t.Fatal(err)
	}
	var got m8core.KBChunk
	err = repo.TransactKB(ctx, func(tx m8app.KBTx) error {
		var gerr error
		got, gerr = tx.GetKBChunk(chunkID)
		return gerr
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "Gear retraction fault isolation." {
		t.Fatalf("body = %q", got.Body)
	}
	if !strings.Contains(got.LocatorJSON, `"ata":"32"`) {
		t.Fatalf("locator = %s", got.LocatorJSON)
	}
}
