package officeapp

import (
	"context"
	"encoding/json"
	"testing"

	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestLayoutTraceSurvivesGenerate(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	spec := content.Spec{
		SchemaVersion: 2,
		Kind:          content.PPTX,
		Title:         "经营汇报",
		TemplateID:    "ops-clear",
		Slides: []content.Slide{{
			Title:  "本月经营指标对照以及需要向管理层单独说明的口径变化",
			Layout: "metrics",
			Metrics: []content.MetricBlock{
				{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"},
				{Label: "退款率", Value: "2.4", Unit: "%", FactID: "refund"},
				{Label: "在途", Value: "86", Unit: "单", FactID: "transit"},
				{Label: "客诉", Value: "11", Unit: "件", FactID: "tickets"},
				{Label: "复购", Value: "19", Unit: "%", FactID: "repeat"},
				{Label: "新品", Value: "7", Unit: "个", FactID: "newsku"},
				{Label: "缺货", Value: "3", Unit: "SKU", FactID: "stockout"},
			},
		}},
	}
	v, err := svc.Generate(context.Background(), task.ID, "trace.pptx", spec, "layout-trace-survive")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetOfficeVersion(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	var persisted content.Spec
	if err = json.Unmarshal(stored.Spec, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Slides) == 0 || persisted.Slides[0].LayoutTrace.FitEvidence == "" {
		t.Fatalf("stored spec lost fitEvidence: %+v", persisted.Slides)
	}
	if persisted.Slides[0].LayoutTrace.ResolvedVariant == "" || persisted.Slides[0].LayoutTrace.Density == "" {
		t.Fatalf("stored spec lost variant/density: %+v", persisted.Slides[0].LayoutTrace)
	}
	if persisted.LayoutTrace.FitEvidence == "" {
		t.Fatal("spec-level LayoutTrace missing after Generate")
	}
}
