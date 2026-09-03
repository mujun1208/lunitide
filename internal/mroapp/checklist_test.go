package mroapp

import (
	"encoding/json"
	"testing"
)

func TestBuildChecklistJSONDropsUncitedSteps(t *testing.T) {
	raw := BuildChecklistJSON(
		[]string{"隔离液压", "更换件号 NAS1149"},
		[]CitationBlock{{Revision: "42", ATA: "32", Quote: "isolate hydraulic"}},
	)
	var got Checklist
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Banner != "辅助建议，不构成放行" {
		t.Fatalf("banner = %q", got.Banner)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("steps = %+v", got.Steps)
	}
	if got.Steps[0].N != 1 || got.Steps[0].Text != "隔离液压" || got.Steps[0].Revision != "42" || got.Steps[0].ATA != "32" {
		t.Fatalf("step = %+v", got.Steps[0])
	}
}

func TestBuildChecklistJSONKeepsExpertName(t *testing.T) {
	raw := BuildChecklistJSON(
		[]string{"按 AMM 检查作动筒"},
		[]CitationBlock{{Revision: "12", ATA: "29", ExpertName: "航空机务专家"}},
	)
	var got Checklist
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Steps) != 1 || got.Steps[0].ExpertName != "航空机务专家" {
		t.Fatalf("steps = %+v", got.Steps)
	}
}
