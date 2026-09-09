package officestudio

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func testDocument(t *testing.T) []byte {
	t.Helper()
	data, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "工作计划", Blocks: []Block{{Type: "heading", Text: "目标"}, {Type: "paragraph", Text: "原始文本 A & B"}, {Type: "numbered", Text: "第一项工作"}, {Type: "table", Rows: [][]string{{"名称", "编号"}, {"用户", "001234567890123456"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func zipParts(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	p, err := readPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	return p.parts
}

func editZIP(t *testing.T, data []byte, changes map[string][]byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	seen := map[string]bool{}
	for _, f := range zr.File {
		seen[f.Name] = true
		if replacement, ok := changes[f.Name]; ok {
			w, err := zw.Create(f.Name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write(replacement); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := zw.Copy(f); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, body := range changes {
		if seen[name] {
			continue
		}
		w, err := zw.Create(name)
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
	return b.Bytes()
}

func findText(t *testing.T, i Inspection, text string) Node {
	t.Helper()
	for _, n := range i.Nodes {
		if n.Text == text {
			return n
		}
	}
	t.Fatalf("text %q missing from index: %#v", text, i.Nodes)
	return Node{}
}

func TestGenerateWordActualTableAndNumberedList(t *testing.T) {
	data := testDocument(t)
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !i.RenderAllowed || i.Editability != "partial" {
		t.Fatalf("inspection: %#v", i)
	}
	parts := zipParts(t, data)
	body := string(parts["word/document.xml"])
	if !strings.Contains(body, "<w:tbl>") || !strings.Contains(body, `<w:numId w:val="2"/>`) {
		t.Fatal("table/list converted into plain paragraphs")
	}
	if n := findText(t, i, "001234567890123456"); !n.Editable || n.Digest == "" || n.Part != "word/document.xml" {
		t.Fatalf("node lacks source mapping: %#v", n)
	}
	if _, err := Inspect(PPTX, data); err == nil {
		t.Fatal("accepted mislabeled Word as presentation")
	}
}

func TestGenerateTenPresentationLayoutsPreservesSuppliedContent(t *testing.T) {
	layouts := []string{"cover", "section", "content", "two-column", "comparison", "quote", "metrics", "timeline", "agenda", "closing"}
	spec := Spec{SchemaVersion: 1, Kind: PPTX, Title: "年度计划"}
	for n, l := range layouts {
		spec.Slides = append(spec.Slides, Slide{Layout: l, Title: fmt.Sprintf("标题%d", n), Subtitle: fmt.Sprintf("副标题%d", n), Bullets: []string{fmt.Sprintf("要点一%d", n), fmt.Sprintf("要点二%d", n)}, Notes: fmt.Sprintf("讲稿%d", n)})
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !i.RenderAllowed {
		t.Fatalf("unsafe generated deck %#v", i.Issues)
	}
	for n := range layouts {
		body := string(parts[fmt.Sprintf("ppt/slides/slide%d.xml", n+1)])
		for _, text := range []string{fmt.Sprintf("标题%d", n), fmt.Sprintf("副标题%d", n), fmt.Sprintf("要点一%d", n), fmt.Sprintf("要点二%d", n)} {
			if !strings.Contains(body, text) {
				t.Errorf("slide %d omitted %s", n+1, text)
			}
		}
		if !bytes.Contains(parts[fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n+1)], []byte(fmt.Sprintf("讲稿%d", n))) {
			t.Error("speaker notes missing")
		}
	}
	if !bytes.Contains(parts["ppt/slides/slide4.xml"], []byte("Column2")) || !bytes.Contains(parts["ppt/slides/slide7.xml"], []byte("MetricCard")) {
		t.Fatal("semantic layouts were not generated")
	}
}

func TestTypedWorkbookPreservesIdentifiersAndNeverInfersFormula(t *testing.T) {
	cells := []Cell{{Type: "text", Value: "001234567890123456789"}, {Type: "text", Value: `=WEBSERVICE("https://example.com")`}, {Type: "number", Value: "19.25", Format: "0.00"}, {Type: "boolean", Value: "true"}, {Type: "date", Value: "2026-09-07"}, {Type: "formula", Value: "=SUM(C1,1)"}}
	data, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "类型检查", Sheets: []Sheet{{Name: "数据", FreezeHeader: true, Rows: [][]Cell{cells}}}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for col, want := range map[string]string{"A1": cells[0].Value, "B1": cells[1].Value, "C1": "19.25", "E1": "2026-09-07"} {
		got, err := f.GetCellValue("数据", col)
		if err != nil || got != want {
			t.Errorf("%s = %q, want %q; %v", col, got, want, err)
		}
	}
	for _, col := range []string{"A1", "B1"} {
		formula, err := f.GetCellFormula("数据", col)
		if err != nil || formula != "" {
			t.Fatalf("text executed as formula at %s", col)
		}
	}
	formula, err := f.GetCellFormula("数据", "F1")
	if err != nil || formula != "SUM(C1,1)" {
		t.Fatalf("explicit formula lost: %q %v", formula, err)
	}
	inspection, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	_ = findText(t, inspection, "=SUM(C1,1)")
	v, err := Validate(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "partial" {
		t.Fatalf("formula evaluation falsely verified: %#v", v)
	}
	found := false
	for _, c := range v.Checks {
		if c.ID == "full_recalculation" && c.Status == "missing" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing full calculation coverage warning")
	}
}

func TestInvalidSpecAndUnsafeFormulasFail(t *testing.T) {
	for _, cell := range []Cell{{Type: "number", Value: "123456789012345678"}, {Type: "number", Value: "NaN"}, {Type: "number", Value: "01"}, {Type: "formula", Value: `=WEBSERVICE("https://example.com")`}, {Type: "formula", Value: "='cmd|/C calc'!A0"}, {Type: "formula", Value: "=[external.xlsx]Sheet1!A1"}, {Type: "date", Value: "07/09/2026"}, {Type: "boolean", Value: "yes"}, {Type: "blank", Value: "hidden"}, {Type: "unknown", Value: "1"}} {
		_, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "拒绝", Sheets: []Sheet{{Name: "数据", Rows: [][]Cell{{cell}}}}})
		if err == nil {
			t.Errorf("accepted %#v", cell)
		}
	}
	if _, err := Generate(Spec{SchemaVersion: 2, Kind: PDF, Title: "不支持", Body: "正文"}); err == nil {
		t.Fatal("accepted future schema")
	}
	if _, err := Generate(Spec{SchemaVersion: 1, Kind: PPTX, Title: "不支持", Slides: []Slide{{Title: "标题", Layout: "invented"}}}); err == nil {
		t.Fatal("silently changed unsupported layout")
	}
}

func TestPatchPreservesUnknownPartsStylesAndUnrelatedNodes(t *testing.T) {
	data := testDocument(t)
	opaque := []byte{0, 1, 2, 3, 0xF0, 0xFF}
	data = editZIP(t, data, map[string][]byte{"custom/preserved.dat": opaque})
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	node := findText(t, i, "原始文本 A & B")
	result, err := Patch(data, PatchRequest{Kind: DOCX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: node.ID, ExpectedDigest: node.Digest, Text: "  修改后的 <文本> & 第二行\n结束  "}}})
	if err != nil {
		t.Fatal(err)
	}
	before, after := zipParts(t, data), zipParts(t, result.Data)
	for name, part := range before {
		if name != "word/document.xml" && !bytes.Equal(part, after[name]) {
			t.Errorf("unrelated part changed: %s", name)
		}
	}
	changed := findText(t, result.Inspection, "  修改后的 <文本> & 第二行\n结束  ")
	if changed.ID != node.ID || changed.Digest == node.Digest {
		t.Fatal("node identity/digest contract failed")
	}
	if len(result.ChangedParts) != 1 || result.ChangedParts[0] != "word/document.xml" {
		t.Fatal(result.ChangedParts)
	}
	if !bytes.Equal(after["custom/preserved.dat"], opaque) {
		t.Fatal("opaque part lost")
	}
	if _, err := Patch(result.Data, PatchRequest{Kind: DOCX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: node.ID, ExpectedDigest: node.Digest, Text: "stale"}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version not rejected: %v", err)
	}
	if _, err := Patch(data, PatchRequest{Kind: DOCX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: node.ID, ExpectedDigest: "incorrect", Text: "stale"}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale node not rejected: %v", err)
	}
}

func TestSharedStringPatchChangesOnlyTargetCell(t *testing.T) {
	data, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "共享文本", Sheets: []Sheet{{Name: "数据", Rows: [][]Cell{{{Type: "text", Value: "共同文本"}, {Type: "text", Value: "共同文本"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	var target Node
	for _, n := range i.Nodes {
		if n.Locator == "cell:A1" {
			target = n
		}
	}
	result, err := Patch(data, PatchRequest{Kind: XLSX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: target.ID, ExpectedDigest: target.Digest, Text: "=不会执行"}}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a, _ := f.GetCellValue("数据", "A1")
	b, _ := f.GetCellValue("数据", "B1")
	formula, _ := f.GetCellFormula("数据", "A1")
	if a != "=不会执行" || b != "共同文本" || formula != "" {
		t.Fatalf("shared string patch bled across cells: %q %q %q", a, b, formula)
	}
	before, after := zipParts(t, data), zipParts(t, result.Data)
	if !bytes.Equal(before["xl/sharedStrings.xml"], after["xl/sharedStrings.xml"]) {
		t.Fatal("shared dictionary unnecessarily changed")
	}
}

func TestBlockedActiveContentAndBrokenRelationships(t *testing.T) {
	original := testDocument(t)
	parts := zipParts(t, original)
	cases := map[string]map[string][]byte{
		"macro":    {"word/vbaProject.bin": []byte("macro")},
		"embedded": {"word/embeddings/object.bin": []byte("object")},
		"signed":   {"_xmlsignatures/sig1.xml": []byte("<signature/>")},
		"external": {"word/_rels/document.xml.rels": []byte(strings.Replace(string(parts["word/_rels/document.xml.rels"]), "</Relationships>", `<Relationship Id="rId900" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/></Relationships>`, 1))},
		"missing":  {"word/_rels/document.xml.rels": []byte(strings.Replace(string(parts["word/_rels/document.xml.rels"]), `Target="styles.xml"`, `Target="missing.xml"`, 1))},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			data := editZIP(t, original, change)
			i, err := Inspect(DOCX, data)
			if err != nil {
				t.Fatal(err)
			}
			if i.RenderAllowed || i.Editability != "blocked" {
				t.Fatalf("active/broken content allowed: %#v", i)
			}
			for _, n := range i.Nodes {
				if n.Editable {
					t.Fatal("blocked document left an editable node")
				}
			}
			v, err := Validate(DOCX, data)
			if err != nil || v.Status != "blocked" {
				t.Fatalf("QA says safe: %#v %v", v, err)
			}
		})
	}
}

func TestArchiveAndXMLAttackInputsRejected(t *testing.T) {
	base := testDocument(t)
	for _, name := range []string{"../escape.xml", "/absolute.xml", `word\escape.xml`, "C:/escape.xml", "word/./ambiguous.xml"} {
		t.Run(name, func(t *testing.T) {
			if _, err := Inspect(DOCX, editZIP(t, base, map[string][]byte{name: []byte("<x/>")})); err == nil {
				t.Fatal("unsafe entry accepted")
			}
		})
	}
	for _, body := range []string{`<!DOCTYPE x [<!ENTITY x SYSTEM "file:///secret">]><x>&x;</x>`, `<x/><y/>`, `<x>` + strings.Repeat(`<n>`, 130) + strings.Repeat(`</n>`, 130) + `</x>`} {
		if _, err := Inspect(DOCX, editZIP(t, base, map[string][]byte{"custom/test.xml": []byte(body)})); err == nil {
			t.Fatal("unsafe XML accepted")
		}
	}
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []fs.FileMode{fs.ModeSymlink | 0600, fs.ModeNamedPipe | 0600} {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		for _, f := range zr.File {
			if err := zw.Copy(f); err != nil {
				t.Fatal(err)
			}
		}
		h := &zip.FileHeader{Name: "custom/link"}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, "outside")
		_ = zw.Close()
		if _, err := Inspect(DOCX, b.Bytes()); err == nil {
			t.Fatal("non-regular entry accepted")
		}
	}
	if _, err := Inspect(DOCX, editZIP(t, base, map[string][]byte{"WORD/DOCUMENT.XML": []byte("<x/>")})); err == nil {
		t.Fatal("case-colliding package accepted")
	}
	if _, err := Inspect(DOCX, bytes.Repeat([]byte{'X'}, MaxInputBytes+1)); !errors.Is(err, ErrLimit) {
		t.Fatal("input budget not enforced")
	}
}

func TestPDFPreflightNeverPretendsCompleteValidation(t *testing.T) {
	data, err := Generate(Spec{SchemaVersion: 1, Kind: PDF, Title: "中文文档", Body: "这是用于验证中文字体的 PDF 文档。\n不执行脚本，也不访问外部链接。"})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(PDF, data)
	if err != nil {
		t.Fatal(err)
	}
	if i.Editability != "readonly" || i.RenderAllowed {
		t.Fatalf("PDF preflight overclaimed: %#v", i)
	}
	v, err := Validate(PDF, data)
	if err != nil || v.Status != "partial" {
		t.Fatalf("PDF actual rendering was not proved: %#v %v", v, err)
	}
	if _, err := Inspect(PDF, []byte("%PDF-1.4 broken")); err == nil {
		t.Fatal("truncated PDF accepted")
	}
	i, err = Inspect(PDF, []byte("%PDF-1.4 /OpenAction /JavaScript\n%%EOF"))
	if err != nil || i.Editability != "blocked" {
		t.Fatal("active PDF allowed")
	}
}

func TestManagedGenerationDeterministicAcrossRepeatedCalls(t *testing.T) {
	for _, spec := range []Spec{
		{SchemaVersion: 1, Kind: DOCX, Title: "文档", Blocks: []Block{{Type: "paragraph", Text: "正文内容"}}},
		{SchemaVersion: 1, Kind: PPTX, Title: "演示", Slides: []Slide{{Title: "标题", Layout: "cover", Subtitle: "摘要"}}},
		{SchemaVersion: 1, Kind: XLSX, Title: "表格", Sheets: []Sheet{{Name: "数据", Rows: [][]Cell{{{Type: "text", Value: "中文"}, {Type: "number", Value: "12"}}}}}},
		{SchemaVersion: 1, Kind: PDF, Title: "中文PDF", Body: "可重复执行的正文。"},
	} {
		t.Run(string(spec.Kind), func(t *testing.T) {
			a, err := Generate(spec)
			if err != nil {
				t.Fatal(err)
			}
			for range 3 {
				b, err := Generate(spec)
				if err != nil || !bytes.Equal(a, b) {
					t.Fatalf("same spec produced different digest: %s / %s (%v)", digest(a), digest(b), err)
				}
			}
		})
	}
}

func TestRichSharedTextCannotBePatchedIntoPlainString(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellRichText("Sheet1", "A1", []excelize.RichTextRun{{Text: "保留", Font: &excelize.Font{Bold: true}}, {Text: "格式", Font: &excelize.Font{Italic: true}}}); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(XLSX, b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	n := findText(t, i, "保留格式")
	if n.Editable || n.Kind != "cell:richtext" {
		t.Fatalf("rich text exposed destructive plain patch: %#v", n)
	}
	if _, err := Patch(b.Bytes(), PatchRequest{Kind: XLSX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: "修改"}}}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("rich text changed despite unsupported style preservation: %v", err)
	}
}

func TestDynamicWordFieldsNeverReachNativeRenderer(t *testing.T) {
	base := testDocument(t)
	parts := zipParts(t, base)
	for _, field := range []string{
		`<w:p><w:r><w:instrText>DDEAUTO cmd /c calc</w:instrText></w:r></w:p>`,
		`<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>DD</w:instrText></w:r><w:r><w:instrText>EAUTO cmd /c calc</w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`,
		`<w:p><w:fldSimple w:instr="INCLUDETEXT &quot;https://example.com/a.docx&quot;"><w:r><w:t>Text</w:t></w:r></w:fldSimple></w:p>`,
	} {
		data := editZIP(t, base, map[string][]byte{"word/document.xml": []byte(strings.Replace(string(parts["word/document.xml"]), "</w:body>", field+"</w:body>", 1))})
		i, err := Inspect(DOCX, data)
		if err != nil {
			t.Fatal(err)
		}
		if i.RenderAllowed || i.Editability != "blocked" {
			t.Fatalf("dynamic field allowed: %#v", i)
		}
	}
}

func TestProtectedDocumentRemainsReadonlyButCanRender(t *testing.T) {
	base := testDocument(t)
	parts := zipParts(t, base)
	data := editZIP(t, base, map[string][]byte{"word/settings.xml": []byte(strings.Replace(string(parts["word/settings.xml"]), "</w:settings>", `<w:documentProtection w:edit="readOnly" w:enforcement="1"/></w:settings>`, 1))})
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !i.RenderAllowed || i.Editability != "readonly" {
		t.Fatalf("protection lost: %#v", i)
	}
	for _, n := range i.Nodes {
		if n.Editable {
			t.Fatal("protected document exposed mutable node")
		}
	}
}
