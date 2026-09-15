package officeapp

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	content "github.com/lunitide/lunitide/internal/officestudio"
)

func injectServiceFormulaCache(t *testing.T, data []byte, wrong string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	injected := false
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && bytes.Contains(body, []byte("<f>")) {
			start := bytes.Index(body, []byte("<f>"))
			end := bytes.Index(body[start:], []byte("</f>"))
			if start >= 0 && end >= 0 {
				end += start + len("</f>")
				after := body[end:]
				if bytes.HasPrefix(after, []byte("<v>")) {
					close := bytes.Index(after, []byte("</v>"))
					if close >= 0 {
						body = append(append(append([]byte{}, body[:end]...), []byte("<v>"+wrong+"</v>")...), after[close+len("</v>"):]...)
						injected = true
					}
				} else {
					body = append(append(append([]byte{}, body[:end]...), []byte("<v>"+wrong+"</v>")...), after...)
					injected = true
				}
			}
		}
		w, err := zw.CreateHeader(&f.FileHeader)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if !injected {
		t.Fatal("no formula cache injected")
	}
	return out.Bytes()
}

func TestRecalculation(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	spec, err := content.X03SumChartSpec()
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Generate(ctx, task.ID, "合计.xlsx", spec, "recalc-source")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := svc.RecalculateWorkbook(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status == "matched" || rec.Status == "passed" {
		t.Fatalf("missing LibreOffice must not claim a matched recalc: %#v", rec)
	}
	if rec.Status != "unknown" {
		t.Fatalf("status=%q want unknown when renderer is absent", rec.Status)
	}
	if rec.VersionSHA != v.SHA256 || rec.Supported && rec.OracleValue == "" {
		t.Fatalf("receipt missing identity/oracle: %#v", rec)
	}

	var insp content.Inspection
	_, src, err := svc.ReadVersion(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	insp, err = content.Inspect(content.XLSX, src)
	if err != nil {
		t.Fatal(err)
	}
	partDigest := ""
	for _, p := range insp.Parts {
		if p.Name == "xl/worksheets/sheet1.xml" {
			partDigest = p.SHA256
		}
	}
	patched, err := svc.Patch(ctx, task.ID, v.ID, 1, content.PatchRequest{
		Kind: content.XLSX, BaseSHA256: v.SHA256,
		Ranges: []content.RangePatch{{
			Part: "xl/worksheets/sheet1.xml", Range: "B2:B5", ExpectedDigest: partDigest,
			Rows: [][]content.Cell{{{Type: "number", Value: "11"}}, {{Type: "number", Value: "22"}}, {{Type: "number", Value: "33"}}, {{Type: "number", Value: "44"}}},
		}},
	}, "x03-110")
	if err != nil {
		t.Fatal(err)
	}
	_, src, err = svc.ReadVersion(ctx, task.ID, patched.ID)
	if err != nil {
		t.Fatal(err)
	}
	wrong := injectServiceFormulaCache(t, src, "100")
	imported, err := svc.Import(ctx, task.ID, patched.ArtifactID, patched.Name, wrong, patched.ID, 2, "stale-cache")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := svc.RecalculateWorkbook(ctx, task.ID, imported.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != "mismatch" {
		t.Fatalf("injected cache 100 vs oracle 110: %#v", stale)
	}

	unsupported, err := svc.Generate(ctx, task.ID, "未知函数.xlsx", content.Spec{
		SchemaVersion: 2, Kind: content.XLSX, Title: "未知函数",
		Sheets: []content.Sheet{{Name: "数据", Rows: [][]content.Cell{
			{{Type: "number", Value: "1"}, {Type: "formula", Value: "=UNKNOWNFUNC(A1)"}},
		}}},
	}, "unknown-fn")
	if err != nil {
		t.Fatal(err)
	}
	unk, err := svc.RecalculateWorkbook(ctx, task.ID, unsupported.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unk.Status != "unknown" || unk.Supported {
		t.Fatalf("unsupported function must stay unknown and block formal calc: %#v", unk)
	}
	if !strings.Contains(strings.ToLower(unk.Notice), "unknown") && !strings.Contains(unk.Notice, "不支持") {
		t.Fatalf("notice: %q", unk.Notice)
	}
}
