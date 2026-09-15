package officeapp

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/doctext"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestPDFParseAllPagesAndSameSource(t *testing.T) {
	headerEOF := []byte("%PDF-1.4\n%%EOF\n")
	t.Run("headerEOFCannotPassParseOrCoverage", func(t *testing.T) {
		assertPDFCannotPassParseOrCoverage(t, headerEOF, "header+EOF fixture")
	})

	t.Run("f01MultiPageGeneratedPDF", func(t *testing.T) {
		body := strings.Repeat("跨页正文行，用于验收每一页都被解析，不得只读第一页。\n", 80)
		spec := content.Spec{SchemaVersion: 1, Kind: content.PDF, Title: "多页解析", Body: body}
		data, err := content.Generate(spec)
		if err != nil {
			t.Fatal(err)
		}
		extracted, err := doctext.ExtractPDFPages(data)
		if err != nil || len(extracted) < 2 {
			t.Fatalf("generated fixture must have >=2 pages: n=%d err=%v", len(extracted), err)
		}
		i, err := content.Inspect(content.PDF, data)
		if err != nil {
			t.Fatal(err)
		}
		pages := pdfPageOrdinates(i)
		if len(pages) != len(extracted) || !consistentPageList(pages) {
			t.Fatalf("inspect must list every page from the page tree, got %v want %d pages", pages, len(extracted))
		}
		v, err := content.Validate(content.PDF, data)
		if err != nil {
			t.Fatal(err)
		}
		if c := findStudioCheck(v.Checks, "pdf-parse"); c.Status != "passed" {
			t.Fatalf("pdf-parse: %#v", c)
		}
		if c := findStudioCheck(v.Checks, "page-coverage"); c.Status != "passed" {
			t.Fatalf("page-coverage: %#v", c)
		}
		if c := findStudioCheck(v.Checks, "page-render"); c.ID == "" || c.Status == "passed" {
			t.Fatalf("page-render without a renderer must stay unknown/missing: %#v", c)
		}
		if c := findStudioCheck(v.Checks, "text-layer"); c.Status != "passed" {
			t.Fatalf("searchable generated PDF text-layer: %#v", c)
		}
		if len(pages) >= 2 && content.PDFPageCoverageCheck(len(pages), pages[:len(pages)-1]).Status == "passed" {
			t.Fatal("page-coverage must not pass when any page is missing")
		}
		if strings.Contains(i.Preview, "高端商用") || strings.Contains(strings.ToLower(i.Preview), "qualified") {
			t.Fatal("must not mark live qualified")
		}

		svc, _, task := studioServiceFixture(t)
		ver, err := svc.Generate(context.Background(), task.ID, "多页.pdf", spec, "f01-pdf")
		if err != nil {
			t.Fatal(err)
		}
		qa, err := svc.Check(context.Background(), task.ID, ver.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if c := findOfficeCheck(qa.Checks, "pdf-parse"); c.Status != "passed" {
			t.Fatalf("service pdf-parse: %#v", c)
		}
		if c := findOfficeCheck(qa.Checks, "page-coverage"); c.Status != "passed" {
			t.Fatalf("service page-coverage: %#v", c)
		}
		if c := findOfficeCheck(qa.Checks, "page-render"); c.ID == "" || c.Status == "passed" {
			t.Fatalf("service page-render must not pass without a per-page render: %#v", c)
		}
	})

	t.Run("damagedMiddlePageCannotPass", func(t *testing.T) {
		damaged := minimalPDF([]string{"PAGE-ONE-UNIQUE", "PAGE-TWO-UNIQUE", "PAGE-THREE-UNIQUE"}, 2)
		if !bytes.HasPrefix(damaged, []byte("%PDF-")) || !bytes.Contains(damaged[max(0, len(damaged)-4096):], []byte("%%EOF")) {
			t.Fatal("damaged fixture lost header/EOF")
		}
		assertPDFCannotPassParseOrCoverage(t, damaged, "first page valid + damaged middle page")
	})

	t.Run("f03MissingGlyphIsNotSuccess", func(t *testing.T) {
		if data, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.PDF, Title: "缺字", Body: "不能静默丢失表情😀"}); err == nil || len(data) != 0 {
			t.Fatalf("missing-glyph generated a formal PDF: bytes=%d err=%v", len(data), err)
		}
		missing := minimalPDF([]string{"uncovered-glyph-😀"}, 0)
		i, err := content.Inspect(content.PDF, missing)
		if err != nil {
			t.Fatal(err)
		}
		v, err := content.Validate(content.PDF, missing)
		if err != nil {
			t.Fatal(err)
		}
		font := findStudioCheck(v.Checks, "font-coverage")
		if font.ID == "" {
			t.Fatal("font-coverage check missing")
		}
		if font.Status == "passed" {
			t.Fatalf("F03 missing-glyph marked font-coverage passed: %#v", font)
		}
		if font.Status != "failed" && font.Status != "unknown" && font.Status != "missing" {
			t.Fatalf("missing-glyph must be failed/unknown/missing: %#v", font)
		}
		if v.Status == "passed" {
			t.Fatal("missing-glyph validation marked passed")
		}
		if strings.Contains(strings.ToLower(font.Message+i.Preview), "passed with tofu") {
			t.Fatal("passed with tofu")
		}

		svc, _, task := studioServiceFixture(t)
		ver, err := svc.Import(context.Background(), task.ID, "", "缺字.pdf", missing, "", 0, "f03-glyph")
		if err != nil {
			t.Fatal(err)
		}
		qa, err := svc.Check(context.Background(), task.ID, ver.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if qa.Quality == "passed" {
			t.Fatal("missing-glyph marked green delivery")
		}
		if c := findOfficeCheck(qa.Checks, "font-coverage"); c.Status == "passed" {
			t.Fatalf("service font-coverage passed: %#v", c)
		}
	})

	t.Run("f02SameSourceBindsVersionSHAAndStales", func(t *testing.T) {
		svc, store, task := studioServiceFixture(t)
		ctx := context.Background()
		rows := [][]string{{"编号", "标题", "说明"}}
		for n := 1; n <= 8; n++ {
			rows = append(rows, []string{fmt.Sprintf("%d", n), "条目" + fmt.Sprintf("%d", n), "长表单元格"})
		}
		spec, err := content.WordLongTableSpec("客户方案长表", rows)
		if err != nil {
			t.Fatal(err)
		}
		v, err := svc.Generate(ctx, task.ID, "长表.docx", spec, "f02-docx")
		if err != nil {
			t.Fatal(err)
		}
		pdf, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.PDF, Title: "同源阅读稿", Body: "独立 PDF 只承诺已支持的 title/body 文本，不把长表当成隐式表格。"})
		if err != nil {
			t.Fatal(err)
		}
		bind := content.BindSameSourcePDF(content.DOCX, v.SHA256, pdf)
		if bind.SourceSHA256 != v.SHA256 || bind.PDFSHA256 == "" || bind.Stale || !bind.ValidFor(v.SHA256) {
			t.Fatalf("bind: %#v", bind)
		}
		if content.AssessSameSourcePDF(content.BindSameSourcePDF(content.DOCX, v.SHA256, headerEOF), v.SHA256, headerEOF).Status == "passed" {
			t.Fatal("header+EOF same-source must not pass")
		}
		if content.AssessSameSourcePDF(bind, v.SHA256, pdf).Status != "passed" {
			t.Fatal("valid DOCX same-source PDF must pass")
		}

		next, err := svc.Import(ctx, task.ID, v.ArtifactID, v.Name, mustGenerateWordEdit(t, spec), v.ID, 1, "f02-edit")
		if err != nil {
			t.Fatal(err)
		}
		if next.SHA256 == v.SHA256 {
			t.Fatal("source SHA did not change")
		}
		stale := content.InvalidateSameSourcePDF(bind, next.SHA256)
		if !stale.Stale || stale.ValidFor(next.SHA256) {
			t.Fatalf("edited source must stale the bound PDF without auto-rebind: %#v", stale)
		}
		if stale.SourceSHA256 == next.SHA256 {
			t.Fatal("old PDF bytes must not be auto-attached to the new source SHA")
		}
		if content.AssessSameSourcePDF(bind, next.SHA256, pdf).Status == "passed" {
			t.Fatal("export path treated stale PDF as valid")
		}

		_, err = store.AddOfficeValidation(ctx, domain.Validation{
			VersionID: next.ID, SHA256: next.SHA256, Validator: "test", Quality: "partial",
			Evidence: encode(map[string]any{"pdfRef": strings.Repeat("a", 64), "sameSourcePdf": bind}),
		})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := svc.Preview(ctx, task.ID, next.ID)
		if err != nil || preview.PDFReady {
			t.Fatalf("stale same-source PDF marked ready: %#v %v", preview, err)
		}
		if svc.SameSourceExport(ctx, next) {
			t.Fatal("stale same-source export still treated as current")
		}
		if _, err = svc.ReadPDF(ctx, task.ID, next.ID); err == nil {
			t.Fatal("stale same-source PDF still readable")
		}
	})
}

func assertPDFCannotPassParseOrCoverage(t *testing.T, data []byte, label string) {
	t.Helper()
	i, err := content.Inspect(content.PDF, data)
	v, _ := content.Validate(content.PDF, data)
	if parseOrCoveragePassed(v) {
		t.Fatalf("%s validate passed pdf-parse/page-coverage: %#v", label, v.Checks)
	}
	if err == nil {
		parse := findStudioCheck(v.Checks, "pdf-parse")
		cover := findStudioCheck(v.Checks, "page-coverage")
		if parse.ID == "" && cover.ID == "" {
			t.Fatalf("%s inspect succeeded on a header/EOF-class file without honest parse/coverage checks", label)
		}
		if parse.Status == "passed" || cover.Status == "passed" {
			t.Fatalf("%s parse/coverage passed: parse=%#v cover=%#v", label, parse, cover)
		}
		if len(pdfPageOrdinates(i)) > 0 && (parse.Status == "passed" || cover.Status == "passed") {
			t.Fatalf("%s listed pages and passed coverage: pages=%v", label, pdfPageOrdinates(i))
		}
	}
}

func parseOrCoveragePassed(v content.Validation) bool {
	return findStudioCheck(v.Checks, "pdf-parse").Status == "passed" || findStudioCheck(v.Checks, "page-coverage").Status == "passed"
}

func findStudioCheck(checks []content.Check, id string) content.Check {
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	return content.Check{}
}

func pdfPageOrdinates(i content.Inspection) []int {
	var pages []int
	for _, n := range i.Nodes {
		if n.Kind == "page" {
			pages = append(pages, n.Ordinal)
		}
	}
	return pages
}

func consistentPageList(pages []int) bool {
	if len(pages) == 0 {
		return false
	}
	one, zero := true, true
	for i, p := range pages {
		if p != i+1 {
			one = false
		}
		if p != i {
			zero = false
		}
	}
	return one || zero
}

func mustGenerateWordEdit(t *testing.T, spec content.Spec) []byte {
	t.Helper()
	spec.Title = spec.Title + "·修订"
	spec.Blocks = append(spec.Blocks, content.Block{Type: "paragraph", Text: "源文件已变化，旧同源 PDF 必须失效。"})
	data, err := content.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// minimalPDF writes an uncompressed page tree. brokenPage is 1-based; 0 means all pages are valid.
func minimalPDF(pageTexts []string, brokenPage int) []byte {
	n := len(pageTexts)
	pageObj := make([]int, n)
	contentObj := make([]int, n)
	kids := make([]string, n)
	for i := range pageTexts {
		pageObj[i] = 3 + i
		contentObj[i] = 3 + n + i
		kids[i] = fmt.Sprintf("%d 0 R", pageObj[i])
	}
	fontObj := 3 + 2*n
	built := make([]string, fontObj+1)
	built[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	built[2] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	for i, text := range pageTexts {
		built[pageObj[i]] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", contentObj[i], fontObj)
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", pdfLiteral(text))
		if brokenPage == i+1 {
			stream = "THIS IS NOT A VALID PAGE CONTENT STREAM"
			built[pageObj[i]] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 99 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", fontObj)
		}
		built[contentObj[i]] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	}
	built[fontObj] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	var body bytes.Buffer
	body.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(built))
	for id := 1; id < len(built); id++ {
		offsets[id] = body.Len()
		fmt.Fprintf(&body, "%d 0 obj\n%s\nendobj\n", id, built[id])
	}
	xrefAt := body.Len()
	fmt.Fprintf(&body, "xref\n0 %d\n", len(built))
	body.WriteString("0000000000 65535 f \n")
	for id := 1; id < len(built); id++ {
		fmt.Fprintf(&body, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&body, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(built), xrefAt)
	return body.Bytes()
}

func pdfLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}
