package officestudio

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func x01Orders() []OrderLine {
	out := make([]OrderLine, 200)
	for n := 1; n <= 200; n++ {
		base := int64(10000 + (n-1)*37)
		amount := base
		if n%10 == 1 {
			amount = -base
		}
		note := ""
		if n > 1 {
			note = "订单" + itoa(n)
		}
		region := []string{"east", "west", "north", "south"}[(n-1)%4]
		out[n-1] = OrderLine{
			OrderID:             fmt.Sprintf("%06d", n),
			Date:                fmt.Sprintf("2026-06-%02d", ((n-1)%28)+1),
			AmountCents:         amount,
			DiscountBasisPoints: ((n - 1) % 5) * 100,
			Region:              region,
			Note:                note,
		}
	}
	return out
}

func TestXLSXIndependentOracleAndTypes(t *testing.T) {
	orders := x01Orders()
	var wantCents int64
	for _, o := range orders {
		wantCents += o.AmountCents
	}
	if wantCents != 2195700 {
		t.Fatalf("fixture cents drifted: %d", wantCents)
	}

	empty, err := PlanWorkbookProfile("ops", "空台账", nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyData, err := Generate(empty)
	if err != nil {
		t.Fatal(err)
	}
	emptyInsp, err := Inspect(XLSX, emptyData)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(emptyInsp.Preview, "1200000") || strings.Contains(emptyInsp.Preview, "节约了") {
		t.Fatal("empty ops workbook invented revenue")
	}

	ops, err := PlanWorkbookProfile("ops", "经营台账", []Fact{{FactID: "orders", Value: "200", Unit: "单", Period: "2026-06", Locator: "订单数", Locked: true}})
	if err != nil {
		t.Fatal(err)
	}
	brand, err := PlanWorkbookProfile("brand", "销售看板", []Fact{{FactID: "orders", Value: "200", Unit: "单", Period: "2026-06", Locator: "订单数", Locked: true}})
	if err != nil {
		t.Fatal(err)
	}
	editorial, err := PlanWorkbookProfile("editorial", "数据说明", []Fact{{FactID: "orders", Value: "200", Unit: "单", Period: "2026-06", Locator: "订单数", Locked: true}})
	if err != nil {
		t.Fatal(err)
	}
	if sheetNames(ops) == sheetNames(brand) || sheetNames(brand) == sheetNames(editorial) {
		t.Fatalf("profiles must differ by sheets: ops=%v brand=%v editorial=%v", sheetNames(ops), sheetNames(brand), sheetNames(editorial))
	}
	if !hasSheets(ops, []string{"运营明细", "汇总", "异常"}) {
		t.Fatalf("ops sheets: %v", sheetNames(ops))
	}
	if !hasSheets(brand, []string{"销售漏斗", "预算", "ROI"}) {
		t.Fatalf("brand sheets: %v", sheetNames(brand))
	}
	if !hasSheets(editorial, []string{"数据字典", "分析", "来源"}) {
		t.Fatalf("editorial sheets: %v", sheetNames(editorial))
	}

	x01, err := X01OrdersSpec(orders)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(x01)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	id := findText(t, i, "000001")
	if id.Kind != "cell:text" {
		t.Fatalf("six-digit order id must stay text: %#v", id)
	}
	amount := findText(t, i, "-10000")
	if amount.Kind != "cell:number" {
		t.Fatalf("amount must be numeric: %#v", amount)
	}
	gotCents, err := IndependentIntegerCents(i)
	if err != nil || gotCents != 2195700 {
		t.Fatalf("independent cents oracle: %d %v", gotCents, err)
	}
	formula := ""
	for _, n := range i.Nodes {
		if n.Kind == "cell:formula" {
			formula = n.Text
			break
		}
	}
	if formula == "" || !strings.Contains(formula, "SUM") || !strings.Contains(formula, "!") {
		t.Fatalf("X01 needs a cross-sheet SUM formula, got %q", formula)
	}
	if strings.Contains(i.Preview, "节约了") || strings.Contains(i.Preview, "高端商用") {
		t.Fatal("invented revenue or live qualified")
	}
	wrong := injectWrongFormulaCache(t, data, "1")
	wrongInsp, err := Inspect(XLSX, wrong)
	if err != nil {
		t.Fatal(err)
	}
	still, err := IndependentIntegerCents(wrongInsp)
	if err != nil || still != 2195700 {
		t.Fatalf("injected cache must not move independent oracle: %d %v", still, err)
	}
	if !FormulaCacheDisagreesWithOracle(wrong, 2195700) {
		t.Fatal("wrong cached total must be caught")
	}

	x02, err := X02SalesSpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(x02.Sheets) != 4 {
		t.Fatalf("X02 sheets=%d want 4", len(x02.Sheets))
	}
	x02Data, err := Generate(x02)
	if err != nil {
		t.Fatal(err)
	}
	x02Insp, err := Inspect(XLSX, x02Data)
	if err != nil {
		t.Fatal(err)
	}
	growth, err := IndependentGrowth(x02Insp)
	if err != nil || growth != "0.25" {
		t.Fatalf("growth oracle: %q %v", growth, err)
	}
	att, err := IndependentAttainment(x02Insp)
	if err != nil || att != "0.9375" {
		t.Fatalf("attainment oracle: %q %v", att, err)
	}
	zero, err := IndependentZeroDenominator(x02Insp)
	if err != nil || zero != "n/a" {
		t.Fatalf("zero denominator: %q %v", zero, err)
	}
	cross := false
	for _, n := range x02Insp.Nodes {
		if n.Kind == "cell:formula" && strings.Contains(n.Text, "!") {
			cross = true
		}
	}
	if !cross {
		t.Fatal("X02 missing cross-sheet formula")
	}
	var chart Node
	for _, n := range x02Insp.Nodes {
		if n.Chart != nil {
			chart = n
			break
		}
	}
	if chart.Chart == nil || len(chart.Chart.SourceRanges) == 0 {
		t.Fatal("X02 chart range missing")
	}

	x03, err := X03SumChartSpec()
	if err != nil {
		t.Fatal(err)
	}
	x03Data, err := Generate(x03)
	if err != nil {
		t.Fatal(err)
	}
	x03Insp, err := Inspect(XLSX, x03Data)
	if err != nil {
		t.Fatal(err)
	}
	partDigest := ""
	for _, p := range x03Insp.Parts {
		if p.Name == "xl/worksheets/sheet1.xml" {
			partDigest = p.SHA256
		}
	}
	patched, err := Patch(x03Data, PatchRequest{
		Kind: XLSX, BaseSHA256: x03Insp.SHA256,
		Ranges: []RangePatch{{
			Part: "xl/worksheets/sheet1.xml", Range: "B2:B5", ExpectedDigest: partDigest,
			Rows: [][]Cell{{{Type: "number", Value: "11"}}, {{Type: "number", Value: "22"}}, {{Type: "number", Value: "33"}}, {{Type: "number", Value: "44"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum, err := IndependentRangeSum(patched.Inspection, "B2:B5")
	if err != nil || sum != 110 {
		t.Fatalf("X03 sum oracle: %d %v", sum, err)
	}
	preserved := false
	for _, n := range patched.Inspection.Nodes {
		if n.Kind == "cell:formula" && strings.Contains(n.Text, "SUM(B2:B5)") {
			preserved = true
		}
	}
	if !preserved {
		t.Fatal("X03 formula not preserved")
	}
	var afterChart Node
	for _, n := range patched.Inspection.Nodes {
		if n.Chart != nil {
			afterChart = n
			break
		}
	}
	if afterChart.Chart == nil || !containsRange(afterChart.Chart.SourceRanges, "B2:B5") {
		t.Fatalf("X03 chart range: %#v", afterChart.Chart)
	}
	stale := injectWrongFormulaCache(t, patched.Data, "100")
	if !FormulaCacheDisagreesWithOracle(stale, 110) {
		t.Fatal("X03 stale cache 100 vs oracle 110 must be caught")
	}
}

func sheetNames(spec Spec) string {
	var names []string
	for _, s := range spec.Sheets {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}

func hasSheets(spec Spec, want []string) bool {
	seen := map[string]bool{}
	for _, s := range spec.Sheets {
		seen[s.Name] = true
	}
	for _, name := range want {
		if !seen[name] {
			return false
		}
	}
	return true
}

func containsRange(ranges []string, want string) bool {
	for _, r := range ranges {
		if strings.Contains(r, want) {
			return true
		}
	}
	return false
}

func injectWrongFormulaCache(t *testing.T, data []byte, wrong string) []byte {
	t.Helper()
	parts := zipParts(t, data)
	changes := map[string][]byte{}
	for name, body := range parts {
		if !strings.HasPrefix(name, "xl/worksheets/sheet") || !bytes.Contains(body, []byte("<f>")) {
			continue
		}
		next := injectCachedValue(body, wrong)
		if !bytes.Equal(next, body) {
			changes[name] = next
		}
	}
	if len(changes) == 0 {
		t.Fatal("no formula cell to inject")
	}
	return editZIP(t, data, changes)
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
