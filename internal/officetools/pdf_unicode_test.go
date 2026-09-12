package officetools

import (
	"bytes"
	"compress/zlib"
	"io"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/lunitide/lunitide/internal/doctext"
)

func TestGenPDFChineseEmbeddedFontRoundtrip(t *testing.T) {
	title := "中文会议纪要 / Meeting 2026"
	line := "第一章：准确读取与完整生成，中文、繁體、English、编号 000123，金额 €120.50。生成结果需要能够打开、读取和核对，不得在换行的位置丢掉任何一个汉字。"
	body := strings.Repeat(line+"\n", 65) + "末尾唯一验收标记：任务已完成。"
	data, err := GenPDF(title, body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("/FontFile2")) || !bytes.Contains(data, []byte("/ToUnicode")) || len(data) > 512<<10 {
		t.Fatalf("font missing or PDF failed to subset it: %d bytes", len(data))
	}
	extracted, err := doctext.Extract("chinese.pdf", data, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{title, "末尾唯一验收标记：任务已完成。", "繁體", "000123"} {
		if !strings.Contains(extracted.Text, want) {
			t.Fatalf("Chinese PDF lost %q in extracted text: %.200s", want, extracted.Text)
		}
	}
	if strings.Count(extracted.Text, "第一章") != 65 {
		t.Fatalf("multi-page document lost lines: %d", strings.Count(extracted.Text, "第一章"))
	}
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	if compact(extracted.Text) != compact(title+body) {
		t.Fatal("line wrapping dropped or duplicated authored PDF characters")
	}
}

func TestGenPDFRejectsUnsupportedGlyphInsteadOfUnreadableFile(t *testing.T) {
	for _, body := range []string{"不能静默丢失表情😀", "超出当前字体范围𠮷", "非法控制\x00", "非法编码\xff"} {
		if data, err := GenPDF("中文", body); err == nil || len(data) != 0 {
			t.Fatalf("unsupported text produced a misleading file: %q bytes=%d err=%v", body, len(data), err)
		}
	}
	if data, err := GenPDF("Old English API", "Latin text remains available."); err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("existing English API broken: %v", err)
	}
}

func TestGenPDFUnicodeConcurrentCalls(t *testing.T) {
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Go(func() {
			data, err := GenPDF("并发中文生成", "各任务使用独立 PDF 实例和内置字体子集，互不污染。")
			if err != nil || len(data) == 0 {
				t.Errorf("concurrent PDF failed: %v", err)
			}
		})
	}
	group.Wait()
}

func TestGenStablePDFThemedWritesHeadingColor(t *testing.T) {
	data, err := GenStablePDFThemed("月报", "封面\n订单 1280单", PDFTheme{Heading: "AA1122"})
	if err != nil {
		t.Fatal(err)
	}
	text := inflatedPDFStreams(t, data)
	if !strings.Contains(text, "0.667 0.067 0.133") {
		t.Fatalf("themed heading RGB missing: %q", text)
	}
	if bytes.Contains(data, []byte("PDF/A")) && bytes.Contains(data, []byte("已符合")) {
		t.Fatal("fallback PDF claimed PDF/A")
	}
}

func inflatedPDFStreams(t *testing.T, data []byte) string {
	t.Helper()
	var b strings.Builder
	rest := data
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			break
		}
		rest = rest[i+6:]
		if len(rest) >= 2 && rest[0] == '\r' && rest[1] == '\n' {
			rest = rest[2:]
		} else if len(rest) >= 1 && (rest[0] == '\n' || rest[0] == '\r') {
			rest = rest[1:]
		}
		j := bytes.Index(rest, []byte("endstream"))
		if j < 0 {
			break
		}
		raw := bytes.TrimSpace(rest[:j])
		rest = rest[j+9:]
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		out, err := io.ReadAll(zr)
		_ = zr.Close()
		if err == nil {
			b.Write(out)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func TestIndependentPDFHeadingRecognizesReportSectionsOnly(t *testing.T) {
	if !IndependentPDFHeading("封面") || !IndependentPDFHeading("目录") || !IndependentPDFHeading("正文") || !IndependentPDFHeading("引用") {
		t.Fatal("structured report headings rejected")
	}
	if IndependentPDFHeading("订单 1280单") || IndependentPDFHeading("节约了") {
		t.Fatal("body or invented line treated as heading")
	}
}
