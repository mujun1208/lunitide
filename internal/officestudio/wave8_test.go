package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

func TestDefaultBrandHasPurposeNamedColors(t *testing.T) {
	c := DefaultBrand().Colors
	if c["emphasis"] == "" || c["risk"] == "" || c["group"] == "" {
		t.Fatalf("purpose colors missing: %#v", c)
	}
	if c["risk"] == c["navy"] {
		t.Fatal("risk must not reuse navy")
	}
}

func TestApplyNarrativePlanFillsWordHeadingWithoutRewritingBody(t *testing.T) {
	spec := Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "研究报告",
		Blocks: []Block{
			{Type: "heading", Text: "范围"},
			{Type: "paragraph", Text: "订单 1280单"},
		},
	}
	plan, err := PlanNarrative(spec, Brief{Purpose: "研究报告"})
	if err != nil {
		t.Fatal(err)
	}
	out := ApplyNarrativePlan(spec, plan)
	if strings.TrimSpace(out.Blocks[0].Purpose) == "" {
		t.Fatalf("heading purpose empty: %#v", out.Blocks[0])
	}
	if out.Blocks[1].Text != "订单 1280单" {
		t.Fatalf("body rewritten: %#v", out.Blocks[1])
	}
	if out.Blocks[1].Purpose != "" {
		t.Fatal("body paragraph should not get a fabricated purpose")
	}
}

func TestPlanLayoutRecordsImageAspectAsContainNotStretch(t *testing.T) {
	plans, err := PlanLayout(NarrativeNode{
		Title:       "证据",
		Layout:      "content",
		Bullets:     []string{"现场照片"},
		ImageAspect: "4/3",
	}, DefaultBrand(), EstimateMeasure{})
	if err != nil || len(plans) == 0 {
		t.Fatal(err)
	}
	ev := plans[0].FitEvidence
	if !strings.Contains(ev, "4/3") || !strings.Contains(ev, "contain") {
		t.Fatalf("fitEvidence=%q", ev)
	}
	if strings.Contains(ev, "stretch") && !strings.Contains(ev, "not stretch") {
		t.Fatalf("claimed stretch: %q", ev)
	}
}

func TestBoundedRepairSeparatesOverlapWithoutRewritingFacts(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "重叠修复",
		Facts: []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
		Slides: []Slide{{
			Title:  "方案对比",
			Layout: "comparison",
			Comparison: &ComparisonBlock{
				Left:  []string{"方案A", "1280"},
				Right: []string{"方案B", "能力高"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	part := zipParts(t, data)["ppt/slides/slide1.xml"]
	item1 := offAfterName(t, part, "Item1")
	item2 := offAfterName(t, part, "Item2")
	overlapped := append(append(append([]byte(nil), part[:item2.start]...), item1.body...), part[item2.end:]...)
	data = editZIP(t, data, map[string][]byte{"ppt/slides/slide1.xml": overlapped})
	out, report, err := BoundedRepair(data, []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.RepairHistory) == 0 {
		t.Fatal("repair history required")
	}
	i, err := Inspect(PPTX, out)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
	findText(t, i, "方案A")
	findText(t, i, "能力高")
	if countIssue(i.Issues, "OFFICE_GEOMETRY_OVERLAP") >= countIssue(InspectMustPPTX(t, data).Issues, "OFFICE_GEOMETRY_OVERLAP") &&
		!strings.Contains(strings.Join(report.RepairHistory, " "), "overlap") {
		t.Fatalf("overlap not repaired or recorded: %#v history=%v", i.Issues, report.RepairHistory)
	}
}

func TestWorkbookDetailFilterAndDashboardHideGridlines(t *testing.T) {
	spec, err := PlanWorkbook("ops-ledger", "经营簿", []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	names := workbookSheetNames(parts)
	var detail, board []byte
	for part, name := range names {
		switch name {
		case "原始数据":
			detail = parts[part]
		case "看板":
			board = parts[part]
		}
	}
	if len(detail) == 0 || !bytes.Contains(detail, []byte("autoFilter")) {
		t.Fatalf("detail filter missing: %s", detail[:min(400, len(detail))])
	}
	if bytes.Contains(detail, []byte("mergeCell")) {
		t.Fatal("detail sheet must not merge cells")
	}
	if len(board) == 0 || !(bytes.Contains(board, []byte(`showGridLines="0"`)) || bytes.Contains(board, []byte(`showGridLines="false"`))) {
		t.Fatalf("dashboard gridlines: %s", board[:min(400, len(board))])
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
}

func InspectMustPPTX(t *testing.T, data []byte) Inspection {
	t.Helper()
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func countIssue(issues []Issue, code string) int {
	n := 0
	for _, issue := range issues {
		if issue.Code == code {
			n++
		}
	}
	return n
}
