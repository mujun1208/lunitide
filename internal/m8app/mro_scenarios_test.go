package m8app_test

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestEnsureMROScenariosSeedsSixActiveCardsIdempotently(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	experts := m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{})
	scenarios := m8app.NewScenarioService(repo)
	ctx := context.Background()

	if err := m8app.EnsureBuiltinExperts(ctx, experts); err != nil {
		t.Fatal(err)
	}
	if err := m8app.EnsureMROScenarios(ctx, experts, scenarios); err != nil {
		t.Fatal(err)
	}
	if err := m8app.EnsureMROScenarios(ctx, experts, scenarios); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	mroID := mroExpertID(t, experts)
	cards, err := scenarios.ListScenarios(ctx, mroID, "active")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 6 {
		t.Fatalf("active MRO scenarios = %d, want 6", len(cards))
	}
	want := map[string]string{
		"手册问答": "RESEARCH_EVIDENCE",
		"排故诊断": "OPERATIONS_RETROSPECTIVE",
		"视觉识件": "RESEARCH_EVIDENCE",
		"预测维护": "OPERATIONS_RETROSPECTIVE",
		"合规工单": "OPERATIONS_RETROSPECTIVE",
		"培训教官": "RESEARCH_EVIDENCE",
	}
	seen := map[string]int{}
	for _, card := range cards {
		seen[card.Title]++
		if want[card.Title] != card.PhaseKey {
			t.Fatalf("%s phase = %q, want %q", card.Title, card.PhaseKey, want[card.Title])
		}
	}
	for title := range want {
		if seen[title] != 1 {
			t.Fatalf("%s count = %d, want 1", title, seen[title])
		}
	}
}

func TestEnsureMROScenariosNoopsWithoutMROExpert(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	experts := m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{})
	scenarios := m8app.NewScenarioService(repo)
	if err := m8app.EnsureMROScenarios(context.Background(), experts, scenarios); err != nil {
		t.Fatalf("missing expert must be no-op: %v", err)
	}
}

func mroExpertID(t *testing.T, svc *m8app.ExpertService) string {
	t.Helper()
	listed, err := svc.List(context.Background(), m8app.ExpertFilter{State: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed.Experts {
		if row.Name == "航空机务专家" || row.CatalogItemID == "mro-expert" {
			return row.ExpertID
		}
	}
	t.Fatal("航空机务专家 not seeded")
	return ""
}
