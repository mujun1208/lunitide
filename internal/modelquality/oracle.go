package modelquality

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/officestudio"
)

func registerOracles(s *Suite) {
	s.oracles["D01"] = evalD01
	s.oracles["D02"] = evalD02
	s.oracles["D03"] = evalD03
	s.oracles["P01"] = evalP01
	s.oracles["P02"] = evalP02
	s.oracles["P03"] = evalP03
	s.oracles["X01"] = evalX01
	s.oracles["X02"] = evalX02
	s.oracles["X03"] = evalX03
	s.oracles["F01"] = evalF01
	s.oracles["F02"] = evalF02
	s.oracles["F03"] = evalF03
	s.oracles["R01"] = evalR01
	s.oracles["R02"] = evalR02
	s.oracles["R03"] = evalR03
	s.oracles["R04"] = evalR04
	s.oracles["L01"] = evalL01
	s.oracles["L02"] = evalL02
	s.oracles["L03"] = evalL03
	s.oracles["L04"] = evalL04
	s.oracles["C01"] = evalC01
	s.oracles["C02"] = evalC02
	s.oracles["C03"] = evalC03
	s.oracles["C04"] = evalC04
}

func evalD01(s *Suite, _ string, obs Observation) Verdict {
	facts := lockedFacts(s.MustCase("D01").Inputs)
	blob := observationBlob(obs, officestudio.DOCX)
	var reasons []string
	for _, f := range facts {
		if !strings.Contains(blob, f.Value) {
			reasons = append(reasons, "missing locked fact "+f.FactID+"="+f.Value)
		}
	}
	if insp, ok := inspectIf(obs, officestudio.DOCX); ok && insp.Structure != nil {
		if insp.Structure.HeadingCounts["1"] < 6 {
			reasons = append(reasons, "sections<6")
		}
		if insp.Structure.Tables < 2 {
			reasons = append(reasons, "tables<2")
		}
	}
	if len(reasons) > 0 {
		return failVerdict(reasons...)
	}
	return passVerdict()
}

func evalD02(s *Suite, _ string, obs Observation) Verdict {
	titles := d02Titles(s.MustCase("D02").Inputs)
	if len(titles) != 65 {
		return failVerdict(fmt.Sprintf("case rows=%d", len(titles)))
	}
	blob := observationBlob(obs, officestudio.DOCX)
	var reasons []string
	for _, title := range titles {
		if !strings.Contains(blob, title) {
			reasons = append(reasons, "missing "+title)
		}
	}
	return maybeFail(reasons...)
}

func evalD03(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "replacementSentence", "计划收入150万元"),
		requireVal(obs, "cellTo", "1500000"),
		requireVal(obs, "onlyTargetChanged", "true"),
		requireVal(obs, "oldVersionImmutable", "true"),
		requireVal(obs, "newPdfBound", "true"),
	)
}

func evalP01(s *Suite, _ string, obs Observation) Verdict {
	facts := lockedFacts(s.MustCase("P01").Inputs)
	blob := observationBlob(obs, officestudio.PPTX)
	var reasons []string
	for _, f := range facts {
		if !strings.Contains(blob, f.Value) && obs.Values[f.FactID] != f.Value {
			reasons = append(reasons, "missing fact "+f.FactID)
		}
	}
	if obs.Values["slides"] != "" && obs.Values["slides"] != "8" {
		reasons = append(reasons, "slides")
	}
	if obs.Values["charts"] != "" && obs.Values["charts"] != "2" {
		reasons = append(reasons, "charts")
	}
	return maybeFail(reasons...)
}

func evalP02(_ *Suite, _ string, obs Observation) Verdict {
	title := "这是一个需要自动换行同时保持清晰阅读层级的企业数字化转型方案对比标题示例"
	blob := obs.Text
	if blob == "" {
		blob = obs.Values["title"]
	}
	var reasons []string
	if !strings.Contains(blob, title) {
		reasons = append(reasons, "title")
	}
	for n := 1; n <= 12; n++ {
		if !strings.Contains(blob, fmt.Sprintf("方案A第%d项", n)) || !strings.Contains(blob, fmt.Sprintf("方案B第%d项", n)) {
			reasons = append(reasons, fmt.Sprintf("comparison %d", n))
		}
	}
	if obs.Values["comparisons"] != "" && obs.Values["comparisons"] != "12" {
		reasons = append(reasons, "comparisons")
	}
	if pt, _ := strconv.Atoi(obs.Values["minBodyPt"]); obs.Values["minBodyPt"] != "" && pt < 20 {
		reasons = append(reasons, "minBodyPt")
	}
	if obs.Values["outOfBounds"] != "" && obs.Values["outOfBounds"] != "0" {
		reasons = append(reasons, "outOfBounds")
	}
	return maybeFail(reasons...)
}

func evalP03(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "chart", "100,250,300"),
		requireVal(obs, "imageBoxEqual", "true"),
		requireVal(obs, "unrelatedEqual", "true"),
	)
}

func evalX01(_ *Suite, _ string, obs Observation) Verdict {
	if len(obs.Artifact) == 0 {
		return failVerdict("X01 missing workbook")
	}
	insp, err := officestudio.Inspect(officestudio.XLSX, obs.Artifact)
	if err != nil {
		return failVerdict(err.Error())
	}
	cents, err := officestudio.IndependentIntegerCents(insp)
	if err != nil || cents != 2195700 {
		return failVerdict(fmt.Sprintf("IndependentIntegerCents=%d %v", cents, err))
	}
	if officestudio.FormulaCacheDisagreesWithOracle(obs.Artifact, 2195700) {
		return failVerdict("formula cache disagrees with 2195700")
	}
	return passVerdict()
}

func evalX02(_ *Suite, _ string, obs Observation) Verdict {
	if len(obs.Artifact) == 0 {
		return failVerdict("X02 missing workbook")
	}
	insp, err := officestudio.Inspect(officestudio.XLSX, obs.Artifact)
	if err != nil {
		return failVerdict(err.Error())
	}
	g, err := officestudio.IndependentGrowth(insp)
	if err != nil || g != "0.25" {
		return failVerdict(fmt.Sprintf("growth=%q %v", g, err))
	}
	a, err := officestudio.IndependentAttainment(insp)
	if err != nil || a != "0.9375" {
		return failVerdict(fmt.Sprintf("attainment=%q %v", a, err))
	}
	z, err := officestudio.IndependentZeroDenominator(insp)
	if err != nil || z != "n/a" {
		return failVerdict(fmt.Sprintf("zero=%q %v", z, err))
	}
	return passVerdict()
}

func evalX03(_ *Suite, _ string, obs Observation) Verdict {
	if len(obs.Artifact) == 0 {
		return failVerdict("X03 missing workbook")
	}
	insp, err := officestudio.Inspect(officestudio.XLSX, obs.Artifact)
	if err != nil {
		return failVerdict(err.Error())
	}
	sum, err := officestudio.IndependentRangeSum(insp, "B2:B5")
	if err != nil || sum != 110 {
		return failVerdict(fmt.Sprintf("IndependentRangeSum=%d %v", sum, err))
	}
	return passVerdict()
}

func evalF01(_ *Suite, _ string, obs Observation) Verdict {
	if obs.Values["oldSha"] == "" || obs.Values["newSha"] == "" || obs.Values["oldSha"] == obs.Values["newSha"] {
		return failVerdict("source SHA must change")
	}
	if !strings.Contains(obs.Text+obs.Values["updatedText"], "季度收入150万元") {
		return failVerdict("updated text")
	}
	return passVerdict()
}

func evalF02(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "sources", "30"),
		requireVal(obs, "tableRows", "90"),
		requireVal(obs, "searchableChinese", "true"),
		emptyAsFail(obs.Values["pagesParsed"], "pagesParsed"),
		emptyAsFail(obs.Values["backend"], "backend"),
	)
}

func evalF03(_ *Suite, scenario string, obs Observation) Verdict {
	if scenario == "missing-glyph" || obs.ScenarioID == "missing-glyph" {
		v := maybeFail(
			requireVal(obs, "missingGlyphDetected", "true"),
			requireVal(obs, "formalBlocked", "true"),
			requireVal(obs, "silentLoss", "false"),
		)
		v.LiveSuccessIncrement = false
		return v
	}
	want := "人民币￥100；达成率％；长度μm；温度℃；生僻字𠮷。"
	if obs.Text != "" && !strings.Contains(obs.Text, want) {
		return failVerdict("pdf text missing input glyph")
	}
	return maybeFail(
		requireVal(obs, "allGlyphsVisible", "true"),
		requireVal(obs, "pdfTextExact", "true"),
	)
}

func evalR01(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "observedExact", "true"),
		requireVal(obs, "sequenceComplete", "true"),
		requireVal(obs, "whitespacePreserved", "true"),
		requireVal(obs, "largeReasoning", "300000"),
	)
}

func evalR02(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "completeCallOnce", "true"),
		requireVal(obs, "incompleteExecuted", "0"),
		requireVal(obs, "usageUnknown", "true"),
	)
}

func evalR03(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "privateIsolated", "true"),
		requireVal(obs, "factsRetained", "true"),
		requireVal(obs, "paramsLegal", "true"),
		emptyAsFail(obs.Values["rebuildReason"], "rebuildReason"),
	)
}

func evalR04(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "qualificationInvalidated", "true"),
		requireVal(obs, "unknownFabricated", "false"),
		requireVal(obs, "inflightPinned", "true"),
		requireVal(obs, "newActivationNeedsQual", "true"),
	)
}

func evalL01(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "itemsCovered", "40"),
		requireVal(obs, "preflightEveryAttempt", "true"),
		requireVal(obs, "brokenToolGroup", "false"),
	)
}

func evalL02(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "taskVerified", "false"),
		requireVal(obs, "missingStep", "write"),
		requireVal(obs, "modelL0Ignored", "true"),
	)
}

func evalL03(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "knownEffectCount", "1"),
		requireVal(obs, "unknownReconciled", "true"),
		requireVal(obs, "sameBudget", "true"),
	)
}

func evalL04(_ *Suite, _ string, obs Observation) Verdict {
	return maybeFail(
		requireVal(obs, "admitted700", "1"),
		requireVal(obs, "doubleCharge", "false"),
		requireVal(obs, "followupAfterCancel", "false"),
	)
}

func evalC01(_ *Suite, _ string, obs Observation) Verdict {
	cases := []string{"2026-01-31T12:00:00Z", "2024-02-28T12:00:00Z"}
	var reasons []string
	for _, in := range cases {
		ts, err := time.Parse(time.RFC3339, in)
		if err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		want := IndependentNextDay(ts).UTC().Format(time.RFC3339)
		if obs.Values[in] != want {
			reasons = append(reasons, fmt.Sprintf("%s got %s want %s", in, obs.Values[in], want))
		}
	}
	if obs.Values["testFilesUnchanged"] != "true" {
		reasons = append(reasons, "test files changed")
	}
	return maybeFail(reasons...)
}

func evalC02(s *Suite, _ string, obs Observation) Verdict {
	want, err := IndependentC02Expected(s.MustCase("C02").Inputs)
	if err != nil {
		return failVerdict(err.Error())
	}
	rows, err := IndependentParseCustomerCSV(obs.Text)
	if err != nil {
		return failVerdict(err.Error())
	}
	got := IndependentCustomerLatest(rows)
	return compareC02Latest(want, got)
}

func evalC03(s *Suite, _ string, obs Observation) Verdict {
	var in struct {
		Rows []SalesRow `json:"rows"`
	}
	if err := json.Unmarshal(s.MustCase("C03").Inputs, &in); err != nil {
		return failVerdict(err.Error())
	}
	want := IndependentSalesGroups(in.Rows)
	got, err := IndependentParseSalesCSV(obs.Text)
	if err != nil {
		return failVerdict(err.Error())
	}
	if !sameSalesGroups(want, got) {
		return failVerdict(fmt.Sprintf("groups got %#v want %#v", got, want))
	}
	return passVerdict()
}

func evalC04(s *Suite, _ string, obs Observation) Verdict {
	facts := lockedFacts(s.MustCase("C04").Inputs)
	var reasons []string
	for _, format := range []string{"docx", "pptx", "xlsx", "pdf"} {
		blob := obs.Values[format]
		for _, f := range facts {
			if !strings.Contains(blob, f.Value) || !strings.Contains(blob, f.Unit) || !strings.Contains(blob, f.Period) {
				reasons = append(reasons, format+" missing "+f.FactID)
			}
		}
	}
	return maybeFail(reasons...)
}

func evalHoldOutFacts(s *Suite, _ string, obs Observation) Verdict {
	h := s.HoldOutCase("H-D01")
	facts := lockedFacts(h.Inputs)
	blob := obs.Text
	for _, f := range facts {
		if blob != "" && !strings.Contains(blob, f.Value) {
			return failVerdict("hold-out fact " + f.FactID)
		}
	}
	return passVerdict()
}

func evalHoldOutRows(s *Suite, _ string, obs Observation) Verdict {
	if s.HoldOutCase("H-D02").RowCount != 40 {
		return failVerdict("hold-out rows")
	}
	if obs.Text != "" && !strings.Contains(obs.Text, "留出条目") {
		return failVerdict("hold-out titles")
	}
	return passVerdict()
}

func evalHoldOutCents(_ *Suite, _ string, obs Observation) Verdict {
	if obs.Values["cents"] != "" && obs.Values["cents"] == "2195700" {
		return failVerdict("hold-out must not reuse X01 cents")
	}
	return passVerdict()
}

func IndependentNextDay(t time.Time) time.Time {
	return t.AddDate(0, 0, 1)
}

type SalesRow struct {
	Month       string `json:"month"`
	Region      string `json:"region"`
	AmountCents int64  `json:"amountCents"`
}

func IndependentSalesGroups(rows []SalesRow) []SalesRow {
	type key struct{ month, region string }
	sum := map[key]int64{}
	var order []key
	seen := map[key]bool{}
	for _, r := range rows {
		k := key{r.Month, r.Region}
		if !seen[k] {
			order = append(order, k)
			seen[k] = true
		}
		sum[k] += r.AmountCents
	}
	out := make([]SalesRow, 0, len(order))
	for _, k := range order {
		out = append(out, SalesRow{Month: k.month, Region: k.region, AmountCents: sum[k]})
	}
	return out
}

type customerRow struct {
	ID        string
	UpdatedAt string
	Date      string
}

func IndependentParseCustomerCSV(text string) ([]customerRow, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(text)))
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("customer csv: no data")
	}
	var out []customerRow
	for _, rec := range records[1:] {
		if len(rec) < 3 {
			return nil, fmt.Errorf("customer csv: short row")
		}
		out = append(out, customerRow{ID: rec[0], UpdatedAt: rec[1], Date: rec[2]})
	}
	return out, nil
}

func IndependentCustomerLatest(rows []customerRow) []customerRow {
	best := map[string]customerRow{}
	var order []string
	for _, r := range rows {
		prev, ok := best[r.ID]
		if !ok {
			order = append(order, r.ID)
			best[r.ID] = r
			continue
		}
		if r.UpdatedAt > prev.UpdatedAt {
			best[r.ID] = r
		}
	}
	out := make([]customerRow, 0, len(order))
	for _, id := range order {
		out = append(out, best[id])
	}
	return out
}

func IndependentParseSalesCSV(text string) ([]SalesRow, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(text)))
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("sales csv: no data")
	}
	var out []SalesRow
	for _, rec := range records[1:] {
		if len(rec) < 3 {
			return nil, fmt.Errorf("sales csv: short row")
		}
		n, err := strconv.ParseInt(rec[2], 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, SalesRow{Month: rec[0], Region: rec[1], AmountCents: n})
	}
	return out, nil
}

func sameSalesGroups(a, b []SalesRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func IndependentC02Expected(raw json.RawMessage) ([]customerRow, error) {
	src, err := independentC02SourceRows(raw)
	if err != nil {
		return nil, err
	}
	return IndependentCustomerLatest(src), nil
}

func independentC02SourceRows(raw json.RawMessage) ([]customerRow, error) {
	var spec struct {
		Rows          int    `json:"rows"`
		UniqueIDs     int    `json:"uniqueIds"`
		DuplicateRule string `json:"duplicateRule"`
		IDWidth       int    `json:"idWidth"`
		EmptyDatesAt  []int  `json:"emptyDatesAt"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, err
	}
	if spec.Rows <= 0 || spec.UniqueIDs <= 0 || spec.IDWidth < 2 {
		return nil, fmt.Errorf("c02 fixture spec incomplete")
	}
	empty := map[int]bool{}
	for _, n := range spec.EmptyDatesAt {
		empty[n] = true
	}
	out := make([]customerRow, 0, spec.Rows)
	for n := 1; n <= spec.UniqueIDs; n++ {
		date := fmt.Sprintf("2026-03-%02d", (n%28)+1)
		if empty[n] {
			date = ""
		}
		out = append(out, customerRow{
			ID:        fmt.Sprintf("%0*d", spec.IDWidth, n),
			UpdatedAt: "2026-06-01T00:00:00Z",
			Date:      date,
		})
	}
	for n := spec.UniqueIDs + 1; n <= spec.Rows; n++ {
		id := n - spec.UniqueIDs
		out = append(out, customerRow{
			ID:        fmt.Sprintf("%0*d", spec.IDWidth, id),
			UpdatedAt: "2026-06-02T00:00:00Z",
			Date:      fmt.Sprintf("2026-07-%02d", (id%28)+1),
		})
	}
	_ = spec.DuplicateRule
	return out, nil
}

func compareC02Latest(want, got []customerRow) Verdict {
	if len(got) != len(want) {
		return failVerdict(fmt.Sprintf("unique=%d want %d", len(got), len(want)))
	}
	byID := map[string]customerRow{}
	for _, r := range got {
		if !hasLeadingZero(r.ID) {
			return failVerdict("leading zero " + r.ID)
		}
		byID[r.ID] = r
	}
	for _, w := range want {
		g, ok := byID[w.ID]
		if !ok {
			return failVerdict("missing fixture id " + w.ID)
		}
		if g.UpdatedAt != w.UpdatedAt {
			return failVerdict("latest timestamp " + w.ID)
		}
		if g.Date != w.Date {
			return failVerdict("empty date " + w.ID)
		}
	}
	return passVerdict()
}

func sixDigitNumericID(id string) bool {
	if len(id) != 6 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func hasLeadingZero(id string) bool {
	return sixDigitNumericID(id) && id[0] == '0'
}

func lockedFacts(raw json.RawMessage) []officestudio.Fact {
	var in struct {
		Facts []officestudio.Fact `json:"facts"`
	}
	_ = json.Unmarshal(raw, &in)
	return in.Facts
}

func d02Titles(raw json.RawMessage) []string {
	var in struct {
		Rows []struct {
			Title string `json:"title"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(raw, &in)
	out := make([]string, 0, len(in.Rows))
	for _, r := range in.Rows {
		out = append(out, r.Title)
	}
	return out
}

func observationBlob(obs Observation, kind officestudio.Kind) string {
	if blob := obs.Text; blob != "" && len(obs.Artifact) == 0 {
		return blob
	}
	if insp, ok := inspectIf(obs, kind); ok {
		var b strings.Builder
		b.WriteString(obs.Text)
		b.WriteString(insp.Preview)
		for _, n := range insp.Nodes {
			b.WriteString(n.Text)
		}
		return b.String()
	}
	return obs.Text
}

func inspectIf(obs Observation, kind officestudio.Kind) (officestudio.Inspection, bool) {
	if len(obs.Artifact) == 0 {
		return officestudio.Inspection{}, false
	}
	insp, err := officestudio.Inspect(kind, obs.Artifact)
	if err != nil {
		return officestudio.Inspection{}, false
	}
	return insp, true
}

func maybeFail(reasons ...string) Verdict {
	var out []string
	for _, r := range reasons {
		if strings.TrimSpace(r) != "" {
			out = append(out, r)
		}
	}
	if len(out) > 0 {
		return failVerdict(out...)
	}
	return passVerdict()
}

func emptyAsFail(got, key string) string {
	if strings.TrimSpace(got) == "" {
		return key + " empty"
	}
	return ""
}
