package officestudio

import (
	"strings"
	"testing"
)

func TestRuleVisualScoreIsUncalibratedAndCannotCertifyFormal(t *testing.T) {
	score := RuleVisualScore([]Check{{ID: "package", Status: "passed"}, {ID: "geometry_bounds", Status: "passed"}}, nil)
	if score < 0 || score > 100 {
		t.Fatalf("score out of range: %d", score)
	}
	report := EvaluateQuality([]Check{
		{ID: "native_render", Status: "missing", Message: "未检测到 LibreOffice"},
	}, score, nil)
	if report.FormalOK {
		t.Fatal("uncalibrated score must not clear blockers")
	}
	if !strings.Contains(report.Coverage, "visualScore=uncalibrated") {
		t.Fatalf("coverage=%q", report.Coverage)
	}
	if strings.Contains(report.Coverage, "certified") || strings.Contains(report.Coverage, "认证") {
		t.Fatalf("must not claim certification: %q", report.Coverage)
	}
}

func TestEvaluateQualityBlocksFormalWhenRendererMissingDespiteHighScore(t *testing.T) {
	report := EvaluateQuality([]Check{
		{ID: "native_render", Status: "missing", Message: "未检测到 LibreOffice"},
		{ID: "geometry_bounds", Status: "passed", Message: "画布内"},
	}, 92, nil)
	if report.FormalOK {
		t.Fatal("missing renderer must not be formal")
	}
	if report.Score >= 85 && len(report.Blockers) == 0 {
		t.Fatal("visual score must not cancel missing required check")
	}
}

func TestDiagnoseRenderBindsIssueToNode(t *testing.T) {
	checks := DiagnoseRender(false, []Issue{{
		Code: "OFFICE_FONT_MISSING", Severity: "error", NodeID: "node-3", Message: "缺字",
	}})
	if len(checks) == 0 || checks[0].Status == "passed" {
		t.Fatalf("unavailable renderer passed: %#v", checks)
	}
	found := false
	for _, c := range checks {
		if strings.Contains(c.Message, "node-3") {
			found = true
		}
	}
	if !found {
		t.Fatalf("node id not bound: %#v", checks)
	}
}

func TestBoundedRepairKeepsLockedFactText(t *testing.T) {
	spec := Spec{SchemaVersion: 2, Kind: PPTX, Title: "锁定", Slides: []Slide{{
		Title:  "指标",
		Layout: "metrics",
		Metrics: []MetricBlock{
			{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"},
		},
	}}}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	facts := []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}}
	out, report, err := BoundedRepair(data, facts)
	if err != nil {
		t.Fatal(err)
	}
	if report.RepairHistory == nil {
		t.Fatal("repair history required")
	}
	i, err := Inspect(PPTX, out)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
}
