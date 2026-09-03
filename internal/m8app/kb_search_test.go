package m8app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

type goldenItem struct {
	Q                string `json:"q"`
	ExpectDocType    string `json:"expectDocType"`
	ExpectContains   string `json:"expectContains"`
	ExpectEmpty      bool   `json:"expectEmpty"`
	ExpectNotAdopted string `json:"expectNotAdopted"`
	ForbidBarePN     bool   `json:"forbidBarePN"`
}

func locatorField(t *testing.T, raw, key string) string {
	t.Helper()
	var loc map[string]any
	if err := json.Unmarshal([]byte(raw), &loc); err != nil {
		t.Fatalf("locator %q: %v", raw, err)
	}
	s, _ := loc[key].(string)
	return s
}

func loadGoldenP0(t *testing.T) []goldenItem {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mro", "golden_p0.json"))
	if err != nil {
		t.Fatal(err)
	}
	var items []goldenItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func TestKBSearchHitsMarkdownAndMissingEmpty(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	expertID := ulid.Make().String()

	miss, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "retraction"})
	if err != nil {
		t.Fatal(err)
	}
	if !miss.Explanation.Missing || len(miss.Hits) != 0 {
		t.Fatalf("empty store must be missing: %+v", miss)
	}

	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil || len(coll.CollectionID) != 26 {
		t.Fatalf("ensure: %+v %v", coll, err)
	}
	again, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil || again.CollectionID != coll.CollectionID {
		t.Fatalf("ensure not idempotent: %+v %v", again, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "amm.md")
	body := "# ATA 32\n\nGear retraction fault isolation procedure.\n\n# ATA 33\n\nCabin lights."
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/markdown", ContentRef: path, SHA256: sha64("d"),
		SourceLocator: "mro://AMM/42?ata=32&status=controlled",
		Projector:     m8app.ParseBodyIndexer,
	}); err != nil {
		t.Fatal(err)
	}

	hit, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "retraction", TailNo: "B-0000"})
	if err != nil {
		t.Fatal(err)
	}
	if hit.Explanation.Missing || len(hit.Hits) == 0 {
		t.Fatalf("want quote hit, got %+v", hit)
	}
	if !strings.Contains(hit.Hits[0].Quote, "retraction") {
		t.Fatalf("quote = %q", hit.Hits[0].Quote)
	}
	if hit.Hits[0].Revision != "42" {
		t.Fatalf("revision = %q", hit.Hits[0].Revision)
	}

	stats, err := svc.KnowledgeGet(ctx, expertID)
	if err != nil || stats.CollectionID != coll.CollectionID || stats.Missing || stats.ChunkCount < 1 {
		t.Fatalf("knowledge.get = %+v err=%v", stats, err)
	}
}

func TestKBSearchEffectivityDropsWrongTail(t *testing.T) {
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
		MediaType: "text/plain", ContentRef: "blob://kb/tail", SHA256: sha64("e"),
		SourceLocator: "mro://AMM/42?tail=B-1234",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{
				ChunkID:     ulid.Make().String(),
				Body:        "Gear retraction fault isolation.",
				LocatorJSON: `{"documentId":"` + doc.DocumentID + `","version":1,"ordinal":0,"docType":"AMM","revision":"42","tails":["B-1234"]}`,
			}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	dropped, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "retraction", TailNo: "B-9999"})
	if err != nil {
		t.Fatal(err)
	}
	if !dropped.Explanation.Missing || len(dropped.Explanation.NotAdopted) == 0 {
		t.Fatalf("wrong tail must not be adopted: %+v", dropped)
	}
}

func TestKBSearchUncontrolledDocumentKeepsHitAndWarns(t *testing.T) {
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
		MediaType: "text/plain", ContentRef: "blob://kb/unc", SHA256: sha64("c"),
		SourceLocator: "mro://AMM/42?ata=32&status=uncontrolled",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{
				ChunkID:     ulid.Make().String(),
				Body:        "Uncontrolled gear isolation note.",
				LocatorJSON: `{"documentId":"` + doc.DocumentID + `","version":1,"ordinal":0,"docType":"AMM","revision":"42","status":"uncontrolled"}`,
			}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	hit, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: "isolation"})
	if err != nil {
		t.Fatal(err)
	}
	if hit.Explanation.Missing || len(hit.Hits) == 0 {
		t.Fatalf("uncontrolled hit must be kept: %+v", hit)
	}
	found := false
	for _, reason := range hit.Explanation.Reasons {
		if reason == "uncontrolled document" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reasons = %+v", hit.Explanation.Reasons)
	}
}

func TestGoldenNonEmptyCorpusGroundsWithMroLocators(t *testing.T) {
	items := loadGoldenP0(t)
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	expertID := ulid.Make().String()
	coll, err := svc.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	// Seed one mro:// document per non-empty golden query so FTS grounds the
	// exact phrase. docType/revision/ata/tail all ride in the source locator and
	// land in the chunk locator via parseSourceLocator.
	seed := func(name, body, locator, sha string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
			CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
			MediaType: "text/markdown", ContentRef: path, SHA256: sha64(sha),
			SourceLocator: locator, Projector: m8app.ParseBodyIndexer,
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed("gear.md", "ATA 32 起落架无法收上如何隔离 排故程序。", "mro://AMM/42?ata=32&status=controlled", "a")
	seed("mel.md", "MEL 对液压失效怎么说 保留项目与限制。", "mro://MEL/7?status=controlled", "b")
	seed("tail.md", "换机尾后原 AMM 是否仍适用 的适用性说明。", "mro://AMM/42?ata=32&tail=B-1234", "c")

	nonEmpty := 0
	for _, item := range items {
		if item.ExpectEmpty {
			continue
		}
		nonEmpty++
		item := item
		t.Run(item.Q, func(t *testing.T) {
			// A revision/effectivity item asks about a different tail: the stale
			// AMM must be dropped as not-adopted, never silently adopted.
			if item.ExpectNotAdopted != "" {
				got, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: item.Q, TailNo: "B-9999"})
				if err != nil {
					t.Fatal(err)
				}
				if !got.Explanation.Missing {
					t.Fatalf("wrong tail must not adopt: %+v", got)
				}
				dropped := false
				for _, reason := range got.Explanation.NotAdopted {
					if strings.Contains(reason, item.ExpectNotAdopted) {
						dropped = true
						break
					}
				}
				if !dropped {
					t.Fatalf("notAdopted = %+v want a %q reason", got.Explanation.NotAdopted, item.ExpectNotAdopted)
				}
				return
			}
			got, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: item.Q})
			if err != nil {
				t.Fatal(err)
			}
			if got.Explanation.Missing || len(got.Hits) == 0 {
				t.Fatalf("%q must ground against the seeded corpus: %+v", item.Q, got)
			}
			top := got.Hits[0]
			if item.ExpectDocType != "" {
				if dt := locatorField(t, top.Locator, "docType"); !strings.EqualFold(dt, item.ExpectDocType) {
					t.Fatalf("docType = %q want %q", dt, item.ExpectDocType)
				}
			}
			if item.ExpectContains != "" && !strings.Contains(top.Quote, item.ExpectContains) {
				t.Fatalf("quote %q must contain %q", top.Quote, item.ExpectContains)
			}
			// forbidBarePN: a grounded MRO hit must carry a revision cite so no
			// bare part number can be surfaced without a controlled source.
			if item.ForbidBarePN && strings.TrimSpace(top.Revision) == "" {
				t.Fatalf("grounded hit must carry a revision cite, got %+v", top)
			}
		})
	}
	if nonEmpty < 3 {
		t.Fatalf("expected at least three non-empty golden items to drive, ran %d", nonEmpty)
	}
}

func TestGoldenEmptyCorpusSearchMissing(t *testing.T) {
	items := loadGoldenP0(t)
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	expertID := ulid.Make().String()
	ran := 0
	for _, item := range items {
		if !item.ExpectEmpty {
			continue
		}
		ran++
		got, err := svc.Search(ctx, m8app.KBSearchInput{ExpertID: expertID, Query: item.Q})
		if err != nil {
			t.Fatal(err)
		}
		if !got.Explanation.Missing || len(got.Hits) != 0 {
			t.Fatalf("%q empty corpus must be missing: %+v", item.Q, got)
		}
	}
	if ran < 2 {
		t.Fatalf("expected at least two expectEmpty golden items, ran %d", ran)
	}
}

func TestListRecentKBAuditAfterUpsert(t *testing.T) {
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
		MediaType: "text/plain", ContentRef: "blob://kb/aud", SHA256: sha64("a"),
		SourceLocator: "mro://AMM/1",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{ChunkID: ulid.Make().String(), Body: "audit body"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ListRecentKBAudit(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Action == "kb.document.upsert" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("audit rows = %+v", rows)
	}
}

func TestCiteReturnsHitUnchanged(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	in := m8app.KBCitedHit{
		ExpertID: ulid.Make().String(), DocID: ulid.Make().String(),
		Revision: "42", Locator: `{"ordinal":0}`, Quote: "Gear retraction", Score: 1,
	}
	got, err := svc.Cite(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("cite mutated hit: %+v", got)
	}
}
