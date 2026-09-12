package officestudio

import (
	"testing"
)

func TestGenerateFromLayoutPlanKeepsEditableMetricsAndComparison(t *testing.T) {
	plans, err := PlanLayout(NarrativeNode{
		Title:  "方案对比",
		Layout: "comparison",
		Comparison: &ComparisonBlock{
			Left:  []string{"方案A", "周期短"},
			Right: []string{"方案B", "覆盖广"},
		},
	}, DefaultBrand(), RuneMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	metricPlans, err := PlanLayout(NarrativeNode{
		Title:  "趋势",
		Layout: "trend",
		Metrics: []MetricBlock{
			{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"},
		},
	}, DefaultBrand(), RuneMeasure{})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := SpecFromLayoutPlans("汇报", append(plans, metricPlans...))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	if findText(t, i, "方案A").ID == "" || findText(t, i, "1280").ID == "" {
		t.Fatal("native text missing")
	}
	n := findText(t, i, "方案A")
	if !n.Editable {
		t.Fatal("comparison text must stay editable")
	}
}
