package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
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

func TestComposerSendGoalTypesHelloIntoOpenedApp(t *testing.T) {
	goal := "在软件的输入框内输入，你好发，然后发送。"
	text, ok := composerSendGoal(goal)
	if !ok || text != "你好" {
		t.Fatalf("composer text=%q ok=%v", text, ok)
	}
	plain, ok := composerSendGoal("在输入框输入你好发送")
	if !ok || plain != "你好" {
		t.Fatalf("plain text=%q ok=%v", plain, ok)
	}
	if _, ok := composerSendGoal("身份证号码后面写204040"); ok {
		t.Fatal("a labeled field fill is not a composer send")
	}
	if !looksLikeTypeAfterLabelTurn(goal) {
		t.Fatal("composer send should use the type path")
	}
	doubao := "在这个桌面这个豆包的输入对话框当中输入你好然后发送"
	text, ok = composerSendGoal(doubao)
	if !ok || text != "你好" {
		t.Fatalf("doubao composer text=%q ok=%v", text, ok)
	}
	doubaoRaw := composerSendTypeArgs(doubao, nil)
	doubaoGot := string(doubaoRaw)
	if !strings.Contains(doubaoGot, `"text":"你好"`) || !strings.Contains(doubaoGot, `"window":"豆包"`) || !strings.Contains(doubaoGot, `"submit":true`) {
		t.Fatalf("doubao args=%s", doubaoRaw)
	}
	merged := composerTypeArgsForCall(doubao, nil, []byte(`{"text":"你好"}`))
	if !strings.Contains(string(merged), `"window":"豆包"`) || !strings.Contains(string(merged), `"submit":true`) {
		t.Fatalf("merged=%s", merged)
	}
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "我说打开我桌面的豆包软件。"},
		{Role: llmadapter.RoleAssistant, Content: "好，我来打开。已打开目标文件或应用。"},
		{Role: llmadapter.RoleUser, Content: goal},
	}
	raw := composerSendTypeArgs(goal, messages)
	got := string(raw)
	if !strings.Contains(got, `"text":"你好"`) || !strings.Contains(got, `"window":"豆包"`) || !strings.Contains(got, `"submit":true`) {
		t.Fatalf("args=%s", raw)
	}
	settled := append(messages, llmadapter.Message{Role: llmadapter.RoleTool, Content: `typed "你好" and submitted in 豆包`})
	if !composerSendSettled(goal, settled) {
		t.Fatal("a submitted type should settle the turn")
	}
	failed := append(messages, llmadapter.Message{Role: llmadapter.RoleTool, Content: "ok:false\n无法执行"})
	if composerSendSettled(goal, failed) {
		t.Fatal("a failed type must not settle")
	}
}

func TestWeChatChatGoalNamesContactAndMinutes(t *testing.T) {
	contact, minutes, ok := parseWeChatChatGoal("和微信的_穆_聊5分钟")
	if !ok || contact != "_穆_" || minutes != 5 {
		t.Fatalf("mu contact=%q minutes=%d ok=%v", contact, minutes, ok)
	}
	contact, minutes, ok = parseWeChatChatGoal("跟微信里的张三聊天，聊10分钟")
	if !ok || contact != "张三" || minutes != 10 {
		t.Fatalf("zhang contact=%q minutes=%d ok=%v", contact, minutes, ok)
	}
	contact, minutes, ok = parseWeChatChatGoal("在微信上和李四聊3分钟")
	if !ok || contact != "李四" || minutes != 3 {
		t.Fatalf("li contact=%q minutes=%d ok=%v", contact, minutes, ok)
	}
	contact, minutes, ok = parseWeChatChatGoal("和微信的文件传输助手聊一会儿")
	if !ok || contact != "文件传输助手" || minutes != 5 {
		t.Fatalf("file contact=%q minutes=%d ok=%v", contact, minutes, ok)
	}
	if _, _, ok = parseWeChatChatGoal("在这个桌面这个豆包的输入对话框当中输入你好然后发送"); ok {
		t.Fatal("doubao send is not a wechat chat")
	}
	if _, _, ok = parseWeChatChatGoal("不要和微信的张三聊天"); ok {
		t.Fatal("a refusal must not start a wechat chat")
	}
	raw := string(wechatChatTypeArgs("和微信的_穆_聊5分钟"))
	if !strings.Contains(raw, `"window":"微信"`) || !strings.Contains(raw, `"after":"_穆_"`) || !strings.Contains(raw, `"text":"你好"`) || !strings.Contains(raw, `"submit":true`) {
		t.Fatalf("wechat args=%s", raw)
	}
	if !looksLikeTypeAfterLabelTurn("和微信的_穆_聊5分钟") || !computerExecutionTurn("和微信的_穆_聊5分钟") {
		t.Fatal("wechat chat must stay on the desktop type path")
	}
}

func TestDocumentOpenTypeSaveArgs(t *testing.T) {
	raw := string(fallbackDesktopTypeArgs("打开桌面上的协议，在证件号码后面写204040，然后保存"))
	if !strings.Contains(raw, `"after":"证件号码"`) || !strings.Contains(raw, `"text":"204040"`) || !strings.Contains(raw, `"save":true`) || !strings.Contains(raw, `"window":"协议"`) {
		t.Fatalf("document args=%s", raw)
	}
	if strings.Contains(raw, "ctrl") || strings.Contains(string(wechatChatTypeArgs("打开桌面上的协议，在证件号码后面写204040，然后保存")), "微信") {
		t.Fatal("a document fill must not become a wechat search")
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
		t.Fatal("train lookup must still be treated as a current lookup")
	}
	if got := fallbackWebSearchArgs("今天合肥到上海虹桥站的火车"); len(got) != 0 {
		t.Fatalf("live tickets must not auto-search the public web: %s", got)
	}
	if mediaGenerationKind("帮我生成一张月球图片") != "image.generate" || mediaGenerationKind("生成一个短视频") != "video.generate" {
		t.Fatal("media generation intent missing")
	}
	if mediaGenerationKind("帮我生成一首可以听的歌") != "audio.generate" || mediaGenerationKind("朗读这段：春风又绿江南岸") != "audio.generate" {
		t.Fatal("speech generation intent missing")
	}
	if mediaGenerationKind("放首歌") != "" || mediaGenerationKind("帮我创建一个技能，可以生成歌曲") != "" {
		t.Fatal("play-existing and skill-authoring must not start speech generation")
	}
	if len(fallbackMediaGenerationArgs("帮我生成一首可以听的歌")) != 0 {
		t.Fatal("song without lyrics must not auto-speak the request")
	}
	if got := string(fallbackMediaGenerationArgs("朗读这段：春风又绿江南岸")); !strings.Contains(got, "春风又绿江南岸") {
		t.Fatalf("read-aloud should inject speech text: %s", got)
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

func TestDesktopOpenTargetStripsBookTitleAndJoinsAsrComma(t *testing.T) {
	target, ok := desktopOpenTargetFromGoal("打开桌面的《企业AI智能助手》txt文件")
	if !ok || target != "企业AI智能助手" {
		t.Fatalf("book title txt = %q ok=%v", target, ok)
	}
	target, ok = desktopOpenTargetFromGoal("打开桌面周报，建议txt文件")
	if !ok || target != "周报建议" {
		t.Fatalf("asr comma filename = %q ok=%v", target, ok)
	}
	if got := string(fallbackDesktopOpenArgs("打开桌面周报，建议txt文件")); !strings.Contains(got, "周报建议") || strings.Contains(got, "周报，") {
		t.Fatalf("comma must not stay in open args: %s", got)
	}
	target, ok = desktopOpenTargetFromGoal("打开记事本，然后写你好")
	if !ok || target != "记事本" {
		t.Fatalf("real second clause must stay split: %q ok=%v", target, ok)
	}
}
