package m8app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestKBSearchNoEmbedStaysFTS(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	expertID := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/plain", ContentRef: "blob://kb/ata", SHA256: sha64("a"),
		SourceLocator: "mro://AMM/42?ata=32&status=controlled",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{ChunkID: ulid.Make().String(), Body: "ATA 32-00 landing gear retraction"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "ATA"})
	if err != nil || got.IndexVersion != "fts5-trigram" || len(got.Hits) == 0 {
		t.Fatalf("D-E1 FTS: %+v %v", got, err)
	}
	if !containsStr(got.Explanation.Reasons, "fts5 body match") {
		t.Fatalf("D-E1 reasons: %+v", got.Explanation.Reasons)
	}
}

func TestKBSearchEmbedFailureStaysFTS(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	svc.SetDenseEmbedder(func(context.Context, []string) ([][]float32, error) {
		return [][]float32{{1, 0}}, nil
	})
	ctx := context.Background()
	expertID := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/plain", ContentRef: "blob://kb/ata2", SHA256: sha64("b"),
		SourceLocator: "mro://AMM/42",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{ChunkID: ulid.Make().String(), Body: "ATA 32 landing gear"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	svc.SetDenseEmbedder(func(context.Context, []string) ([][]float32, error) {
		return nil, errors.New("embed down")
	})
	got, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "ATA"})
	if err != nil || got.IndexVersion != "fts5-trigram" || len(got.Hits) == 0 {
		t.Fatalf("D-E3: %+v %v", got, err)
	}
}

func TestKBSearchHybridKeepsFTSAndDense(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	svc.SetDenseEmbedder(func(_ context.Context, texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i, text := range texts {
			switch {
			case text == "ATA":
				out[i] = []float32{0.15, 0.95}
			case strings.Contains(text, "起落架"):
				out[i] = []float32{0, 1}
			default:
				out[i] = []float32{1, 0}
			}
		}
		return out, nil
	})
	ctx := context.Background()
	expertID := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/plain", ContentRef: "blob://kb/a", SHA256: sha64("c"),
		SourceLocator: "mro://AMM/1?ata=32",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{ChunkID: ulid.Make().String(), Body: "ATA 32-00 retraction checklist"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/plain", ContentRef: "blob://kb/b", SHA256: sha64("d"),
		SourceLocator: "mro://AMM/1",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{ChunkID: ulid.Make().String(), Body: "起落架收上故障隔离步骤"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "ATA", TopK: 6})
	if err != nil {
		t.Fatal(err)
	}
	if got.IndexVersion != "fts5+dense-v1" {
		t.Fatalf("D-E2 version=%s", got.IndexVersion)
	}
	var ata, syn bool
	for _, h := range got.Hits {
		if strings.Contains(h.Quote, "ATA") {
			ata = true
		}
		if strings.Contains(h.Quote, "起落架") {
			syn = true
		}
	}
	if !ata || !syn {
		t.Fatalf("D-E2 both paths: ata=%v syn=%v hits=%+v", ata, syn, got.Hits)
	}
	if !containsStr(got.Explanation.Reasons, "fts5 body match") {
		t.Fatalf("D-E2 must keep FTS reason: %+v", got.Explanation.Reasons)
	}
}

type depthUoW struct {
	inner m8app.KBUnitOfWork
	depth *int
}

func (u depthUoW) TransactKB(ctx context.Context, fn func(m8app.KBTx) error) error {
	*u.depth++
	defer func() { *u.depth-- }()
	return u.inner.TransactKB(ctx, fn)
}

func TestKBEmbedHappensAfterCommit(t *testing.T) {
	store := openSliceStore(t)
	depth := 0
	httpDuringTX := false
	svc := m8app.NewKBService(depthUoW{inner: store.AgentRuntimeRepository(), depth: &depth}, "local-user")
	svc.SetDenseEmbedder(func(context.Context, []string) ([][]float32, error) {
		if depth > 0 {
			httpDuringTX = true
		}
		return [][]float32{{1, 0}}, nil
	})
	ctx := context.Background()
	expertID := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/plain", ContentRef: "blob://kb/e5", SHA256: sha64("e"),
		SourceLocator: "mro://AMM/1",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{ChunkID: ulid.Make().String(), Body: "ATA projector body"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if httpDuringTX {
		t.Fatal("D-E5 embed HTTP must not run inside the ingest transaction")
	}
	scope := m8app.ExpertScopeID(expertID)
	var blobs int
	if err := store.AgentRuntimeRepository().TransactKB(ctx, func(tx m8app.KBTx) error {
		rows, err := tx.ListKBChunkEmbeddings(scope)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if _, ok := gateway.DecodeEmbeddingBLOB(row.Chunk.Embedding); ok {
				blobs++
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if blobs == 0 {
		t.Fatal("D-E5 second transaction must persist embedding")
	}
}

