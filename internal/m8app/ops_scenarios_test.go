package m8app_test

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestEnsureOpsExpertScenariosSeedsThreePerCardIdempotently(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	experts := m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{})
	scenarios := m8app.NewScenarioService(repo)
	ctx := context.Background()

	if err := m8app.EnsureBuiltinExperts(ctx, experts); err != nil {
		t.Fatal(err)
	}
	if err := m8app.EnsureOpsExpertScenarios(ctx, experts, scenarios); err != nil {
		t.Fatal(err)
	}
	if err := m8app.EnsureOpsExpertScenarios(ctx, experts, scenarios); err != nil {
		t.Fatalf("second seed: %v", err)
	}

	want := map[string][]string{
		"uas-airworthiness-expert": {"法规问答", "履历与触发器", "PIREP 草稿"},
		"tooling-chemical-expert":  {"工艺/SDS 问答", "校准与借还", "套件备妥"},
		"parts-expert":             {"库存与适用性", "AOG 草稿", "采购模板草稿"},
		"mx-planning-expert":       {"到期全景", "工作包组装", "间隔复审草案"},
	}
	listed, err := experts.List(ctx, m8app.ExpertFilter{State: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	for catalogID, titles := range want {
		expertID := ""
		for _, row := range listed.Experts {
			if row.CatalogItemID == catalogID {
				expertID = row.ExpertID
				break
			}
		}
		if expertID == "" {
			t.Fatalf("missing seeded expert %s", catalogID)
		}
		cards, err := scenarios.ListScenarios(ctx, expertID, "active")
		if err != nil {
			t.Fatal(err)
		}
		if len(cards) != 3 {
			t.Fatalf("%s scenarios = %d, want 3", catalogID, len(cards))
		}
		seen := map[string]int{}
		for _, card := range cards {
			seen[card.Title]++
		}
		for _, title := range titles {
			if seen[title] != 1 {
				t.Fatalf("%s %q count = %d, want 1", catalogID, title, seen[title])
			}
		}
	}
}

func TestEnsureOpsExpertScenariosNoopsWithoutExperts(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	experts := m8app.NewExpertService(repo, "local-user", &m8app.MemoryPersonaStore{})
	scenarios := m8app.NewScenarioService(repo)
	if err := m8app.EnsureOpsExpertScenarios(context.Background(), experts, scenarios); err != nil {
		t.Fatalf("missing experts must be no-op: %v", err)
	}
}
