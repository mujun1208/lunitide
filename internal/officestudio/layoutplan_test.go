package officestudio

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlanLayoutSplitsLongChineseMetricsWithoutTruncating(t *testing.T) {
	node := NarrativeNode{
		NodeID:  "n1",
		Layout:  "metrics",
		Title:   "本月经营指标对照以及需要向管理层单独说明的口径变化",
		Purpose: "指标概览",
		Metrics: []MetricBlock{
			{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"},
			{Label: "退款率", Value: "2.4", Unit: "%", FactID: "refund"},
			{Label: "在途", Value: "86", Unit: "单", FactID: "transit"},
			{Label: "客诉", Value: "11", Unit: "件", FactID: "tickets"},
			{Label: "复购", Value: "19", Unit: "%", FactID: "repeat"},
			{Label: "新品", Value: "7", Unit: "个", FactID: "newsku"},
			{Label: "缺货", Value: "3", Unit: "SKU", FactID: "stockout"},
		},
	}
	plans, err := PlanLayout(node, DefaultBrand(), RuneMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) < 2 {
		t.Fatalf("7 metrics must split or change variant, got %d", len(plans))
	}
	seen := map[string]bool{}
	for _, p := range plans {
		if p.FitEvidence == "" {
			t.Fatal("fitEvidence required")
		}
		for _, m := range p.Metrics {
			if seen[m.FactID] {
				t.Fatalf("duplicate fact %s", m.FactID)
			}
			seen[m.FactID] = true
			if utf8.RuneCountInString(m.Value) != utf8.RuneCountInString(strings.TrimSpace(m.Value)) {
				t.Fatal("value altered")
			}
		}
		if strings.Contains(p.Title, "口径变") && !strings.Contains(p.Title, "口径变化") {
			t.Fatalf("title truncated: %q", p.Title)
		}
	}
	if !seen["stockout"] || !seen["orders"] {
		t.Fatalf("facts dropped: %#v", seen)
	}
}

func TestPlanLayoutSplitsTimelineAndLongBulletsWithoutTruncating(t *testing.T) {
	long := strings.Repeat("口径说明需要单独成段且不能被截成半句。", 16)
	if utf8.RuneCountInString(long) <= 300 {
		t.Fatalf("fixture too short: %d", utf8.RuneCountInString(long))
	}
	plans, err := PlanLayout(NarrativeNode{
		Title:   "时间线",
		Layout:  "timeline",
		Bullets: []string{"启动", "核对", "复盘", "发布", "回访", "归档", "复盘纪要"},
	}, DefaultBrand(), RuneMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) < 2 {
		t.Fatalf("7 timeline items must split, got %d", len(plans))
	}
	seen := map[string]bool{}
	for _, p := range plans {
		if p.FitEvidence == "" {
			t.Fatal("fitEvidence required")
		}
		for _, b := range p.Bullets {
			seen[b] = true
		}
	}
	if !seen["启动"] || !seen["复盘纪要"] {
		t.Fatalf("timeline items dropped: %#v", seen)
	}
	split, err := PlanLayout(NarrativeNode{
		Title:   "说明",
		Layout:  "content",
		Bullets: []string{long},
	}, DefaultBrand(), RuneMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, p := range split {
		joined += strings.Join(p.Bullets, "")
		for _, b := range p.Bullets {
			if utf8.RuneCountInString(b) > 300 {
				t.Fatalf("bullet still over limit: %d", utf8.RuneCountInString(b))
			}
		}
	}
	if !strings.Contains(joined, "不能被截成半句") || strings.Contains(joined, "不能被截成半") && !strings.Contains(joined, "不能被截成半句") {
		t.Fatalf("long bullet truncated: %q", joined)
	}
	if utf8.RuneCountInString(joined) < utf8.RuneCountInString(long) {
		t.Fatalf("lost runes: got %d want %d", utf8.RuneCountInString(joined), utf8.RuneCountInString(long))
	}
}

func TestPlanLayoutKeepsBulletsWhenMetricsOverflow(t *testing.T) {
	plans, err := PlanLayout(NarrativeNode{
		Title:   "指标与口径",
		Layout:  "metrics",
		Bullets: []string{"口径说明保留", "不得丢条目"},
		Metrics: []MetricBlock{
			{Label: "订单", Value: "1280", FactID: "orders"},
			{Label: "退款", Value: "2.4", FactID: "refund"},
			{Label: "在途", Value: "86", FactID: "transit"},
			{Label: "客诉", Value: "11", FactID: "tickets"},
			{Label: "复购", Value: "19", FactID: "repeat"},
			{Label: "新品", Value: "7", FactID: "newsku"},
			{Label: "缺货", Value: "3", FactID: "stockout"},
		},
	}, DefaultBrand(), RuneMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	seenFact, seenBullet := map[string]bool{}, map[string]bool{}
	for _, p := range plans {
		for _, m := range p.Metrics {
			seenFact[m.FactID] = true
		}
		for _, b := range p.Bullets {
			seenBullet[b] = true
		}
	}
	if !seenFact["orders"] || !seenFact["stockout"] {
		t.Fatalf("metrics dropped: %#v", seenFact)
	}
	if !seenBullet["口径说明保留"] || !seenBullet["不得丢条目"] {
		t.Fatalf("bullets dropped on metric overflow: %#v plans=%d", seenBullet, len(plans))
	}
}

func TestEngineeredVariantsAre36AndUnreviewed(t *testing.T) {
	variants := EngineeredVariants()
	if len(variants) != 36 {
		t.Fatalf("want 36 engineered variants, got %d", len(variants))
	}
	seen := map[string]bool{}
	for _, v := range variants {
		if seen[v.VariantID] {
			t.Fatalf("duplicate variant %s", v.VariantID)
		}
		seen[v.VariantID] = true
		parts := strings.Split(v.VariantID, "/")
		if len(parts) != 2 || v.System != parts[0] || v.Layout != parts[1] {
			t.Fatalf("variant id %q %#v", v.VariantID, v)
		}
		if v.DesignerReviewed {
			t.Fatalf("must not mark designer reviewed: %#v", v)
		}
		if v.MaxItems < 1 {
			t.Fatalf("capacity missing: %#v", v)
		}
	}
	if !seen["ops-clear/metrics"] || !seen["brand-pitch/cover"] || !seen["editorial-report/timeline"] {
		t.Fatalf("missing expected variants: %v", seen)
	}
	cov := TemplateVariantCoverage()
	if !strings.Contains(cov, "templateVariants=36 engineered") || !strings.Contains(cov, "designerReviewed=0") {
		t.Fatalf("coverage=%q", cov)
	}
	if strings.Contains(strings.ToLower(cov), "designer-checked") || strings.Contains(cov, "designerReviewed=36") {
		t.Fatal("must not claim designer-reviewed variants")
	}
	report := EvaluateQuality(nil, 0, nil)
	if !strings.Contains(report.Coverage, "designerReviewed=0") {
		t.Fatalf("quality coverage missing variant honesty: %q", report.Coverage)
	}
}

func TestPlanLayoutPicksDensityCapacityWithoutTruncating(t *testing.T) {
	metrics := []MetricBlock{
		{Label: "订单", Value: "1280", FactID: "orders"},
		{Label: "退款", Value: "2.4", FactID: "refund"},
		{Label: "在途", Value: "86", FactID: "transit"},
		{Label: "客诉", Value: "11", FactID: "tickets"},
	}
	plans, err := PlanLayout(NarrativeNode{
		Title: "指标", Layout: "metrics", Density: "airy", Metrics: metrics,
	}, DefaultBrand(), RuneMeasure{}, "ops-clear")
	if err != nil || len(plans) < 2 {
		t.Fatalf("airy metrics must split: %d %v", len(plans), err)
	}
	if plans[0].VariantID != "ops-clear/metrics/airy" {
		t.Fatalf("variant=%s", plans[0].VariantID)
	}
	seen := map[string]bool{}
	for _, p := range plans {
		if p.VariantID != "ops-clear/metrics/airy" {
			t.Fatalf("density changed: %s", p.VariantID)
		}
		for _, m := range p.Metrics {
			seen[m.FactID] = true
		}
	}
	if !seen["orders"] || !seen["tickets"] {
		t.Fatalf("airy split dropped facts: %#v", seen)
	}
}

func TestTemplateCatalogHasThreeStarterSystems(t *testing.T) {
	ids := TemplateIDs()
	for _, want := range []string{"ops-clear", "brand-pitch", "editorial-report"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing template %s in %v", want, ids)
		}
	}
}

func TestPlanLayoutUsesTemplateManifestCapacity(t *testing.T) {
	manifest, ok := LoadTemplate("ops-clear")
	if !ok || manifest.Constraints.MaxMetrics != 6 || len(manifest.Slots) == 0 {
		t.Fatalf("ops-clear manifest unused: %#v %v", manifest, ok)
	}
	node := NarrativeNode{Title: "指标", Layout: "metrics", Metrics: make([]MetricBlock, 7)}
	for i := range node.Metrics {
		node.Metrics[i] = MetricBlock{Label: "项", Value: fmt.Sprintf("%d", i+1), FactID: fmt.Sprintf("f%d", i)}
	}
	plans, err := PlanLayout(node, DefaultBrand(), RuneMeasure{}, "ops-clear")
	if err != nil || len(plans) < 2 {
		t.Fatalf("manifest capacity not applied: %d %v", len(plans), err)
	}
	if !strings.Contains(plans[0].VariantID, "ops-clear") {
		t.Fatalf("variant missing template: %s", plans[0].VariantID)
	}
}

func TestTemplateSupportsKindSeparatesFamilies(t *testing.T) {
	if !TemplateSupportsKind("brand-pitch", PPTX) || TemplateSupportsKind("brand-pitch", DOCX) || TemplateSupportsKind("brand-pitch", XLSX) {
		t.Fatal("brand-pitch must be pptx-only")
	}
	if !TemplateSupportsKind("research-report", DOCX) || TemplateSupportsKind("research-report", PPTX) {
		t.Fatal("research-report must be docx-only")
	}
	if !TemplateSupportsKind("ops-ledger", XLSX) || TemplateSupportsKind("ops-ledger", PPTX) {
		t.Fatal("ops-ledger must be xlsx-only")
	}
}
