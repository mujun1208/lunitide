package officeapp

import (
	"context"
	"errors"
	"testing"

	content "github.com/lunitide/lunitide/internal/officestudio"
)

func ambiguousValueSpec() content.Spec {
	return content.Spec{
		SchemaVersion: 2, Kind: content.XLSX, Title: "口径",
		Sheets: []content.Sheet{{Name: "数据", FreezeHeader: true, Rows: [][]content.Cell{
			{{Type: "text", Value: "数值"}, {Type: "text", Value: "单位"}, {Type: "text", Value: "币种"}, {Type: "text", Value: "期间"}},
			{{Type: "number", Value: "100"}, {Type: "text", Value: "元"}, {Type: "text", Value: "CNY"}, {Type: "text", Value: "2026-01"}},
			{{Type: "number", Value: "1000"}, {Type: "text", Value: "元"}, {Type: "text", Value: "CNY"}, {Type: "text", Value: "2026-01"}},
			{{Type: "number", Value: "100"}, {Type: "text", Value: "元"}, {Type: "text", Value: "USD"}, {Type: "text", Value: "2026-01"}},
			{{Type: "number", Value: "100"}, {Type: "text", Value: "元"}, {Type: "text", Value: "CNY"}, {Type: "text", Value: "2026-02"}},
			{{Type: "formula", Value: "=SUM(A2:A2)"}, {Type: "text", Value: "元"}, {Type: "text", Value: "CNY"}, {Type: "text", Value: "2026-01"}},
		}}},
	}
}

func TestFactBindingRejectsAmbiguousValues(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v, err := svc.Generate(ctx, task.ID, "口径.xlsx", ambiguousValueSpec(), "bind-source")
	if err != nil {
		t.Fatal(err)
	}
	onlyThousand := content.Spec{
		SchemaVersion: 2, Kind: content.XLSX, Title: "仅千",
		Facts:  []content.Fact{{FactID: "amt", Value: "100", Unit: "元", Currency: "CNY", Period: "2026-01", Locked: true}},
		Sheets: []content.Sheet{{Name: "数据", Rows: [][]content.Cell{{{Type: "text", Value: "订单1000元"}}}}},
	}
	if _, err = svc.Generate(ctx, task.ID, "仅千.xlsx", onlyThousand, "thousand"); !errors.Is(err, content.ErrFactConflict) {
		t.Fatalf("100 must not bind 1000: %v", err)
	}

	if _, err = svc.CaptureMetricByFact(ctx, task.ID, v.ID, content.Fact{FactID: "amt", Value: "100"}, "bare-100"); err == nil {
		t.Fatal("bare 100 must not pick an ambiguous row")
	}
	if _, err = svc.CaptureMetricByFact(ctx, task.ID, v.ID, content.Fact{FactID: "amt", Value: "100", Currency: "USD", Period: "2026-02"}, "cross-axis"); err == nil {
		t.Fatal("same value with mismatched currency/period captured")
	}
	m, err := svc.CaptureMetricByFact(ctx, task.ID, v.ID, content.Fact{
		FactID: "amt", Value: "100", Unit: "元", Currency: "CNY", Period: "2026-01", Locator: "1月人民币",
	}, "cny-jan")
	if err != nil || m.RawValue != "100" || m.Currency != "CNY" || m.Period != "2026-01" {
		t.Fatalf("typed binding: %#v %v", m, err)
	}
	if _, err = store.GetOfficeMetric(ctx, m.ID); err != nil {
		t.Fatal(err)
	}

	formula := metricNode(t, svc, task.ID, v, "=SUM(A2:A2)")
	if _, err = svc.CaptureMetric(ctx, task.ID, MetricCapture{
		SourceVersionID: v.ID, SourceNodeID: formula.ID, SourceNodeDigest: formula.Digest, Name: "公式",
	}, "formula-no-receipt"); err == nil {
		t.Fatal("formula without recalculation receipt captured")
	}
	if _, err = svc.CaptureMetric(ctx, task.ID, MetricCapture{
		SourceVersionID: v.ID, SourceNodeID: formula.ID, SourceNodeDigest: formula.Digest, Name: "公式",
		Recalc: RecalcEvidence{SourceSHA: "deadbeef", FormulaDigest: formula.Digest, CachedValue: "100", OracleValue: "100"},
	}, "stale-receipt"); err == nil {
		t.Fatal("stale recalculation receipt captured")
	}
	if _, err = svc.CaptureMetric(ctx, task.ID, MetricCapture{
		SourceVersionID: v.ID, SourceNodeID: formula.ID, SourceNodeDigest: formula.Digest, Name: "公式",
		Recalc: RecalcEvidence{SourceSHA: v.SHA256, FormulaDigest: formula.Digest, CachedValue: "999", OracleValue: "100"},
	}, "cache-mismatch"); err == nil {
		t.Fatal("cached value disagreeing with oracle captured")
	}
	got, err := svc.CaptureMetric(ctx, task.ID, MetricCapture{
		SourceVersionID: v.ID, SourceNodeID: formula.ID, SourceNodeDigest: formula.Digest, Name: "已核验公式",
		Unit: "元", Currency: "CNY", Period: "2026-01",
		Recalc: RecalcEvidence{SourceSHA: v.SHA256, FormulaDigest: formula.Digest, CachedValue: "100", OracleValue: "100"},
	}, "formula-ok")
	if err != nil || got.RawValue != "100" || got.ValueType != "formula" {
		t.Fatalf("verified formula capture: %#v %v", got, err)
	}
}
