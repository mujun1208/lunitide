package app

import "testing"

func TestExplicitOfficeOutputFormatWinsOverSourceAndExpert(t *testing.T) {
	for _, tc := range []struct{ goal, tool string }{
		{"请生成中文 PDF 报告，不联网", "pdf.gen"},
		{"将 Word 报告转成 PDF", "pdf.gen"},
		{"参考文档，帮我做一个10页的PDF", "pdf.gen"},
		{"帮我做一个Word周报", "docx.gen"},
		{"帮我做个Excel表格", "excel.gen"},
		{"生成 Word 报告，参考 source.pdf", "docx.gen"},
		{"参考附件 PDF，生成 PPT", "pptx.gen"},
		{"请生成 Excel 表格，不要生成 PDF", "excel.gen"},
		{"Export a PDF report", "pdf.gen"},
	} {
		if got := officeGenToolForGoal(tc.goal); got != tc.tool {
			t.Errorf("%s: %s want %s", tc.goal, got, tc.tool)
		}
		if got := officeGenToolForTurn(&chatTurnCheckpoint{Goal: tc.goal, PptActive: true, DocxActive: true}); got != tc.tool {
			t.Errorf("expert overrode explicit output: %s -> %s", tc.goal, got)
		}
	}
	for _, goal := range []string{"请读取附件 PDF", "检查这个 PDF 文件", "不要生成 PDF", "what is a PDF?", "怎么做一个PDF", "做一个PDF还是Word"} {
		if got := explicitOfficeOutputTool(goal); got != "" {
			t.Errorf("not an output request: %s -> %s", goal, got)
		}
		if got := officeGenToolForGoal(goal); got != "" {
			t.Errorf("how-to or mixed format routed to %s: %s", got, goal)
		}
	}
}

func TestTextAndVoiceRetainAllRequestedGenerationModelsAndFormats(t *testing.T) {
	for _, companion := range []bool{false, true} {
		for _, tc := range []struct{ goal, tool string }{
			{"查询今天的新闻，导出 PDF", "pdf.gen"},
			{"查询新闻，生成 Word 报告", "docx.gen"},
			{"查天气，生成 Excel 表格", "excel.gen"},
			{"查新闻，生成 PPT", "pptx.gen"},
			{"查新闻，生成一个短视频", "video.generate"},
			{"查新闻，生成一张图片", "image.generate"},
		} {
			defs := assembleRoutedTools(engineToolDefinitions(), tc.goal, companion, true)
			for _, needed := range []string{tc.tool, "web.search"} {
				if !toolDefinitionsHave(defs, needed) {
					t.Errorf("companion=%v goal=%s missing=%s", companion, tc.goal, needed)
				}
			}
		}
	}
	defs := specialistToolDefinitions(engineToolDefinitions())
	for _, name := range []string{"image.generate", "video.generate", "pdf.gen"} {
		if !toolDefinitionsHave(defs, name) {
			t.Errorf("specialist missing %s", name)
		}
	}
}

func TestPDFRecoveryKeepsRealContentAndRejectsPromise(t *testing.T) {
	if args := fallbackOfficeGenArgs("pdf.gen", "生成 PDF", "稍等，我这就生成"); len(args) != 0 {
		t.Fatal("promise became a PDF")
	}
	if args := fallbackOfficeGenArgs("pdf.gen", "生成 PDF", "本周完成接口联调。\n下周进行回归测试。"); len(args) == 0 {
		t.Fatal("real content not available for PDF recovery")
	}
}

func TestMediaGenerationHonorsNegationAndExplicitModelRequests(t *testing.T) {
	for _, tc := range []struct{ goal, tool string }{
		{"不要生成图片", ""}, {"先不用生视频", ""}, {"怎么生成图片", ""},
		{"帮我配置生图模型", ""}, {"不要生成图片，制作视频", "video.generate"},
		{"调用生图模型生成一张图片", "image.generate"},
		{"Generate an image of a red square", "image.generate"},
		{"create a short video", "video.generate"},
	} {
		if got := mediaGenerationKind(tc.goal); got != tc.tool {
			t.Errorf("%s -> %s, want %s", tc.goal, got, tc.tool)
		}
	}
}

func TestBufferedVoiceResultDoesNotRepeatEarlierLeadIn(t *testing.T) {
	if got := pickTurnContinueKind("已生成文件。", "好，我来处理。", "generated report.pdf (1000 bytes)", []string{"pdf.gen"}, true, false, true, true, 0, "生成 PDF", true); got != "" {
		t.Fatalf("completed result triggered %s", got)
	}
	if got := pickTurnContinueKind("", "稍等，我来处理。", "", nil, false, false, true, true, 0, "生成 PDF", true); got != "wait" {
		t.Fatalf("empty promise stopped instead of executing: %s", got)
	}
	if got := pickTurnContinueKind("", "稍等，我来处理。", "", nil, false, false, false, false, 0, "生成 PDF", true); got != "wait" {
		t.Fatalf("typed empty promise must also execute or fail, not stop: %s", got)
	}
}
