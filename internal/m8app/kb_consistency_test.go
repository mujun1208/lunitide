package m8app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/oklog/ulid/v2"
)

func TestKBLatestVersionOnlyAcrossFTSShortQueryAndDense(t *testing.T) {
	store := openSliceStore(t)
	ctx := context.Background()
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	svc.SetDenseEmbedder(func(context.Context, []string) ([][]float32, error) { return [][]float32{{1, 0}}, nil })
	expertID, docID := ulid.Make().String(), ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	for version, body := range []string{"obsoleteonly ZZ deprecated instruction", "replacement current instruction"} {
		text := body
		_, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{CollectionID: coll.CollectionID, DocumentID: docID, ExpectedVersion: int64(version), MediaType: "text/plain", ContentRef: "blob://fixture", SHA256: sha64([]string{"a", "b"}[version]), SourceLocator: "fixture", Projector: func(_ context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return m8app.ChunksFromParts(doc, []string{text})
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"obsoleteonly", "ZZ", "unrelated dense query"} {
		out, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: q})
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range out.Hits {
			if strings.Contains(hit.Quote, "obsoleteonly") || strings.Contains(hit.Quote, "ZZ") {
				t.Fatalf("superseded dense/FTS row recalled: %+v", hit)
			}
		}
	}
	svc.SetDenseEmbedder(nil)
	for _, q := range []string{"obsoleteonly", "ZZ"} {
		out, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: q})
		if err != nil || len(out.Hits) != 0 {
			t.Fatalf("superseded FTS/LIKE row recalled: %+v %v", out, err)
		}
	}
}

func TestKBFailedProjectionPersistsFailureAndCanRetry(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()
	svc := m8app.NewKBService(repo, "local-user")
	expertID, docID := ulid.Make().String(), ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	in := m8app.KBUpsertInput{CollectionID: coll.CollectionID, DocumentID: docID, MediaType: "text/plain", ContentRef: "blob://fixture", SHA256: sha64("a"), SourceLocator: "fixture", Projector: func(context.Context, m8core.KBDocument) ([]m8core.KBChunk, error) {
		return nil, errors.New("fixture parser offline")
	}}
	result, err := svc.UpsertDocument(ctx, in)
	if !errors.Is(err, m8app.ErrKBIndexFailed) || result.IndexState != "failed" {
		t.Fatalf("failure response: %+v %v", result, err)
	}
	err = repo.TransactKB(ctx, func(tx m8app.KBTx) error {
		doc, exists, err := tx.GetKBLatestDocument(docID)
		if err != nil {
			return err
		}
		if !exists || doc.IndexState != "failed" {
			t.Fatalf("failed document lost: %+v", doc)
		}
		docs, ready, chunks, err := tx.CountKBStats(coll.CollectionID)
		if err != nil {
			return err
		}
		if docs != 1 || ready != 0 || chunks != 0 {
			t.Fatalf("partial projection: %d/%d/%d", docs, ready, chunks)
		}
		events, err := tx.ListAuditEvents()
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Action == "kb.document.index_failed" && event.ResourceID == docID {
				return nil
			}
		}
		t.Fatal("failure audit rolled back")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	in.ExpectedVersion = result.Version
	in.Projector = func(_ context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
		return m8app.ChunksFromParts(doc, []string{"recovered grounded body"})
	}
	result, err = svc.UpsertDocument(ctx, in)
	if err != nil || result.IndexState != "ready" || result.Version != 2 {
		t.Fatalf("retry did not recover: %+v %v", result, err)
	}
}

func TestKBCitationRequiresCurrentScopedStoredEvidence(t *testing.T) {
	store := openSliceStore(t)
	ctx := context.Background()
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	expertID, docID := ulid.Make().String(), ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	in := m8app.KBUpsertInput{CollectionID: coll.CollectionID, DocumentID: docID, MediaType: "text/plain", ContentRef: "blob://citation", SHA256: sha64("a"), SourceLocator: "fixture", Projector: func(_ context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
		return m8app.ChunksFromParts(doc, []string{"grounded citation text"})
	}}
	if _, err := svc.UpsertDocument(ctx, in); err != nil {
		t.Fatal(err)
	}
	search, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "grounded"})
	if err != nil || len(search.Hits) != 1 {
		t.Fatalf("search: %+v %v", search, err)
	}
	hit := search.Hits[0]
	if _, err := svc.Cite(ctx, hit); err != nil {
		t.Fatalf("valid stored evidence refused: %v", err)
	}
	for _, mutation := range []func(*m8app.KBCitedHit){
		func(h *m8app.KBCitedHit) { h.ExpertID = ulid.Make().String() },
		func(h *m8app.KBCitedHit) { h.DocID = ulid.Make().String() },
		func(h *m8app.KBCitedHit) { h.Locator = `{}` },
		func(h *m8app.KBCitedHit) { h.Quote = "invented quotation" },
		func(h *m8app.KBCitedHit) { h.Revision = "forged revision" },
		func(h *m8app.KBCitedHit) {
			var loc map[string]any
			_ = json.Unmarshal([]byte(h.Locator), &loc)
			loc["page"] = 999
			raw, _ := json.Marshal(loc)
			h.Locator = string(raw)
		},
	} {
		candidate := hit
		mutation(&candidate)
		if _, err := svc.Cite(ctx, candidate); err == nil {
			t.Fatalf("forged citation accepted: %+v", candidate)
		}
	}
	in.ExpectedVersion = 1
	in.SHA256 = sha64("b")
	if _, err := svc.UpsertDocument(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cite(ctx, hit); err == nil {
		t.Fatal("superseded citation accepted as current")
	}
}
