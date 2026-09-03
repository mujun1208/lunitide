package doctext_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
)

func TestExtractMarkdownKeepsHeadingMedia(t *testing.T) {
	res, err := doctext.Extract("/abs/manual.md", []byte("# 标题\n\nATA 32 正文"), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Media != "text/markdown" || res.Kind != "markdown" {
		t.Fatalf("markdown routing = %+v", res)
	}
	if !strings.Contains(res.Text, "ATA 32") {
		t.Fatalf("markdown body lost: %q", res.Text)
	}
}

func TestExtractPlainText(t *testing.T) {
	res, err := doctext.Extract("/abs/notes.txt", []byte("plain body line"), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Media != "text/plain" || res.Kind != "plain" {
		t.Fatalf("plain routing = %+v", res)
	}
}

func TestExtractDocxRoundTrip(t *testing.T) {
	body := "ATA 32 起落架收放系统排故程序：确认液压压力、检查作动筒、复位限位开关，并按 AMM 42 修订记录签署放行。"
	data, err := officetools.GenDocx("维护手册 起落架章节", []officetools.DocxBlock{
		{Type: "heading", Text: "第一章 起落架排故"},
		{Type: "paragraph", Text: body},
		{Type: "paragraph", Text: "Gear retraction isolation steps for the landing gear."},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := doctext.Extract("/abs/manual.docx", data, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "docx" || res.Media != "text/plain" {
		t.Fatalf("docx routing = %+v", res)
	}
	for _, want := range []string{"起落架", "复位限位开关", "Gear retraction"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("docx text missing %q in %q", want, res.Text)
		}
	}
}

func TestExtractPptxRoundTrip(t *testing.T) {
	data, err := officetools.GenPptx("机务培训", []officetools.SlideSpec{
		{Title: "起落架排故", Bullets: []string{"检查液压压力", "复位限位开关", "Landing gear check"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := doctext.Extract("/abs/deck.pptx", data, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "pptx" {
		t.Fatalf("pptx routing = %+v", res)
	}
	if !strings.Contains(res.Text, "复位限位开关") {
		t.Fatalf("pptx text missing bullet: %q", res.Text)
	}
}

func TestExtractXlsxRoundTrip(t *testing.T) {
	data, err := officetools.GenXLSX([]officetools.SheetSpec{{
		Name:    "库存",
		Headers: []string{"件号", "库位", "数量"},
		Rows:    [][]interface{}{{"PN-1234", "A1", 5}, {"PN-5678", "B2", 3}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := doctext.Extract("/abs/stock.xlsx", data, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "xlsx" {
		t.Fatalf("xlsx routing = %+v", res)
	}
	for _, want := range []string{"件号", "PN-1234", "PN-5678"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("xlsx text missing %q in %q", want, res.Text)
		}
	}
}

func TestExtractPdfTextLayer(t *testing.T) {
	data, err := officetools.GenPDF("Lunitide MRO Manual",
		"Gear retraction isolation procedure. Reset the limit switch and sign off per AMM revision.")
	if err != nil {
		t.Fatal(err)
	}
	res, err := doctext.Extract("/abs/manual.pdf", data, "")
	if err != nil {
		t.Fatalf("pdf extract: %v", err)
	}
	if res.Kind != "pdf" {
		t.Fatalf("pdf routing = %+v", res)
	}
	// PDF extractors may space glyphs unpredictably; compare with whitespace
	// removed so a valid text layer still matches the source words.
	norm := strings.Join(strings.Fields(res.Text), "")
	for _, want := range []string{"retraction", "AMM"} {
		if !strings.Contains(norm, want) {
			t.Fatalf("pdf text missing %q in %q", want, res.Text)
		}
	}
}

func TestExtractSniffsMislabelledOoxml(t *testing.T) {
	data, err := officetools.GenDocx("换标题", []officetools.DocxBlock{
		{Type: "heading", Text: "标题"},
		{Type: "paragraph", Text: "这一段正文需要足够长以便顺利通过文档生成的正文校验规则，并且能够被解析器完整地抽取出来作为可检索的知识内容片段。"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Wrong extension and no media hint: magic-byte sniffing must still route
	// the OOXML container to the docx parser.
	res, err := doctext.Extract("/abs/manual.bin", data, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "docx" {
		t.Fatalf("sniff kind = %q want docx", res.Kind)
	}
}

func TestExtractRejectsBinary(t *testing.T) {
	_, err := doctext.Extract("/abs/logo.bin", []byte("\x00\x01\x02 binary \x00 payload"), "")
	if !errors.Is(err, doctext.ErrUnsupportedFormat) {
		t.Fatalf("err = %v want ErrUnsupportedFormat", err)
	}
}

func TestExtractEmptyBodyFails(t *testing.T) {
	_, err := doctext.Extract("/abs/blank.md", []byte("   \n\t  \n"), "")
	if !errors.Is(err, doctext.ErrNoTextLayer) {
		t.Fatalf("err = %v want ErrNoTextLayer", err)
	}
}
