package officestudio

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
)

func PDFDeliveryChecks(data []byte) []Check {
	pages, err := doctext.ExtractPDFPages(data)
	parse := Check{ID: "pdf-parse", Status: "failed", Message: "未能从真实页树解析 PDF"}
	if err != nil {
		parse.Message = err.Error()
	} else if len(pages) == 0 {
		parse.Status = "missing"
		parse.Message = "页树没有页面"
	} else if doctext.PDFPagesParseFailed(pages) {
		parse.Message = "页树中有无法解析的页面，不能因为首页可读而通过"
	} else {
		parse.Status = "passed"
		parse.Message = fmt.Sprintf("已按页树解析 %d 页", len(pages))
	}

	listed := make([]int, 0, len(pages))
	for _, page := range pages {
		listed = append(listed, page.Page)
	}
	cover := PDFPageCoverageCheck(len(pages), listed)
	if parse.Status != "passed" && cover.Status == "passed" {
		cover.Status = "failed"
		cover.Message = "页解析未完成，拒绝全量覆盖"
	}

	render := Check{ID: "page-render", Status: "missing", Message: "尚无逐页实际渲染证据；解析成功不能当作渲染通过。"}

	text := Check{ID: "text-layer", Status: "missing", Message: "文字层不可验证"}
	if parse.Status == "passed" {
		if pdfPagesHaveText(pages) {
			text.Status = "passed"
			text.Message = fmt.Sprintf("可搜索文字层覆盖 %d 页", len(pages))
		} else {
			text.Status = "missing"
			text.Message = "存在无文字层的页面；空页不能当作已解析成功"
		}
	}

	return []Check{parse, cover, render, text, PDFFontCoverageCheck(data, pages)}
}

func PDFPageCoverageCheck(expected int, listed []int) Check {
	if expected < 1 {
		return Check{ID: "page-coverage", Status: "failed", Message: "页树没有页面，拒绝全量标记"}
	}
	if !pdfPageListComplete(listed, expected) {
		return Check{ID: "page-coverage", Status: "failed", Message: "页覆盖不完整，拒绝全量标记"}
	}
	return Check{ID: "page-coverage", Status: "passed", Message: fmt.Sprintf("页覆盖 %d..%d", listed[0], listed[len(listed)-1])}
}

func pdfPageListComplete(listed []int, expected int) bool {
	if expected < 1 || len(listed) != expected {
		return false
	}
	one, zero := true, true
	for i, page := range listed {
		if page != i+1 {
			one = false
		}
		if page != i {
			zero = false
		}
	}
	return one || zero
}

func pdfPagesHaveText(pages []doctext.PDFPageText) bool {
	if len(pages) == 0 {
		return false
	}
	for _, page := range pages {
		if page.ParseFailed || strings.TrimSpace(page.Text) == "" {
			return false
		}
	}
	return true
}

func PDFFontCoverageCheck(data []byte, pages []doctext.PDFPageText) Check {
	text := pdfGlyphSourceText(data, pages)
	if err := officetools.CheckPDFTextGlyphs(text); err != nil {
		return Check{ID: "font-coverage", Status: "failed", Message: err.Error()}
	}
	if strings.TrimSpace(text) == "" {
		return Check{ID: "font-coverage", Status: "missing", Message: "无抽取文本，字形覆盖不可验证"}
	}
	return Check{ID: "font-coverage", Status: "passed", Message: "抽取文本均在内置字体覆盖范围内"}
}

func pdfGlyphSourceText(data []byte, pages []doctext.PDFPageText) string {
	var b strings.Builder
	for _, page := range pages {
		b.WriteString(page.Text)
	}
	if !bytes.Contains(data, []byte("/Filter")) && !bytes.Contains(data, []byte("FlateDecode")) {
		if extra := pdfParenStrings(data); extra != "" && utf8.ValidString(extra) {
			b.WriteString(extra)
		}
	}
	return b.String()
}

func pdfParenStrings(data []byte) string {
	var b strings.Builder
	in, esc := false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !in {
			if c == '(' {
				in = true
			}
			continue
		}
		if esc {
			b.WriteByte(c)
			esc = false
			continue
		}
		if c == '\\' {
			esc = true
			continue
		}
		if c == ')' {
			in = false
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
