package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

func d01LockedFacts() []Fact {
	return []Fact{
		{FactID: "revenue_q1", Value: "1200000", Unit: "CNY", Period: "2026Q1", SourceID: "synthetic-sales-v1", Locator: "Q1收入", Locked: true},
		{FactID: "revenue_q2", Value: "1500000", Unit: "CNY", Period: "2026Q2", SourceID: "synthetic-sales-v1", Locator: "Q2收入", Locked: true},
		{FactID: "cost_q1", Value: "720000", Unit: "CNY", Period: "2026Q1", SourceID: "synthetic-sales-v1", Locator: "Q1成本", Locked: true},
		{FactID: "cost_q2", Value: "870000", Unit: "CNY", Period: "2026Q2", SourceID: "synthetic-sales-v1", Locator: "Q2成本", Locked: true},
		{FactID: "customers_q1", Value: "120", Unit: "count", Period: "2026Q1", SourceID: "synthetic-sales-v1", Locator: "Q1客户", Locked: true},
		{FactID: "customers_q2", Value: "150", Unit: "count", Period: "2026Q2", SourceID: "synthetic-sales-v1", Locator: "Q2客户", Locked: true},
		{FactID: "retention", Value: "92", Unit: "percent", Period: "2026Q2", SourceID: "synthetic-sales-v1", Locator: "留存", Locked: true},
		{FactID: "nps", Value: "48", Unit: "points", Period: "2026Q2", SourceID: "synthetic-sales-v1", Locator: "NPS", Locked: true},
	}
}

func TestDOCXLongTableAndFieldRefresh(t *testing.T) {
	ops, err := WordDeliverySpec("ops", "运营纪要", d01LockedFacts())
	if err != nil {
		t.Fatal(err)
	}
	brand, err := WordDeliverySpec("brand", "品牌纪要", d01LockedFacts())
	if err != nil {
		t.Fatal(err)
	}
	editorial, err := WordDeliverySpec("editorial", "中文研究报告", d01LockedFacts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(headingSequence(ops), ",") == strings.Join(headingSequence(brand), ",") || strings.Join(headingSequence(brand), ",") == strings.Join(headingSequence(editorial), ",") {
		t.Fatalf("styles must differ by structure, not only chrome: ops=%v brand=%v editorial=%v", headingSequence(ops), headingSequence(brand), headingSequence(editorial))
	}
	if !hasHeadingOrder(ops, []string{"结论", "指标", "行动"}) {
		t.Fatalf("ops structure: %v", headingSequence(ops))
	}
	if !hasHeadingOrder(brand, []string{"观点", "证据", "建议"}) {
		t.Fatalf("brand structure: %v", headingSequence(brand))
	}
	if !hasHeadingOrder(editorial, []string{"摘要", "经营概况", "收入分析", "成本分析", "客户分析", "建议"}) {
		t.Fatalf("editorial D01 sections: %v", headingSequence(editorial))
	}

	empty, err := WordDeliverySpec("editorial", "空报告", nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyData, err := Generate(empty)
	if err != nil {
		t.Fatal(err)
	}
	emptyBlob := string(zipParts(t, emptyData)["word/document.xml"])
	if strings.Contains(emptyBlob, "1200000") || strings.Contains(emptyBlob, "节约了") || strings.Contains(emptyBlob, "周报完成率") {
		t.Fatal("empty editorial invented revenue")
	}

	data, err := Generate(editorial)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if i.Structure == nil || i.Structure.HeadingCounts["1"] < 6 || i.Structure.Tables != 2 || i.Structure.TOCFields != 1 || i.Structure.PageFields < 1 || !i.Structure.NeedsFieldUpdate {
		t.Fatalf("D01 structure: %#v", i.Structure)
	}
	blob := i.Preview
	for _, n := range i.Nodes {
		blob += n.Text
	}
	for _, f := range d01LockedFacts() {
		if !strings.Contains(blob, f.Value) {
			t.Fatalf("locked fact %s=%s missing", f.FactID, f.Value)
		}
	}
	if strings.Contains(blob, "节约了") || strings.Contains(i.Preview, "高端商用") || strings.Contains(strings.ToLower(i.Preview), "qualified") {
		t.Fatal("invented revenue or live qualified mark")
	}
	v, err := Validate(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	field := FieldRefreshStatus(i, nil)
	if field.ID != "fields_update" || field.Status == "passed" {
		t.Fatalf("unrefreshed fields must not pass: %#v", field)
	}
	if field.Status != "unknown" && field.Status != "missing" {
		t.Fatalf("no renderer evidence should be unknown/missing: %#v", field)
	}
	foundMissing := false
	for _, c := range v.Checks {
		if c.ID == "fields_update" && c.Status == "missing" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("Validate must keep fields_update missing before refresh: %#v", v.Checks)
	}

	parts := zipParts(t, data)
	if !bytes.Contains(parts["word/settings.xml"], []byte("updateFields")) {
		t.Fatal("field update flag missing")
	}
	candidate := editZIP(t, data, map[string][]byte{
		"word/document.xml": bytes.Replace(parts["word/document.xml"], []byte("目录待实际排版后更新；当前未计算页码。"), []byte("摘要 2"), 1),
	})
	merged, err := MergeNativeCaches(DOCX, data, candidate)
	if err != nil || merged.UpdatedFields == 0 {
		t.Fatalf("field refresh merge: %+v %v", merged, err)
	}
	out := zipParts(t, merged.Data)
	if bytes.Contains(out["word/document.xml"], []byte("目录待实际排版后更新；当前未计算页码。")) {
		t.Fatal("TOC cache not refreshed")
	}
	if !bytes.Contains(out["word/document.xml"], []byte("摘要 2")) {
		t.Fatal("TOC page number missing after refresh")
	}
	refreshed, err := Inspect(DOCX, merged.Data)
	if err != nil {
		t.Fatal(err)
	}
	if status := FieldRefreshStatus(refreshed, &merged); status.Status != "passed" {
		t.Fatalf("refreshed fields: %#v", status)
	}

	longRows := [][]string{{"编号", "标题", "说明"}}
	for n := 1; n <= 65; n++ {
		desc := "条目说明"
		if n <= 6 {
			desc = strings.Repeat("这是一段需要换行且跨页仍保持完整的中英混排 Customer Description。", 4)
		}
		longRows = append(longRows, []string{itoa(n), "条目" + itoa(n), desc})
	}
	longSpec, err := WordLongTableSpec("客户方案长表", longRows)
	if err != nil {
		t.Fatal(err)
	}
	longData, err := Generate(longSpec)
	if err != nil {
		t.Fatal(err)
	}
	longParts := zipParts(t, longData)
	body := longParts["word/document.xml"]
	if !bytes.Contains(body, []byte("<w:tblHeader/>")) {
		t.Fatal("65-row table missing repeating header")
	}
	if bytes.Contains(body, []byte("trHeight")) {
		t.Fatal("fixed row height would clip long cells")
	}
	if bytes.Count(body, []byte("<w:tr>")) != 66 {
		t.Fatalf("want header+65 rows, got %d tr", bytes.Count(body, []byte("<w:tr>")))
	}
	longInsp, err := Inspect(DOCX, longData)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range longInsp.Issues {
		if issue.Code == "OFFICE_FIXED_ROW_CLIP" || issue.Code == "OFFICE_TABLE_HEADER" {
			t.Fatalf("D02 table issue: %#v", issue)
		}
	}
	if !strings.Contains(longInsp.Preview, "条目65") {
		t.Fatal("row 65 truncated")
	}

	sentence := findText(t, i, "计划收入120万元")
	cell := findText(t, i, "1200000")
	patched, err := Patch(data, PatchRequest{
		Kind: DOCX, BaseSHA256: i.SHA256,
		Operations: []TextPatch{
			{NodeID: sentence.ID, ExpectedDigest: sentence.Digest, Text: "计划收入150万元"},
			{NodeID: cell.ID, ExpectedDigest: cell.Digest, Text: "1500000"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, p := range i.Parts {
		before[p.Name] = p.SHA256
	}
	changed := map[string]bool{}
	for _, name := range patched.ChangedParts {
		changed[name] = true
	}
	if !changed["word/document.xml"] {
		t.Fatal("D03 must change the target document part")
	}
	for _, p := range patched.Inspection.Parts {
		if changed[p.Name] {
			continue
		}
		if before[p.Name] != p.SHA256 {
			t.Fatalf("unrelated part digest changed: %s", p.Name)
		}
	}
	if findText(t, patched.Inspection, "计划收入150万元").ID == "" || findText(t, patched.Inspection, "1500000").ID == "" {
		t.Fatal("D03 target nodes missing")
	}
	if strings.Contains(patched.Inspection.Preview, "计划收入120万元") {
		t.Fatal("original sentence survived targeted patch")
	}
}

func headingSequence(spec Spec) []string {
	var out []string
	for _, b := range spec.Blocks {
		if b.Type == "heading" || b.Type == "heading2" || b.Type == "heading3" {
			out = append(out, b.Text)
		}
	}
	return out
}

func hasHeadingOrder(spec Spec, want []string) bool {
	got := headingSequence(spec)
	i := 0
	for _, h := range got {
		if i < len(want) && h == want[i] {
			i++
		}
	}
	return i == len(want)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
