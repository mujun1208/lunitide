package modelquality

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/officestudio"
)

var fixedEvalIDs = []string{
	"D01", "D02", "D03", "P01", "P02", "P03",
	"X01", "X02", "X03", "F01", "F02", "F03",
	"R01", "R02", "R03", "R04", "L01", "L02", "L03", "L04",
	"C01", "C02", "C03", "C04",
}

func TestEvalSuiteAllCasesHaveIndependentOracle(t *testing.T) {
	suite, err := LoadEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	ids := suite.CaseIDs()
	if got := strings.Join(ids, " "); got != strings.Join(fixedEvalIDs, " ") {
		t.Fatalf("case list: %s", got)
	}
	if suite.IsEvalComplete() || suite.IsLiveQualified() {
		t.Fatal("printing the case list is not eval complete or live qualified")
	}

	hold := HoldOutListed()
	if len(hold) == 0 {
		t.Fatal("hold-out must be listed")
	}
	fixed := map[string]bool{}
	for _, id := range fixedEvalIDs {
		fixed[id] = true
	}
	for _, id := range hold {
		if fixed[id] {
			t.Fatalf("hold-out %s overlaps the 24", id)
		}
		h := suite.HoldOutCase(id)
		if h.CaseID == "" || h.InputDigest == "" || h.Budget.MaxTotalTokens == 0 {
			t.Fatalf("hold-out %s missing structure: %+v", id, h)
		}
		if !suite.HasIndependentOracle(id) {
			t.Fatalf("hold-out %s has no independent oracle", id)
		}
		if h.Timezone == "" && h.Title == "" && h.RowCount == 0 {
			t.Fatalf("hold-out %s must change numbers/titles/rows/timezone", id)
		}
	}
	d01 := suite.MustCase("D01")
	hd01 := suite.HoldOutCase("H-D01")
	if hd01.Title == "" || hd01.Title == d01.Title {
		t.Fatalf("hold-out title must differ from D01: %q", hd01.Title)
	}
	if hd01.Timezone == "" || hd01.Timezone == "Asia/Shanghai" {
		t.Fatalf("hold-out timezone must differ: %q", hd01.Timezone)
	}
	if factValue(d01.Inputs, "revenue_q1") == factValue(hd01.Inputs, "revenue_q1") {
		t.Fatal("hold-out numbers must differ from D01")
	}
	if suite.MustCase("D02").RowCount() == suite.HoldOutCase("H-D02").RowCount {
		t.Fatal("hold-out row count must differ from D02")
	}

	for _, id := range fixedEvalIDs {
		c := suite.MustCase(id)
		if c.OracleLabel == "deterministic-host-v1" && !suite.HasIndependentOracle(id) {
			t.Fatalf("%s string-only oracle %q is not an oracle", id, c.OracleLabel)
		}
		if !suite.HasIndependentOracle(id) {
			t.Fatalf("%s missing independent oracle", id)
		}
		if c.Budget.MaxTotalTokens == 0 || c.Budget.MaxModelAttempts == 0 {
			t.Fatalf("%s missing budget", id)
		}
		if c.InputDigest == "" || c.InputDigest == c.OracleLabel {
			t.Fatalf("%s missing input digest", id)
		}
		obs := hostObservation(t, suite, id, defaultScenario(c))
		v := suite.Evaluate(id, defaultScenario(c), obs)
		if !v.Pass {
			t.Fatalf("%s host oracle: %v", id, v.Reasons)
		}
	}

	t.Run("x01_x02_x03_reuse_t15", func(t *testing.T) {
		x01 := x01HostArtifact(t, suite)
		insp, err := officestudio.Inspect(officestudio.XLSX, x01)
		if err != nil {
			t.Fatal(err)
		}
		cents, err := officestudio.IndependentIntegerCents(insp)
		if err != nil || cents != 2195700 {
			t.Fatalf("X01 IndependentIntegerCents=%d %v", cents, err)
		}
		if v := suite.Evaluate("X01", "default", Observation{Artifact: x01}); !v.Pass {
			t.Fatalf("X01: %v", v.Reasons)
		}

		x02, err := officestudio.X02SalesSpec()
		if err != nil {
			t.Fatal(err)
		}
		x02Data, err := officestudio.Generate(x02)
		if err != nil {
			t.Fatal(err)
		}
		x02Insp, err := officestudio.Inspect(officestudio.XLSX, x02Data)
		if err != nil {
			t.Fatal(err)
		}
		g, err := officestudio.IndependentGrowth(x02Insp)
		if err != nil || g != "0.25" {
			t.Fatalf("X02 growth %q %v", g, err)
		}
		a, err := officestudio.IndependentAttainment(x02Insp)
		if err != nil || a != "0.9375" {
			t.Fatalf("X02 attainment %q %v", a, err)
		}
		z, err := officestudio.IndependentZeroDenominator(x02Insp)
		if err != nil || z != "n/a" {
			t.Fatalf("X02 zero %q %v", z, err)
		}
		if v := suite.Evaluate("X02", "default", Observation{Artifact: x02Data}); !v.Pass {
			t.Fatalf("X02: %v", v.Reasons)
		}

		x03, err := officestudio.X03SumChartSpec()
		if err != nil {
			t.Fatal(err)
		}
		x03Data, err := officestudio.Generate(x03)
		if err != nil {
			t.Fatal(err)
		}
		sum, err := officestudio.IndependentRangeSum(mustInspect(t, officestudio.XLSX, x03Data), "B2:B5")
		if err != nil || sum != 100 {
			t.Fatalf("pre-patch sum %d %v", sum, err)
		}
		partDigest := sheetDigest(t, x03Data)
		base := mustInspect(t, officestudio.XLSX, x03Data)
		patched, err := officestudio.Patch(x03Data, officestudio.PatchRequest{
			Kind: officestudio.XLSX, BaseSHA256: base.SHA256,
			Ranges: []officestudio.RangePatch{{
				Part: "xl/worksheets/sheet1.xml", Range: "B2:B5", ExpectedDigest: partDigest,
				Rows: [][]officestudio.Cell{{{Type: "number", Value: "11"}}, {{Type: "number", Value: "22"}}, {{Type: "number", Value: "33"}}, {{Type: "number", Value: "44"}}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := officestudio.IndependentRangeSum(patched.Inspection, "B2:B5")
		if err != nil || got != 110 {
			t.Fatalf("X03 IndependentRangeSum=%d %v", got, err)
		}
		if v := suite.Evaluate("X03", "default", Observation{Artifact: patched.Data}); !v.Pass {
			t.Fatalf("X03: %v", v.Reasons)
		}
	})

	t.Run("mutate_input_or_cache_fails_oracle", func(t *testing.T) {
		x01 := x01HostArtifact(t, suite)
		if v := suite.Evaluate("X01", "default", Observation{Artifact: x01}); !v.Pass {
			t.Fatalf("clean X01: %v", v.Reasons)
		}
		wrongCache := injectX01Cache(t, x01, "1")
		if v := suite.Evaluate("X01", "default", Observation{Artifact: wrongCache}); v.Pass {
			t.Fatal("wrong cached total must fail the X01 oracle")
		}
		mutated := mutateX01FirstAmount(t, x01)
		if v := suite.Evaluate("X01", "default", Observation{Artifact: mutated}); v.Pass {
			t.Fatal("changing one X01 amount must fail IndependentIntegerCents vs 2195700")
		}

		d01 := hostObservation(t, suite, "D01", "default")
		if v := suite.Evaluate("D01", "default", d01); !v.Pass {
			t.Fatalf("clean D01: %v", v.Reasons)
		}
		d01.Text = strings.ReplaceAll(d01.Text, "1200000", "9999999")
		d01.Artifact = nil
		if v := suite.Evaluate("D01", "default", d01); v.Pass {
			t.Fatal("changing one D01 fact must fail the oracle")
		}
	})

	t.Run("fixture_and_fault_do_not_count_live", func(t *testing.T) {
		acc := Accounting{}
		suite.Record(&acc, Outcome{CaseID: "R02", ScenarioID: "default", FixtureOnly: true, Mode: "fixture", OraclePass: true})
		suite.Record(&acc, Outcome{CaseID: "R04", ScenarioID: "default", FixtureOnly: true, Mode: "fixture", OraclePass: true})
		suite.Record(&acc, Outcome{CaseID: "L02", ScenarioID: "default", FixtureOnly: true, Mode: "fixture", OraclePass: true})
		suite.Record(&acc, Outcome{CaseID: "L03", ScenarioID: "default", FixtureOnly: true, Mode: "fixture", OraclePass: true})
		suite.Record(&acc, Outcome{CaseID: "L04", ScenarioID: "default", FixtureOnly: true, Mode: "fixture", OraclePass: true})
		if acc.LiveSuccess != 0 {
			t.Fatalf("fixtureOnly incremented live success: %+v", acc)
		}
		if acc.FixturePass != 5 {
			t.Fatalf("fixture passes: %+v", acc)
		}
		f03 := hostObservation(t, suite, "F03", "missing-glyph")
		v := suite.Evaluate("F03", "missing-glyph", f03)
		if !v.Pass {
			t.Fatalf("F03 missing-glyph oracle (fault detected): %v", v.Reasons)
		}
		if v.LiveSuccessIncrement {
			t.Fatal("F03 missing-glyph must not increment live success")
		}
		suite.Record(&acc, Outcome{CaseID: "F03", ScenarioID: "missing-glyph", Mode: "fixture", FaultScenario: true, OraclePass: true})
		if acc.LiveSuccess != 0 {
			t.Fatalf("F03 missing-glyph counted live: %+v", acc)
		}
		suite.Record(&acc, Outcome{CaseID: "D01", ScenarioID: "default", Mode: "live", OraclePass: true, EvidenceKind: "host"})
		if acc.LiveQualified || acc.EvalComplete || acc.HoldOutComplete {
			t.Fatalf("host/fixture pass marked complete: %+v", acc)
		}
		if acc.LiveSuccess != 0 {
			t.Fatalf("host evidence must not mint live success: %+v", acc)
		}
		for _, id := range hold {
			suite.Record(&acc, Outcome{CaseID: id, HoldOut: true, OraclePass: true})
		}
		if acc.HoldOutComplete || acc.EvalComplete || acc.LiveQualified {
			t.Fatalf("hold-out listed is not complete: %+v", acc)
		}

		omitted := Accounting{}
		suite.Record(&omitted, Outcome{CaseID: "R02", ScenarioID: "default", OraclePass: true, EvidenceKind: "live"})
		if omitted.LiveSuccess != 0 {
			t.Fatalf("R02 without caller FixtureOnly incremented live: %+v", omitted)
		}
		if omitted.FixturePass != 1 {
			t.Fatalf("R02 must count fixture from testdata: %+v", omitted)
		}
		suite.Record(&omitted, Outcome{CaseID: "F03", ScenarioID: "missing-glyph", OraclePass: true, EvidenceKind: "live"})
		if omitted.LiveSuccess != 0 {
			t.Fatalf("F03 missing-glyph without FaultScenario incremented live: %+v", omitted)
		}
		if omitted.FaultContractPass != 1 {
			t.Fatalf("F03 missing-glyph must count fault from testdata: %+v", omitted)
		}
		suite.Record(&omitted, Outcome{CaseID: "F03", OraclePass: true, EvidenceKind: "live"})
		if omitted.LiveSuccess != 0 {
			t.Fatalf("F03 with omitted scenario must not increment live: %+v", omitted)
		}
		for _, id := range hold {
			suite.Record(&omitted, Outcome{CaseID: id, OraclePass: true, EvidenceKind: "live"})
		}
		if omitted.HoldOutSeen != len(hold) || omitted.HoldOutComplete || omitted.LiveSuccess != 0 {
			t.Fatalf("hold-out without HoldOut flag: %+v", omitted)
		}
	})

	t.Run("c02_binds_fixture_ids_and_rules", func(t *testing.T) {
		if v := suite.Evaluate("C02", "default", Observation{Text: c02CandidateCSV()}); !v.Pass {
			t.Fatalf("fixture C02: %v", v.Reasons)
		}
		if v := suite.Evaluate("C02", "default", Observation{Text: c02WrongIDSetCSV()}); v.Pass {
			t.Fatal("80 unique 6-digit IDs outside 000001-000080 must fail")
		}
		if v := suite.Evaluate("C02", "default", Observation{Text: c02NoLeadingZeroCSV()}); v.Pass {
			t.Fatal("IDs without a leading zero must fail")
		}
		if v := suite.Evaluate("C02", "default", Observation{Text: c02StaleLatestCSV()}); v.Pass {
			t.Fatal("keeping the earlier duplicate row must fail")
		}
		if v := suite.Evaluate("C02", "default", Observation{Text: c02FilledEmptyDateCSV()}); v.Pass {
			t.Fatal("filling empty date on ID 000038 must fail")
		}
	})
}

func defaultScenario(c EvalCase) string {
	if c.CaseID == "F03" {
		return "supported-font"
	}
	if c.CaseID == "R01" {
		return "fixture-exact-values"
	}
	if c.CaseID == "C04" {
		return "all-formats-success"
	}
	return "default"
}

func hostObservation(t *testing.T, suite *Suite, id, scenario string) Observation {
	t.Helper()
	c := suite.MustCase(id)
	switch id {
	case "D01":
		spec, err := officestudio.WordDeliverySpec("editorial", "中文研究报告", factsFromInputs(c.Inputs))
		if err != nil {
			t.Fatal(err)
		}
		data, err := officestudio.Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		insp := mustInspect(t, officestudio.DOCX, data)
		return Observation{Artifact: data, Text: inspectionText(insp), Kind: officestudio.DOCX}
	case "D02":
		rows := [][]string{{"编号", "标题", "说明"}}
		var in struct {
			Rows []struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(c.Inputs, &in); err != nil {
			t.Fatal(err)
		}
		for _, r := range in.Rows {
			rows = append(rows, []string{fmt.Sprintf("%d", r.ID), r.Title, r.Description})
		}
		spec, err := officestudio.WordLongTableSpec("客户方案长表", rows)
		if err != nil {
			t.Fatal(err)
		}
		data, err := officestudio.Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		return Observation{Artifact: data, Kind: officestudio.DOCX, Text: inspectionText(mustInspect(t, officestudio.DOCX, data))}
	case "D03":
		return Observation{Values: map[string]string{
			"originalSentence": "计划收入120万元", "replacementSentence": "计划收入150万元",
			"cellFrom": "1200000", "cellTo": "1500000", "onlyTargetChanged": "true",
			"oldVersionImmutable": "true", "newPdfBound": "true",
		}}
	case "P01":
		facts := factsFromInputs(c.Inputs)
		slides := make([]officestudio.Slide, 8)
		for i := range slides {
			slides[i] = officestudio.Slide{Title: fmt.Sprintf("页%d", i+1), Layout: "content", Bullets: factBullets(facts)}
		}
		slides[0].Charts = []officestudio.SlideChart{{Type: "column", Title: "收入", Categories: []string{"Q1", "Q2"}, Series: []officestudio.ChartSeries{{Name: "收入", Values: []string{"1200000", "1500000"}}}, X: 900000, Y: 1800000, Width: 8500000, Height: 4000000, Legend: true}}
		slides[1].Charts = []officestudio.SlideChart{{Type: "column", Title: "成本", Categories: []string{"Q1", "Q2"}, Series: []officestudio.ChartSeries{{Name: "成本", Values: []string{"720000", "870000"}}}, X: 900000, Y: 1800000, Width: 8500000, Height: 4000000, Legend: true}}
		data, err := officestudio.Generate(officestudio.Spec{SchemaVersion: 2, Kind: officestudio.PPTX, Title: "经营复盘", Slides: slides})
		if err != nil {
			t.Fatal(err)
		}
		return Observation{Artifact: data, Kind: officestudio.PPTX, Text: inspectionText(mustInspect(t, officestudio.PPTX, data)), Values: map[string]string{"slides": "8", "charts": "2"}}
	case "P02":
		return Observation{Values: map[string]string{
			"title":       "这是一个需要自动换行同时保持清晰阅读层级的企业数字化转型方案对比标题示例",
			"comparisons": "12", "minBodyPt": "20", "outOfBounds": "0", "illegalOverlap": "0",
		}, Text: p02ComparisonText()}
	case "P03":
		return Observation{Values: map[string]string{"chart": "100,250,300", "imageBoxEqual": "true", "unrelatedEqual": "true"}}
	case "X01":
		return Observation{Artifact: x01HostArtifact(t, suite)}
	case "X02":
		spec, err := officestudio.X02SalesSpec()
		if err != nil {
			t.Fatal(err)
		}
		data, err := officestudio.Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		return Observation{Artifact: data}
	case "X03":
		spec, err := officestudio.X03SumChartSpec()
		if err != nil {
			t.Fatal(err)
		}
		data, err := officestudio.Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		insp := mustInspect(t, officestudio.XLSX, data)
		patched, err := officestudio.Patch(data, officestudio.PatchRequest{
			Kind: officestudio.XLSX, BaseSHA256: insp.SHA256,
			Ranges: []officestudio.RangePatch{{
				Part: "xl/worksheets/sheet1.xml", Range: "B2:B5", ExpectedDigest: sheetDigest(t, data),
				Rows: [][]officestudio.Cell{{{Type: "number", Value: "11"}}, {{Type: "number", Value: "22"}}, {{Type: "number", Value: "33"}}, {{Type: "number", Value: "44"}}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return Observation{Artifact: patched.Data}
	case "F01":
		return Observation{Values: map[string]string{
			"oldSha": "sha-old", "newSha": "sha-new", "oldPdfBound": "sha-old", "updatedText": "季度收入150万元",
		}, Text: "季度收入150万元"}
	case "F02":
		return Observation{Values: map[string]string{
			"pagesParsed": "3", "sources": "30", "tableRows": "90", "searchableChinese": "true", "backend": "docx-same-source",
		}}
	case "F03":
		if scenario == "missing-glyph" {
			return Observation{Values: map[string]string{
				"fontFixture": "known-missing-glyph", "missingGlyphDetected": "true", "formalBlocked": "true",
				"silentLoss": "false",
			}, ScenarioID: "missing-glyph"}
		}
		return Observation{Values: map[string]string{
			"fontFixture": "verified-full-glyph-coverage", "allGlyphsVisible": "true", "pdfTextExact": "true",
			"formalVerified": "true",
		}, Text: "人民币￥100；达成率％；长度μm；温度℃；生僻字𠮷。", ScenarioID: "supported-font"}
	case "R01":
		return Observation{Values: map[string]string{
			"observedExact": "true", "sequenceComplete": "true", "whitespacePreserved": "true", "largeReasoning": "300000",
		}}
	case "R02":
		return Observation{Values: map[string]string{"completeCallOnce": "true", "incompleteExecuted": "0", "usageUnknown": "true"}}
	case "R03":
		return Observation{Values: map[string]string{"privateIsolated": "true", "factsRetained": "true", "paramsLegal": "true", "rebuildReason": "provider_switch"}}
	case "R04":
		return Observation{Values: map[string]string{"qualificationInvalidated": "true", "unknownFabricated": "false", "inflightPinned": "true", "newActivationNeedsQual": "true"}}
	case "L01":
		return Observation{Values: map[string]string{"itemsCovered": "40", "preflightEveryAttempt": "true", "brokenToolGroup": "false"}}
	case "L02":
		return Observation{Values: map[string]string{"taskVerified": "false", "missingStep": "write", "modelL0Ignored": "true"}}
	case "L03":
		return Observation{Values: map[string]string{"knownEffectCount": "1", "unknownReconciled": "true", "sameBudget": "true"}}
	case "L04":
		return Observation{Values: map[string]string{"admitted700": "1", "doubleCharge": "false", "followupAfterCancel": "false"}}
	case "C01":
		return Observation{Values: map[string]string{
			"2026-01-31T12:00:00Z": IndependentNextDay(mustTime(t, "2026-01-31T12:00:00Z")).UTC().Format(time.RFC3339),
			"2024-02-28T12:00:00Z": IndependentNextDay(mustTime(t, "2024-02-28T12:00:00Z")).UTC().Format(time.RFC3339),
			"testFilesUnchanged":   "true",
		}}
	case "C02":
		return Observation{Text: c02CandidateCSV(), Values: map[string]string{"uniqueRows": "80"}}
	case "C03":
		return Observation{Text: "month,region,amountCents\n2026-05,east,30000\n2026-06,west,15000\n"}
	case "C04":
		facts := factsFromInputs(c.Inputs)
		blob := factBlob(facts)
		return Observation{Values: map[string]string{
			"docx": blob, "pptx": blob, "xlsx": blob, "pdf": blob, "formats": "4", "bundleVerified": "true",
		}}
	default:
		t.Fatalf("no host observation for %s", id)
	}
	return Observation{}
}

func x01HostArtifact(t *testing.T, suite *Suite) []byte {
	t.Helper()
	c := suite.MustCase("X01")
	var in struct {
		Orders []struct {
			OrderID             string  `json:"orderId"`
			Date                string  `json:"date"`
			AmountCents         int64   `json:"amountCents"`
			DiscountBasisPoints int     `json:"discountBasisPoints"`
			Region              string  `json:"region"`
			Note                *string `json:"note"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(c.Inputs, &in); err != nil {
		t.Fatal(err)
	}
	orders := make([]officestudio.OrderLine, 0, len(in.Orders))
	for _, o := range in.Orders {
		note := ""
		if o.Note != nil {
			note = *o.Note
		}
		orders = append(orders, officestudio.OrderLine{
			OrderID: o.OrderID, Date: o.Date, AmountCents: o.AmountCents,
			DiscountBasisPoints: o.DiscountBasisPoints, Region: o.Region, Note: note,
		})
	}
	spec, err := officestudio.X01OrdersSpec(orders)
	if err != nil {
		t.Fatal(err)
	}
	data, err := officestudio.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func injectX01Cache(t *testing.T, data []byte, wrong string) []byte {
	t.Helper()
	insp := mustInspect(t, officestudio.XLSX, data)
	_ = insp
	return injectCachedFormulaValue(t, data, wrong)
}

func mutateX01FirstAmount(t *testing.T, data []byte) []byte {
	t.Helper()
	insp := mustInspect(t, officestudio.XLSX, data)
	partDigest := ""
	for _, p := range insp.Parts {
		if p.Name == "xl/worksheets/sheet1.xml" {
			partDigest = p.SHA256
		}
	}
	patched, err := officestudio.Patch(data, officestudio.PatchRequest{
		Kind: officestudio.XLSX, BaseSHA256: insp.SHA256,
		Ranges: []officestudio.RangePatch{{
			Part: "xl/worksheets/sheet1.xml", Range: "C2:C2", ExpectedDigest: partDigest,
			Rows: [][]officestudio.Cell{{{Type: "number", Value: "0"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return patched.Data
}

func mustInspect(t *testing.T, kind officestudio.Kind, data []byte) officestudio.Inspection {
	t.Helper()
	i, err := officestudio.Inspect(kind, data)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func sheetDigest(t *testing.T, data []byte) string {
	t.Helper()
	i := mustInspect(t, officestudio.XLSX, data)
	for _, p := range i.Parts {
		if p.Name == "xl/worksheets/sheet1.xml" {
			return p.SHA256
		}
	}
	t.Fatal("sheet1 missing")
	return ""
}

func factsFromInputs(raw json.RawMessage) []officestudio.Fact {
	var in struct {
		Facts []struct {
			FactID   string `json:"factId"`
			Value    string `json:"value"`
			Unit     string `json:"unit"`
			Period   string `json:"period"`
			SourceID string `json:"sourceId"`
			Locked   bool   `json:"locked"`
		} `json:"facts"`
	}
	_ = json.Unmarshal(raw, &in)
	out := make([]officestudio.Fact, 0, len(in.Facts))
	for _, f := range in.Facts {
		out = append(out, officestudio.Fact{FactID: f.FactID, Value: f.Value, Unit: f.Unit, Period: f.Period, SourceID: f.SourceID, Locked: f.Locked})
	}
	return out
}

func factValue(raw json.RawMessage, id string) string {
	for _, f := range factsFromInputs(raw) {
		if f.FactID == id {
			return f.Value
		}
	}
	return ""
}

func factBullets(facts []officestudio.Fact) []string {
	var out []string
	for _, f := range facts {
		out = append(out, f.FactID+"="+f.Value)
	}
	return out
}

func factBlob(facts []officestudio.Fact) string {
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(f.FactID)
		b.WriteByte('=')
		b.WriteString(f.Value)
		b.WriteByte(' ')
		b.WriteString(f.Unit)
		b.WriteByte(' ')
		b.WriteString(f.Period)
		b.WriteByte('\n')
	}
	return b.String()
}

func p02ComparisonText() string {
	var b strings.Builder
	b.WriteString("这是一个需要自动换行同时保持清晰阅读层级的企业数字化转型方案对比标题示例\n")
	for n := 1; n <= 12; n++ {
		fmt.Fprintf(&b, "方案A第%d项详细说明 方案B第%d项不同策略\n", n, n)
	}
	return b.String()
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func inspectionText(i officestudio.Inspection) string {
	var b strings.Builder
	b.WriteString(i.Preview)
	for _, n := range i.Nodes {
		b.WriteString(n.Text)
	}
	return b.String()
}

func injectCachedFormulaValue(t *testing.T, data []byte, wrong string) []byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	changed := false
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && bytes.Contains(body, []byte("<f>")) {
			next := injectCachedValue(body, wrong)
			if !bytes.Equal(next, body) {
				body = next
				changed = true
			}
		}
		hw, err := w.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = hw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("no formula cell to inject")
	}
	return buf.Bytes()
}

func injectCachedValue(sheet []byte, wrong string) []byte {
	start := bytes.Index(sheet, []byte("<f>"))
	if start < 0 {
		return sheet
	}
	end := bytes.Index(sheet[start:], []byte("</f>"))
	if end < 0 {
		return sheet
	}
	end += start + len("</f>")
	after := sheet[end:]
	if bytes.HasPrefix(after, []byte("<v>")) {
		close := bytes.Index(after, []byte("</v>"))
		if close < 0 {
			return sheet
		}
		return append(append(append([]byte{}, sheet[:end]...), []byte("<v>"+wrong+"</v>")...), after[close+len("</v>"):]...)
	}
	return append(append(append([]byte{}, sheet[:end]...), []byte("<v>"+wrong+"</v>")...), after...)
}

func c02CandidateCSV() string {
	return c02SourceCSV(1, true, true)
}

func c02WrongIDSetCSV() string {
	var b strings.Builder
	b.WriteString("id,updated_at,date\n")
	for n := 101; n <= 180; n++ {
		fmt.Fprintf(&b, "%06d,2026-06-01T00:00:00Z,2026-03-01\n", n)
	}
	return b.String()
}

func c02NoLeadingZeroCSV() string {
	var b strings.Builder
	b.WriteString("id,updated_at,date\n")
	for n := 1; n <= 80; n++ {
		fmt.Fprintf(&b, "%d,2026-06-01T00:00:00Z,2026-03-01\n", 100000+n)
	}
	return b.String()
}

func c02StaleLatestCSV() string {
	return c02SourceCSV(1, false, true)
}

func c02FilledEmptyDateCSV() string {
	return c02SourceCSV(1, true, false)
}

func c02SourceCSV(idBase int, includeLater bool, keepEmpty38 bool) string {
	var b strings.Builder
	b.WriteString("id,updated_at,date\n")
	for n := 1; n <= 80; n++ {
		date := fmt.Sprintf("2026-03-%02d", (n%28)+1)
		if n == 5 || n == 17 || n == 38 {
			if keepEmpty38 || n != 38 {
				date = ""
			}
		}
		if n == 38 && !keepEmpty38 {
			date = "2026-03-11"
		}
		fmt.Fprintf(&b, "%06d,2026-06-01T00:00:00Z,%s\n", idBase+n-1, date)
	}
	if includeLater {
		for n := 81; n <= 100; n++ {
			id := n - 80
			fmt.Fprintf(&b, "%06d,2026-06-02T00:00:00Z,2026-07-%02d\n", idBase+id-1, (id%28)+1)
		}
	}
	return b.String()
}
