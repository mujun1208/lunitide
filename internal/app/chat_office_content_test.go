package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
)

const officeFallbackProse = "# 项目介绍\n这是已经完成的产品介绍正文。系统支持本地文件读取和文档生成，保留已有操作权限，并在完成后返回能够打开的实际文件。\n# 验收结果\n正文、产物路径和处理过程需要一起保存，重新进入聊天后仍然可以查看。"

func TestOfficeFallbackWordRetainsWholeBodyAndChapterStyles(t *testing.T) {
	text := "第一章 出发\n" + strings.Repeat("她说稍等，接着翻开手中的书，把沿途的见闻认真记在纸上。", 90) + "\n\n第二章 归来\n" + strings.Repeat("旧日的街巷在远处展开，他们终于抵达出发时约定的地方。", 60) + "末尾唯一验收标记。"
	args := fallbackOfficeGenArgs("docx.gen", "写一份旅行小说Word", text)
	var spec struct {
		Title  string                  `json:"title"`
		Kind   string                  `json:"kind"`
		Author string                  `json:"author"`
		Blocks []officetools.DocxBlock `json:"blocks"`
	}
	if err := json.Unmarshal(args, &spec); err != nil {
		t.Fatal(err)
	}
	file, err := officetools.GenDocxDoc(officetools.DocxDoc{Title: spec.Title, Kind: spec.Kind, Author: spec.Author, Blocks: spec.Blocks})
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := doctext.Extract("novel.docx", file, "")
	if err != nil || !strings.Contains(extracted.Text, "第二章 归来") || !strings.Contains(extracted.Text, "末尾唯一验收标记。") || strings.Count(extracted.Text, "旧日的街巷") != 60 {
		t.Fatalf("document dropped authored text: len=%d err=%v", len(extracted.Text), err)
	}
}

func TestOfficeFallbackPPTUsesActualContentAndKeepsTail(t *testing.T) {
	text := "# 市场分析\n" + strings.Repeat("实际调查发现产品使用流程可以简化，需要保留原有功能并验证结果。", 30) + "\n# 实施方案\n先完成基础能力，再依据用户反馈调整。末页唯一验收标记。\n# 附录"
	args := fallbackOfficeGenArgs("pptx.gen", "做一份市场分析PPT", text)
	var spec struct {
		Title  string                  `json:"title"`
		Slides []officetools.SlideSpec `json:"slides"`
	}
	if err := json.Unmarshal(args, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Slides) < 3 {
		t.Fatal("long content was not paginated")
	}
	file, err := officetools.GenPptx(spec.Title, spec.Slides)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := doctext.Extract("slides.pptx", file, "")
	if err != nil || !strings.Contains(extracted.Text, "末页唯一验收标记") || !strings.Contains(extracted.Text, "附录") || strings.Contains(extracted.Text, "详见对话中的介绍要点") {
		t.Fatalf("presentation substituted or dropped content: %s err=%v", extracted.Text, err)
	}
	var joined strings.Builder
	for _, slide := range spec.Slides {
		joined.WriteString(strings.Join(slide.Bullets, ""))
	}
	if strings.Count(joined.String(), "实际调查发现") != 30 {
		t.Fatal("long slide body was truncated")
	}
}

func TestOfficeFallbackExcelUsesTableDataAndExactIdentifiers(t *testing.T) {
	text := "| 编号 | 项目 | 金额 |\n| --- | --- | ---: |\n| 000123 | A\\|B | 120.5 |\n| 210004040430100001 | 最后一项 | 99 |"
	args := fallbackOfficeGenArgs("excel.gen", "制作清单Excel", text)
	var spec struct {
		Sheets []officetools.SheetSpec `json:"sheets"`
	}
	if err := json.Unmarshal(args, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Sheets) != 1 || len(spec.Sheets[0].Rows) != 2 || spec.Sheets[0].Rows[0][0] != "000123" || spec.Sheets[0].Rows[0][1] != "A|B" || spec.Sheets[0].Rows[0][2] != 120.5 {
		t.Fatalf("table data changed: %s", args)
	}
	file, err := officetools.GenXLSX(spec.Sheets)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := doctext.Extract("table.xlsx", file, "")
	if err != nil || !strings.Contains(extracted.Text, "210004040430100001") || !strings.Contains(extracted.Text, "最后一项") || strings.Contains(extracted.Text, "由对话要点生成") {
		t.Fatalf("spreadsheet data lost: %s err=%v", extracted.Text, err)
	}
	if got := officeContentSheets("```csv\n编号,名称\n0001,真实内容\n```"); len(got) != 1 || got[0].Rows[0][0] != "0001" {
		t.Fatalf("CSV lost source rows: %+v", got)
	}
}

func TestOfficeFallbackDoesNotManufactureMissingOrOversizedContent(t *testing.T) {
	for _, name := range []string{"pptx.gen", "docx.gen", "excel.gen"} {
		for _, body := range []string{"", "好的，稍等，我来做。"} {
			if args := fallbackOfficeGenArgs(name, "制作一份文件", body); len(args) != 0 {
				t.Fatalf("%s manufactured a file: %s", name, args)
			}
		}
	}
	if len(officeContentSlides("演示文稿", strings.Repeat("一段很长的真实正文", 3000))) > 0 {
		t.Fatal("over-limit deck was silently shortened")
	}
	if len(officeContentSheets("| A | B |\n| --- | --- |\n| 缺一列 |")) > 0 {
		t.Fatal("malformed table was silently shortened")
	}
}

func TestOfficeFallbackExcelRetainsEveryTableInSourceOrder(t *testing.T) {
	text := "```csv\r\n编号,内容\r\n0001,第一份\r\n```\r\n\r\n| 编号 | 内容 |\r\n| --- | --- |\r\n| 0002 | 第二份 |\r\n\r\n```csv\r\n编号,内容\r\n0003,最后一份\r\n```"
	sheets := officeContentSheets(text)
	if len(sheets) != 3 {
		t.Fatalf("some source tables disappeared: %+v", sheets)
	}
	for i, id := range []string{"0001", "0002", "0003"} {
		if len(sheets[i].Rows) != 1 || sheets[i].Rows[0][0] != id {
			t.Fatalf("table %d lost its source row: %+v", i, sheets[i])
		}
	}
	for _, tail := range []string{"\n```csv\nA,B\n1", "\n| A | B |\n| --- | --- |"} {
		if got := officeContentSheets(text + tail); len(got) != 0 {
			t.Fatalf("incomplete final table was passed off as complete: %+v", got)
		}
	}
}
