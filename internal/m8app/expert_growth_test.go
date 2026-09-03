package m8app_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestGrowthGetOrInitIdempotentAndRefreshCoverage(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	kb := m8app.NewKBService(repo, "local-user")
	growth := m8app.NewGrowthService(repo)
	ctx := context.Background()
	expertID := ulid.Make().String()

	first, err := growth.GetOrInit(ctx, expertID, "帮助机务同事检索受控手册。")
	if err != nil {
		t.Fatal(err)
	}
	if first.MissionSnapshot == "" || !strings.Contains(first.LadderJSON, "知识积累") {
		t.Fatalf("init path = %+v", first)
	}
	second, err := growth.GetOrInit(ctx, expertID, "changed mission must not overwrite")
	if err != nil || second.MissionSnapshot != first.MissionSnapshot || second.UpdatedAt != first.UpdatedAt {
		t.Fatalf("get-or-init not idempotent: %+v vs %+v err=%v", first, second, err)
	}

	coll, err := kb.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kb.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: ulid.Make().String(),
		MediaType: "text/plain", ContentRef: "blob://kb/amm", SHA256: sha64("a"),
		SourceLocator: "mro://AMM/42?ata=32",
		Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
			return []m8core.KBChunk{{
				ChunkID:     ulid.Make().String(),
				Body:        "Landing gear retraction isolation.",
				LocatorJSON: `{"documentId":"` + doc.DocumentID + `","version":1,"ordinal":0,"docType":"AMM"}`,
			}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	refreshed, err := growth.RefreshCoverage(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	var cov struct {
		DocTypes []string `json:"docTypes"`
		Gaps     []string `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(refreshed.CoverageJSON), &cov); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, dt := range cov.DocTypes {
		if dt == "AMM" {
			found = true
		}
	}
	if !found {
		t.Fatalf("coverage missing AMM: %s", refreshed.CoverageJSON)
	}
}

func TestEnsureExpertFoundationsSeedsColleagueNotMarket(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	experts := m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{})
	kb := m8app.NewKBService(repo, "local-user")
	growth := m8app.NewGrowthService(repo)
	ctx := context.Background()
	createExpert(t, experts, "产品经理")
	if err := m8app.EnsureBuiltinExperts(ctx, experts); err != nil {
		t.Fatal(err)
	}
	if err := m8app.EnsureExpertFoundations(ctx, experts, kb, growth); err != nil {
		t.Fatal(err)
	}
	listed, err := experts.List(ctx, m8app.ExpertFilter{})
	if err != nil {
		t.Fatal(err)
	}
	colleague := map[string]struct{}{"pm-advisor": {}}
	for _, item := range m8app.ConversationExperts() {
		colleague[item.Name] = struct{}{}
	}
	var marketID, pptID string
	for _, row := range listed.Experts {
		if row.Name == "产品经理" {
			marketID = row.ExpertID
		}
		if row.Name == "PPT专家" {
			pptID = row.ExpertID
		}
		if _, ok := colleague[row.Name]; !ok {
			continue
		}
		stats, err := kb.KnowledgeGet(ctx, row.ExpertID)
		if err != nil || stats.Missing || stats.CollectionID == "" {
			t.Fatalf("%s missing foundation: %+v %v", row.Name, stats, err)
		}
		path, ok, err := growth.Get(ctx, row.ExpertID)
		if err != nil || !ok || path.LadderJSON == "" {
			t.Fatalf("%s missing growth: %+v ok=%v err=%v", row.Name, path, ok, err)
		}
	}
	if pptID == "" || marketID == "" {
		t.Fatal("need PPT专家 and market 产品经理 rows")
	}
	market, err := kb.KnowledgeGet(ctx, marketID)
	if err != nil || !market.Missing || market.CollectionID != "" {
		t.Fatalf("market persona must not have a collection: %+v %v", market, err)
	}
}
