package officerender_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
	"github.com/lunitide/lunitide/internal/officerender"
	"github.com/lunitide/lunitide/internal/officestudio"
)

// Opt in to an existing or administratively extracted LibreOffice executable.
// The test never installs anything and only opens freshly generated TempDir data.
func TestOfficeNativeLibreOfficeUpdatesSyntheticDocuments(t *testing.T) {
	executable := os.Getenv("OFFICE_STUDIO_TEST_LIBREOFFICE")
	if executable == "" {
		t.Skip("set OFFICE_STUDIO_TEST_LIBREOFFICE to run actual native update fixtures")
	}
	r := &officerender.Renderer{Root: t.TempDir(), Executable: executable, Preflight: func(kind string, data []byte) error {
		i, err := officestudio.Inspect(officestudio.Kind(kind), data)
		if err != nil {
			return err
		}
		if !i.RenderAllowed {
			return errors.New("fixture preflight denied native rendering")
		}
		return nil
	}}
	cases := []struct {
		name                        string
		spec                        officestudio.Spec
		formulaCells, formulaErrors int
	}{
		{"toc", officestudio.Spec{SchemaVersion: 1, Kind: officestudio.DOCX, Title: "不可进入目录的标题", Document: &officestudio.DocumentOptions{PageNumbers: true}, Blocks: []officestudio.Block{{Type: "toc"}, {Type: "heading", Text: "章节一"}, {Type: "heading2", Text: "详细方法"}, {Type: "paragraph", Text: "合成中文验证材料"}}}, 0, 0},
		{"calculate", officestudio.Spec{SchemaVersion: 1, Kind: officestudio.XLSX, Title: "合成计算", Sheets: []officestudio.Sheet{{Name: "Data", Rows: [][]officestudio.Cell{{{Type: "number", Value: "7"}, {Type: "number", Value: "11"}, {Type: "formula", Value: "=SUM(A1:B1)"}, {Type: "text", Value: "00123"}, {Type: "text", Value: "=SUM(A1:B1)"}}}}, {Name: "Other", Rows: [][]officestudio.Cell{{{Type: "formula", Value: "=Data!C1*2"}}}}}}, 2, 0},
		{"formula-error", officestudio.Spec{SchemaVersion: 1, Kind: officestudio.XLSX, Title: "合成错误公式", Sheets: []officestudio.Sheet{{Name: "Data", Rows: [][]officestudio.Cell{{{Type: "formula", Value: "=1/0"}}}}}}, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := officestudio.Generate(tc.spec)
			if err != nil {
				t.Fatal(err)
			}
			before := append([]byte{}, data...)
			result, err := r.RenderWithChecks(context.Background(), string(tc.spec.Kind), data, officerender.NativeOptions{UpdateFields: tc.spec.Kind == officestudio.DOCX, Recalculate: tc.spec.Kind == officestudio.XLSX, ExportUpdatedCopy: true})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, data) || result.Native == nil || !result.Native.SourceUnchanged || result.Native.Scope != "derived-preview" {
				t.Fatal("input changed or scope/evidence missing")
			}
			merged, mergeErr := officestudio.MergeNativeCaches(tc.spec.Kind, data, result.UpdatedOffice)
			if dir := os.Getenv("OFFICE_STUDIO_TEST_EVIDENCE_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for name, content := range map[string][]byte{tc.name + "-source." + string(tc.spec.Kind): data, tc.name + "-native." + string(tc.spec.Kind): result.UpdatedOffice, tc.name + "-preview.pdf": result.PDF} {
					if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.formulaErrors > 0 {
				if mergeErr == nil {
					t.Fatal("formula error cache was published")
				}
			} else {
				if mergeErr != nil {
					t.Fatal("actual native cache merge:", mergeErr)
				}
				if tc.spec.Kind == officestudio.XLSX && merged.UpdatedCells != tc.formulaCells {
					t.Fatal("incomplete formula merge")
				}
				beforeParts, afterParts := nativeZipParts(t, data), nativeZipParts(t, merged.Data)
				allowed := map[string]bool{}
				for _, name := range merged.ChangedParts {
					allowed[name] = true
				}
				for name, body := range beforeParts {
					if !allowed[name] && !bytes.Equal(body, afterParts[name]) {
						t.Fatalf("native merge changed unrelated %s", name)
					}
				}
				if len(beforeParts) != len(afterParts) {
					t.Fatal("native merge changed original package members")
				}
			}
			reader, err := pdf.NewReader(bytes.NewReader(result.PDF), int64(len(result.PDF)))
			if err != nil {
				t.Fatal(err)
			}
			plain, err := reader.GetPlainText()
			if err != nil {
				t.Fatal(err)
			}
			text, err := io.ReadAll(plain)
			if err != nil {
				t.Fatal(err)
			}
			if tc.spec.Kind == officestudio.DOCX {
				if !result.Native.FieldsRefreshed || result.Native.UpdatedIndexes != 1 || strings.Count(string(text), "章节一") != 2 || strings.Count(string(text), "不可进入目录的标题") != 1 || strings.Contains(string(text), "目录待实际") {
					t.Fatalf("actual TOC not correct: %+v\n%s", result.Native, text)
				}
			} else {
				if !result.Native.Recalculated || !result.Native.FormulaScanFull || result.Native.FormulaCells != tc.formulaCells || result.Native.FormulaErrorCount != tc.formulaErrors {
					t.Fatalf("formula coverage incorrect: %+v", result.Native)
				}
				if tc.name == "calculate" {
					for _, value := range []string{"18", "36", "00123", "=SUM(A1:B1)"} {
						if !strings.Contains(string(text), value) {
							t.Fatalf("missing %q in actual PDF: %s", value, text)
						}
					}
				}
				if tc.name == "formula-error" && (len(result.Native.FormulaErrors) != 1 || result.Native.FormulaErrors[0].Row != 1 || result.Native.FormulaErrors[0].Column != 1 || result.Native.FormulaErrors[0].Code == 0) {
					t.Fatal("formula error details not actual", result.Native)
				}
			}
		})
	}
}

func nativeZipParts(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range r.File {
		stream, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = body
	}
	return out
}
