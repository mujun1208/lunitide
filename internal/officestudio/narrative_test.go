package officestudio

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanNarrativeFillsPurposeClaimEvidenceWithoutInventingFacts(t *testing.T) {
	spec := Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         "经营汇报",
		Facts:         []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
		Slides: []Slide{
			{Title: "封面", Layout: "cover", Subtitle: "八月对照"},
			{
				Title: "指标", Layout: "metrics",
				Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}},
				Evidence: []EvidenceItem{{Text: "台账 A2", FactID: "orders"}},
			},
		},
	}
	plan, err := PlanNarrative(spec, Brief{Purpose: "经营汇报", Audience: "管理层"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Fatalf("nodes=%d", len(plan.Nodes))
	}
	for _, n := range plan.Nodes {
		if strings.TrimSpace(n.Purpose) == "" || strings.TrimSpace(n.Claim) == "" {
			t.Fatalf("missing purpose/claim: %#v", n)
		}
		if n.NodeID == "" {
			t.Fatal("nodeId required")
		}
	}
	if !containsString(plan.Nodes[1].EvidenceRefs, "orders") {
		t.Fatalf("metric fact not linked: %#v", plan.Nodes[1].EvidenceRefs)
	}
	if strings.Contains(plan.FitEvidence, "节约") {
		t.Fatal("invented savings")
	}
}

func TestPlanNarrativeRejectsConflictFacts(t *testing.T) {
	_, err := PlanNarrative(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "冲突",
		Facts:  []Fact{{FactID: "orders", Value: "1280"}},
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "999", FactID: "orders"}}}},
	}, Brief{})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("conflict: %v", err)
	}
}

func TestPrepareManagedSpecWritesNarrativeOntoSlides(t *testing.T) {
	prepared, err := PrepareManagedSpec(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "经营汇报",
		Slides: []Slide{{
			Title: "指标", Layout: "metrics",
			Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(prepared.Slides[0].Purpose) == "" || strings.TrimSpace(prepared.Slides[0].Claim) == "" {
		t.Fatalf("narrative not applied: %#v", prepared.Slides[0])
	}
	if !containsString(prepared.Slides[0].EvidenceRefs, "orders") {
		t.Fatalf("evidence refs: %#v", prepared.Slides[0].EvidenceRefs)
	}
}

func TestKeepSlideNarrativeDoesNotDuplicateBulletAsSubtitle(t *testing.T) {
	got := keepSlideNarrative(Slide{Title: "收入", Claim: "初始状态", Bullets: []string{"初始状态"}})
	if got.Subtitle != "" {
		t.Fatalf("bullet promoted to subtitle: %#v", got)
	}
}

func TestApplyNarrativeOutlineDoesNotRewriteLockedClaim(t *testing.T) {
	spec := Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "经营汇报",
		Slides: []Slide{{
			Title: "指标", Layout: "metrics", Claim: "订单仍为 1280",
			Metrics: []MetricBlock{{Label: "订单", Value: "1280", FactID: "orders"}},
		}},
	}
	plan, err := PlanNarrative(spec, Brief{Outline: []NarrativeNode{{
		Title: "指标", Purpose: "指标概览", Claim: "不得覆盖已写结论",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyNarrativePlan(spec, plan)
	if out.Slides[0].Claim != "订单仍为 1280" {
		t.Fatalf("locked claim rewritten: %q", out.Slides[0].Claim)
	}
	if out.Slides[0].Purpose != "指标概览" {
		t.Fatalf("outline purpose not applied: %q", out.Slides[0].Purpose)
	}
}

