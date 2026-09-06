package m8app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/oklog/ulid/v2"
)

func TestKBProjectionDoesNotHoldWriterAndConcurrentPublishConflicts(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	svc := m8app.NewKBService(repo, "local-user")
	coll, err := svc.EnsureExpertCollection(ctx, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	in := m8app.KBUpsertInput{CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(), MediaType: "text/plain", ContentRef: "blob://boundary", SHA256: sha64("a"), SourceLocator: "fixture"}
	in.Projector = func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return m8app.ChunksFromParts(doc, []string{"late first projection"})
	}
	firstDone := make(chan error, 1)
	go func() { _, err := svc.UpsertDocument(ctx, in); firstDone <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("projector did not start")
	}
	// Unrelated write and another publish both finish while the first parser waits.
	writeCtx, writeCancel := context.WithTimeout(ctx, time.Second)
	defer writeCancel()
	if err := svc.EnsureCollection(writeCtx, ulid.Make().String(), ulid.Make().String()); err != nil {
		close(release)
		t.Fatalf("parser blocked writer: %v", err)
	}
	second := in
	second.SHA256 = sha64("b")
	second.Projector = func(_ context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
		return m8app.ChunksFromParts(doc, []string{"winning projection"})
	}
	if _, err := svc.UpsertDocument(writeCtx, second); err != nil {
		close(release)
		t.Fatalf("second publish blocked: %v", err)
	}
	close(release)
	if err := <-firstDone; !errors.Is(err, m8app.ErrKBVersionConflict) {
		t.Fatalf("late publish must conflict: %v", err)
	}
	if err := repo.TransactKB(ctx, func(tx m8app.KBTx) error {
		doc, ok, err := tx.GetKBLatestDocument(in.DocumentID)
		if err != nil {
			return err
		}
		if !ok || doc.Version != 1 || doc.SHA256 != second.SHA256 {
			t.Fatalf("winner overwritten: %+v", doc)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestKBLongMarkdownPreservesEveryRune(t *testing.T) {
	text := "# Heading\n" + strings.Repeat("甲乙丙丁", 1500) + "\n# Last\n唯一结尾"
	parts := m8app.SplitSearchableParts("text/markdown", text)
	joined := strings.Join(parts, "")
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	if compact(joined) != compact(text) {
		t.Fatal("markdown lost content during chunking")
	}
	doc := m8core.KBDocument{DocumentID: ulid.Make().String(), Version: 1, SourceLocator: "fixture", SHA256: sha64("a")}
	if _, err := m8app.ChunksFromParts(doc, parts); err != nil {
		t.Fatal(err)
	}
	if _, err := m8app.ChunksFromParts(doc, []string{strings.Repeat("x", m8core.MaxKBChunkBody+1)}); err == nil {
		t.Fatal("oversized supplied chunk silently clipped")
	}
}
