package xlsxrows

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestGridsReadsValuesAndKeepsGapRows(t *testing.T) {
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	if err := f.SetCellValue("Sheet1", "A1", "name"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "A3", "gap"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	grids, err := Grids(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(grids) != 1 || grids[0].Name != "Sheet1" {
		t.Fatalf("grids = %#v", grids)
	}
	rows := grids[0].Rows
	if len(rows) != 3 || rows[0][0] != "name" || rows[2][0] != "gap" {
		t.Fatalf("rows = %#v", rows)
	}
	if len(rows[1]) != 0 {
		t.Fatalf("gap row should be empty, got %#v", rows[1])
	}
}

func TestGridsRejectsEmptyAndNegativeSharedString(t *testing.T) {
	if _, err := Grids(nil); err == nil {
		t.Fatal("empty input must fail")
	}
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	if err := f.SetCellValue("Sheet1", "A1", "ok"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	poisoned, err := rewriteSharedIndex(buf.Bytes(), "-1")
	if err != nil {
		t.Fatal(err)
	}
	grids, err := Grids(poisoned)
	if err != nil {
		t.Fatal(err)
	}
	if len(grids) != 1 {
		t.Fatalf("grids = %#v", grids)
	}
	for _, row := range grids[0].Rows {
		for _, cell := range row {
			if cell != "" {
				t.Fatalf("negative shared-string index must be empty, got %#v", grids[0].Rows)
			}
		}
	}
}

func rewriteSharedIndex(raw []byte, index string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		var body bytes.Buffer
		if _, err := body.ReadFrom(rc); err != nil {
			_ = rc.Close()
			return nil, err
		}
		_ = rc.Close()
		payload := body.Bytes()
		if strings.HasSuffix(strings.ToLower(file.Name), "sheet1.xml") {
			payload = bytes.Replace(payload, []byte(`<v>0</v>`), []byte("<v>"+index+"</v>"), 1)
		}
		w, err := zw.Create(file.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(payload); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
