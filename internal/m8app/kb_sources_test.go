package m8app_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKBSourceRefreshChangeDeleteFailureAndReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "kb.db")
	file := filepath.Join(t.TempDir(), "source.md")
	expert := ulid.Make().String()
	store, err := storage.OpenTemplated(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if e := os.WriteFile(file, []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ingest := func(rev *int64) (m8app.KBLocalSourceResult, error) {
		return svc.IngestLocalSource(ctx, m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file, ExpectedRevision: rev})
	}
	search := func(query string) m8app.KBSearchResult {
		t.Helper()
		result, e := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expert, Query: query})
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	write("Original pneumatic procedure")
	first, err := ingest(nil)
	if err != nil {
		t.Fatal(err)
	}
	hit := search("pneumatic")
	if len(hit.Hits) != 1 {
		t.Fatalf("initial hit: %+v", hit)
	}
	replay, err := ingest(nil)
	if err != nil || replay.Source.Version != 1 || replay.Documents[0].DocumentID != first.Documents[0].DocumentID || replay.Documents[0].Version != 1 {
		t.Fatalf("identical replay=%+v %v", replay, err)
	}
	write("Updated hydraulic procedure")
	if _, e := svc.Cite(ctx, hit.Hits[0]); !errors.Is(e, m8app.ErrKBDocumentNotReady) {
		t.Fatalf("changed citation: %v", e)
	}
	if hits := search("pneumatic"); len(hits.Hits) != 0 {
		t.Fatalf("stale result leaked: %+v", hits)
	}
	stats, err := svc.KnowledgeGet(ctx, expert)
	if err != nil || len(stats.Sources) != 1 || stats.Sources[0].State != "stale" {
		t.Fatalf("stale state: %+v %v", stats, err)
	}
	staleRevision := first.Source.Revision
	if _, e := ingest(&staleRevision); !errors.Is(e, m8app.ErrKBVersionConflict) {
		t.Fatalf("stale refresh accepted: %v", e)
	}
	revision := stats.Sources[0].Revision
	second, err := ingest(&revision)
	if err != nil || second.Source.Version != 2 || second.Documents[0].Version != 2 || second.Documents[0].DocumentID != first.Documents[0].DocumentID {
		t.Fatalf("refresh=%+v %v", second, err)
	}
	if len(search("hydraulic").Hits) != 1 {
		t.Fatal("new body missing")
	}
	write("   ")
	failed, err := ingest(nil)
	if !errors.Is(err, m8app.ErrKBIndexFailed) || failed.Source.State != "failed" || failed.Documents[0].IndexState != "failed" {
		t.Fatalf("failed=%+v %v", failed, err)
	}
	if len(search("hydraulic").Hits) != 0 {
		t.Fatal("failed refresh resurrected previous body")
	}
	if e := store.Close(); e != nil {
		t.Fatal(e)
	}
	store, err = storage.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc = m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	stats, err = svc.KnowledgeGet(ctx, expert)
	if err != nil || stats.Sources[0].State != "failed" || len(stats.Sources[0].Versions) != 3 {
		t.Fatalf("reopen=%+v %v", stats, err)
	}
	write("Restored hydraulic procedure")
	restored, err := ingest(nil)
	if err != nil || restored.Source.Version != 4 {
		t.Fatalf("retry=%+v %v", restored, err)
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if len(search("hydraulic").Hits) != 0 {
		t.Fatal("deleted source body leaked")
	}
	stats, err = svc.KnowledgeGet(ctx, expert)
	if err != nil || stats.Sources[0].State != "missing" || stats.ReadyCount != 0 {
		t.Fatalf("missing=%+v %v", stats, err)
	}
}

func TestKBSourceDeleteTombstoneHidesSearchAndCite(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "keep.md")
	if err := os.WriteFile(file, []byte("Tombstone pneumatic note"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "kb-del.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	expert := ulid.Make().String()
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	ingested, err := svc.IngestLocalSource(ctx, m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file})
	if err != nil {
		t.Fatal(err)
	}
	hit, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expert, Query: "pneumatic"})
	if err != nil || len(hit.Hits) != 1 {
		t.Fatalf("pre-delete search %+v %v", hit, err)
	}
	if err := svc.DeleteLocalSource(ctx, m8app.KBDeleteSourceInput{CollectionID: coll.CollectionID, SourceID: ingested.Source.SourceID, ExpectedRevision: ingested.Source.Revision}); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expert, Query: "pneumatic"})
	if err != nil || len(after.Hits) != 0 {
		t.Fatalf("tombstone leaked search %+v %v", after, err)
	}
	if _, e := svc.Cite(ctx, hit.Hits[0]); !errors.Is(e, m8app.ErrKBDocumentNotReady) {
		t.Fatalf("tombstone cite: %v", e)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("delete must not remove the original file")
	}
}

type failingSourceUOW struct {
	m8app.KBUnitOfWork
	fail bool
}
type failingSourceTx struct {
	m8app.KBTx
	m8app.KBSourceTx
	owner *failingSourceUOW
}

func (t failingSourceTx) PutKBChunks(chunks []m8core.KBChunk) error {
	if t.owner.fail {
		t.owner.fail = false
		return errors.New("simulated atomic projection failure")
	}
	return t.KBTx.PutKBChunks(chunks)
}
func (u *failingSourceUOW) TransactKB(ctx context.Context, fn func(m8app.KBTx) error) error {
	return u.KBUnitOfWork.TransactKB(ctx, func(tx m8app.KBTx) error { return fn(failingSourceTx{tx, tx.(m8app.KBSourceTx), u}) })
}
func TestKBSourceProjectionCommitFailureHasDurableFailedReceipt(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	uow := &failingSourceUOW{KBUnitOfWork: store.AgentRuntimeRepository()}
	svc := m8app.NewKBService(uow, "local-user")
	expert := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "commit.md")
	if err = os.WriteFile(file, []byte("Full indexed source"), 0600); err != nil {
		t.Fatal(err)
	}
	input := m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file}
	uow.fail = true
	failed, err := svc.IngestLocalSource(ctx, input)
	if err == nil || len(failed.Documents) != 0 || failed.Source.State != "failed" {
		t.Fatalf("uncommitted result: %+v %v", failed, err)
	}
	stats, err := svc.KnowledgeGet(ctx, expert)
	if err != nil || stats.ChunkCount != 0 || len(stats.Sources[0].Versions) != 1 || stats.Sources[0].Versions[0].State != "failed" {
		t.Fatalf("receipt: %+v %v", stats, err)
	}
	if _, err = svc.IngestLocalSource(ctx, input); err != nil {
		t.Fatalf("retry blocked: %v", err)
	}
}

func TestKBSourceInterruptedRefreshCanBeRetriedWithoutResurrectingOldGroups(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewKBService(repo, "local-user")
	expert := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "interrupted.md")
	if err = os.WriteFile(file, []byte("Original active text"), 0600); err != nil {
		t.Fatal(err)
	}
	input := m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file}
	first, err := svc.IngestLocalSource(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	pending := first.Source
	pending.State = "refreshing"
	pending.Version++
	pending.Revision++
	if err = repo.TransactKB(ctx, func(tx m8app.KBTx) error { return tx.(m8app.KBSourceTx).PutKBSource(pending, first.Source.Revision) }); err != nil {
		t.Fatal(err)
	}
	svc = m8app.NewKBService(repo, "local-user")
	found, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expert, Query: "Original"})
	if err != nil || len(found.Hits) != 0 {
		t.Fatalf("pending leaked: %+v %v", found, err)
	}
	result, err := svc.IngestLocalSource(ctx, input)
	if err != nil || result.Source.Version != 3 {
		t.Fatalf("interrupted retry: %+v %v", result, err)
	}
	stats, err := svc.KnowledgeGet(ctx, expert)
	if err != nil || len(stats.Sources[0].Versions) != 3 || stats.Sources[0].Versions[1].State != "failed" {
		t.Fatalf("lost interrupted history: %+v %v", stats, err)
	}
}

func TestKBSourceShrinkingGroupsHidesRemovedDocuments(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	expert := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "groups.md")
	var body strings.Builder
	for i := 0; i < 513; i++ {
		fmt.Fprintf(&body, "# Section %d\nUniqueoriginal content\n", i)
	}
	if e := os.WriteFile(file, []byte(body.String()), 0600); e != nil {
		t.Fatal(e)
	}
	in := m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file}
	first, err := svc.IngestLocalSource(ctx, in)
	if err != nil || len(first.Documents) != 2 {
		t.Fatalf("groups=%+v %v", first, err)
	}
	if e := os.WriteFile(file, []byte("Onlyreplacement text"), 0600); e != nil {
		t.Fatal(e)
	}
	second, err := svc.IngestLocalSource(ctx, in)
	if err != nil || len(second.Documents) != 1 {
		t.Fatalf("shrink=%+v %v", second, err)
	}
	found, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expert, Query: "Uniqueoriginal"})
	if err != nil || len(found.Hits) != 0 {
		t.Fatalf("removed group leaked=%+v %v", found, err)
	}
	if err = svc.DocumentsReady(ctx, []string{first.Documents[1].DocumentID}); !errors.Is(err, m8app.ErrKBDocumentNotReady) {
		t.Fatalf("old group still ready: %v", err)
	}
}

func TestKBSourceRejectsAnotherCollectionOwnerAndLostCAS(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewKBService(repo, "owner-a")
	expert := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "owned.md")
	if e := os.WriteFile(file, []byte("Owner private text"), 0600); e != nil {
		t.Fatal(e)
	}
	in := m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: file}
	first, err := svc.IngestLocalSource(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	other := m8app.NewKBService(repo, "owner-b")
	if _, e := other.IngestLocalSource(ctx, in); !errors.Is(e, m8app.ErrPayloadInvalid) {
		t.Fatalf("owner bypass: %v", e)
	}
	if _, e := other.KnowledgeGet(ctx, expert); !errors.Is(e, m8app.ErrPayloadInvalid) {
		t.Fatalf("owner read bypass: %v", e)
	}
	old := first.Source
	newer := old
	newer.Revision++
	newer.State = "stale"
	if e := repo.TransactKB(ctx, func(tx m8app.KBTx) error { return tx.(m8app.KBSourceTx).PutKBSource(newer, old.Revision) }); e != nil {
		t.Fatal(e)
	}
	late := old
	late.Revision++
	if e := repo.TransactKB(ctx, func(tx m8app.KBTx) error { return tx.(m8app.KBSourceTx).PutKBSource(late, old.Revision) }); !errors.Is(e, m8app.ErrKBVersionConflict) {
		t.Fatalf("late refresh overwrote newer state: %v", e)
	}
}

func TestKBSourceDirectUpsertRejectsForgedDigest(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	expert := ulid.Make().String()
	coll, e := svc.EnsureExpertCollection(ctx, expert)
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "digest.txt")
	if e = os.WriteFile(file, []byte("Actual source body"), 0600); e != nil {
		t.Fatal(e)
	}
	result, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(), MediaType: "text/plain", ContentRef: file, SHA256: strings.Repeat("a", 64), SourceLocator: file, Projector: m8app.ParseBodyIndexer})
	if !errors.Is(err, m8app.ErrKBIndexFailed) || result.IndexState != "failed" {
		t.Fatalf("forged digest=%+v %v", result, err)
	}
	stats, err := svc.KnowledgeGet(ctx, expert)
	if err != nil || len(stats.Sources) != 1 || stats.Sources[0].State != "failed" {
		t.Fatalf("source receipt=%+v %v", stats, err)
	}
}

func TestKBSourcePagesVerifyOnlyVisibleSourcesAndNavigateHistory(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewKBService(repo, "local-user")
	expert := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expert)
	if err != nil {
		t.Fatal(err)
	}
	var inputs []m8app.KBLocalSourceInput
	var records []m8app.KBLocalSourceResult
	for i := 0; i < 6; i++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("page%d.md", i))
		if err = os.WriteFile(path, []byte(fmt.Sprintf("Source %d", i)), 0600); err != nil {
			t.Fatal(err)
		}
		input := m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: path}
		result, e := svc.IngestLocalSource(ctx, input)
		if e != nil {
			t.Fatal(e)
		}
		inputs = append(inputs, input)
		records = append(records, result)
	}
	if err = os.Remove(inputs[5].Path); err != nil {
		t.Fatal(err)
	}
	page, err := svc.KnowledgeGet(ctx, expert)
	if err != nil || len(page.Sources) != 4 || page.NextSourceCursor == "" {
		t.Fatalf("page one=%+v %v", page, err)
	}
	err = repo.TransactKB(ctx, func(tx m8app.KBTx) error {
		last, _, e := tx.(m8app.KBSourceTx).GetKBSourceByID(coll.CollectionID, records[5].Source.SourceID)
		if e == nil && last.State != "fresh" {
			t.Fatal("off-page file was read")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	page2, err := svc.KnowledgeGetPage(ctx, expert, m8app.KBSourcePage{SourcesAfter: page.NextSourceCursor})
	if err != nil || len(page2.Sources) != 2 || page2.NextSourceCursor != "" || page2.Sources[1].State != "missing" {
		t.Fatalf("page two=%+v %v", page2, err)
	}
	for version := 2; version <= 53; version++ {
		if err = os.WriteFile(inputs[0].Path, []byte(fmt.Sprintf("Changed %d", version)), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = svc.IngestLocalSource(ctx, inputs[0]); err != nil {
			t.Fatal(err)
		}
	}
	history, err := svc.KnowledgeGetPage(ctx, expert, m8app.KBSourcePage{HistorySourceID: records[0].Source.SourceID})
	if err != nil || len(history.Sources) != 1 || len(history.Sources[0].Versions) != 50 || history.Sources[0].NextBeforeVersion != 4 {
		t.Fatalf("history=%+v %v", history, err)
	}
	older, err := svc.KnowledgeGetPage(ctx, expert, m8app.KBSourcePage{HistorySourceID: records[0].Source.SourceID, HistoryBeforeVersion: 4})
	if err != nil || len(older.Sources[0].Versions) != 3 || older.Sources[0].Versions[2].Version != 1 || older.Sources[0].NextBeforeVersion != 0 {
		t.Fatalf("older history=%+v %v", older, err)
	}
	otherExpert := ulid.Make().String()
	if _, err = svc.EnsureExpertCollection(ctx, otherExpert); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.KnowledgeGetPage(ctx, otherExpert, m8app.KBSourcePage{HistorySourceID: records[0].Source.SourceID}); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("history scope bypass: %v", err)
	}
}
