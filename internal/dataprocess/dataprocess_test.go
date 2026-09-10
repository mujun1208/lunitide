package dataprocess

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestCSVFilterMergeDedupAndFormulaReject(t *testing.T) {
	raw := []byte("name,amount\nA,1\nA,1\nB,\n=1+1,3\n")
	tab, err := ImportCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	if DetectDuckDB() == "" || DetectDuckDB() == "ready" {
		t.Fatal("DuckDB must stay missing_dependency, not vendored")
	}
	if err := RejectFormulaCell("=1+1"); err == nil {
		t.Fatal("formula injection must be rejected")
	}
	if err := RejectFormulaCell("12.5"); err != nil {
		t.Fatal(err)
	}
	filtered := Filter(tab, "name", "A")
	if len(filtered.Rows) != 2 {
		t.Fatalf("filter: %+v", filtered)
	}
	deduped := Dedup(filtered, []string{"name", "amount"})
	if len(deduped.Rows) != 1 {
		t.Fatalf("dedup: %+v", deduped)
	}
	other, _ := ImportCSV([]byte("name,amount\nC,2\n"))
	merged, err := Merge(deduped, other)
	if err != nil || len(merged.Rows) != 2 {
		t.Fatalf("merge: %+v %v", merged, err)
	}
	st := Stats(tab, "amount")
	if st.Count != 4 || st.Empty != 1 {
		t.Fatalf("stats %+v", st)
	}
	out := string(ExportCSV(merged))
	if !strings.Contains(out, "C,2") {
		t.Fatalf("export %q", out)
	}
}

func TestImportJSONAndXLSXInferTypesAndCancel(t *testing.T) {
	tab, err := ImportJSON([]byte(`[{"name":"A","amount":"1.50"},{"name":"B","amount":""}]`))
	if err != nil || len(tab.Rows) != 2 {
		t.Fatalf("json import %+v %v", tab, err)
	}
	kinds := InferTypes(tab)
	if kinds["name"] != "text" || kinds["amount"] != "number" {
		t.Fatalf("types %+v", kinds)
	}

	xf := excelize.NewFile()
	_ = xf.SetCellValue("Sheet1", "A1", "name")
	_ = xf.SetCellValue("Sheet1", "B1", "amount")
	_ = xf.SetCellValue("Sheet1", "A2", "A")
	_ = xf.SetCellValue("Sheet1", "B2", "1")
	var buf bytes.Buffer
	if err := xf.Write(&buf); err != nil {
		t.Fatal(err)
	}
	xtab, err := ImportXLSX(buf.Bytes())
	if err != nil || len(xtab.Rows) != 1 || xtab.Headers[0] != "name" {
		t.Fatalf("xlsx %+v %v", xtab, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Apply(ctx, tab, func(t Table) Table { return t }); err == nil {
		t.Fatal("cancelled apply must fail")
	}
}
