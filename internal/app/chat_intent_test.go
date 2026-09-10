package app

import (
	"strings"
	"testing"
)

func TestParseDesktopTypeArgsFromGoal(t *testing.T) {
	after, text, ok := parseDesktopTypeArgsFromGoal("在身份证号码后面写204040")
	if !ok || after != "身份证号码" || text != "204040" {
		t.Fatalf("in 身份证号码后面写204040: after=%q text=%q ok=%v", after, text, ok)
	}
	after, text, ok = parseDesktopTypeArgsFromGoal("身份证号码后面写204040")
	if !ok || after != "身份证号码" || text != "204040" {
		t.Fatalf("身份证号码后面写204040: after=%q text=%q ok=%v", after, text, ok)
	}
	after, text, ok = parseDesktopTypeArgsFromGoal("帮我在文档的身份证号码后面写上姓名")
	if !ok || after != "身份证号码" || text != "姓名" {
		t.Fatalf("文档身份证号码: after=%q text=%q ok=%v", after, text, ok)
	}
}

func TestFallbackDesktopTypeArgs(t *testing.T) {
	raw := fallbackDesktopTypeArgs("在身份证号码后面写204040")
	if len(raw) == 0 {
		t.Fatal("expected args")
	}
	s := string(raw)
	if !strings.Contains(s, `"text":"204040"`) || !strings.Contains(s, `"after":"身份证号码"`) {
		t.Fatalf("args = %s", raw)
	}
}

func TestLooksLikeTypeAfterLabelTurnComplete(t *testing.T) {
	if !looksLikeTypeAfterLabelTurn("身份证号码后面写204040") {
		t.Fatal("complete command should match")
	}
	if !looksLikeTypeAfterLabelTurn("在证件号码后面填一下") {
		t.Fatal("incomplete fill should match")
	}
	if looksLikeTypeAfterLabelTurn("今晚月色如何") {
		t.Fatal("idle chat should not match")
	}
}

func TestHostToolFallbackIntentCoversVoiceAndTypedTasks(t *testing.T) {
	if !looksLikeCurrentLookupTurn("今天合肥到上海虹桥站的火车") {
		t.Fatal("train lookup must get a deterministic web search")
	}
	if got := string(fallbackWebSearchArgs("今天合肥到上海虹桥站的火车")); !strings.Contains(got, `"max":5`) {
		t.Fatalf("search args = %s", got)
	}
	if mediaGenerationKind("帮我生成一张月球图片") != "image.generate" || mediaGenerationKind("生成一个短视频") != "video.generate" {
		t.Fatal("media generation intent missing")
	}
	if mediaGenerationKind("我配置不了生成图片的模型") != "" {
		t.Fatal("configuration question must not start paid generation")
	}
}

func TestLookupFallbackHonorsOfflineAndReferenceOnlyDocuments(t *testing.T) {
	for _, goal := range []string{
		"把已完成的新闻报告转为 Word，不联网，只使用附件内容。",
		"将以上天气数据生成文档，不要搜索。",
		"将已有新闻整理成 Word，保留来源链接。",
		"将已有新闻搜索结果整理成 Word，保留来源。",
		"Convert the existing latest news search results to a Word report.",
		"根据附件制作股价报告。无需联网。",
		"Convert the provided latest news into a Word document without browsing.",
		"Do not search online. Format the latest news into a document.",
		"只用现有材料生成新闻 Word。不联网。" + strings.Repeat("保留原文与来源。", 30),
	} {
		if looksLikeCurrentLookupTurn(goal) || len(fallbackWebSearchArgs(goal)) != 0 {
			t.Fatalf("offline/reference task requested lookup: %s", goal)
		}
	}
	for _, goal := range []string{"查今天合肥天气", "今天沪深指数怎么样", "搜索最新新闻并生成 Word", "将已有报告转为 Word，再查询最新股价", "Search latest news and create a Word report", "不要搜索旧新闻，请搜索今天最新新闻", "将已有报告转为 Word，并搜索最新新闻补充", "Convert the existing report to Word and search latest news"} {
		if !looksLikeCurrentLookupTurn(goal) || len(fallbackWebSearchArgs(goal)) == 0 {
			t.Fatalf("requested lookup suppressed: %s", goal)
		}
	}
	if len(fallbackWebSearchArgs(strings.Repeat("x", 512))) == 0 || len(fallbackWebSearchArgs(strings.Repeat("x", 513))) != 0 || len(fallbackWebSearchArgs(strings.Repeat("新", 171))) != 0 {
		t.Fatal("fallback search did not respect the byte-based runtime limit")
	}
}

func TestDesktopFallbackExtractsTargetBeforeEditTail(t *testing.T) {
	target, ok := desktopOpenTargetFromGoal("打开桌面的企业AI智能助手文档的最后一行输入13145262")
	if !ok || target != "企业AI智能助手文档" {
		t.Fatalf("target=%q ok=%v", target, ok)
	}
	if string(autoDesktopObserveArgs()) != `{"action":"observe"}` {
		t.Fatalf("desktop observe nudge must read the UIA tree, not screenshot pixels: %s", autoDesktopObserveArgs())
	}
	if !looksLikeDesktopObserveTurn("打开桌面的企业AI智能助手文档的最后一行输入13145262") {
		t.Fatal("multi-step desktop task must observe after opening")
	}
	if got := string(fallbackDesktopOpenArgs("打开桌面上的协议文档")); !strings.Contains(got, "协议文档") {
		t.Fatalf("open args = %s", got)
	}
}
