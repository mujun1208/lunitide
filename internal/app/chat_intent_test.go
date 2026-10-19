package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestBareEntityTopicLookupIntent(t *testing.T) {
	for _, goal := range []string{
		"北京支点互动科技有限公司",
		"中航材利顿航空科技股份有限公司",
		"量子计算是什么",
		"国家超级计算天津中心",
		"emo什么意思",
	} {
		if !looksLikeCurrentLookupTurn(goal) {
			t.Fatalf("bare entity must route to lookup: %s", goal)
		}
		if len(fallbackWebSearchArgs(goal)) == 0 {
			t.Fatalf("bare entity must have a fallback web query: %s", goal)
		}
	}
	for _, goal := range []string{
		"打开北京支点互动科技有限公司官网",
		"写一篇关于北京支点互动科技有限公司的文章",
		"介绍一下你自己",
		"自我介绍",
		"个人简介",
		"今晚月色怎么样",
		"在吗",
		"我们公司",
	} {
		if looksLikeCurrentLookupTurn(goal) {
			t.Fatalf("non-lookup goal classified as lookup: %s", goal)
		}
	}
}

func TestLookupFollowUpOverridesRoute(t *testing.T) {
	apps := []string{"微信", "飞书", "浏览器"}
	if !lookupFollowUpOverridesRoute(RouteR2, "查询啊", apps) {
		t.Fatal("lookup follow-up misread as R2 must be suppressed to R1")
	}
	if !lookupFollowUpOverridesRoute(RouteR3, "查询啊", apps) {
		t.Fatal("lookup follow-up misread as R3 must be suppressed to R1")
	}
	if lookupFollowUpOverridesRoute(RouteR1, "查询啊", apps) {
		t.Fatal("R1 needs no suppression")
	}
	if lookupFollowUpOverridesRoute(RouteR2, "打开微信", apps) {
		t.Fatal("desktop app goal must keep R2")
	}
	if lookupFollowUpOverridesRoute(RouteR2, "在微信里查一下天气", apps) {
		t.Fatal("named-app lookup goal must keep R2")
	}
}

func TestLaneRescueForBareEntityAndSemanticR1(t *testing.T) {
	// A bare company name carries no lookup verb; the bare-entity intent
	// must still open the lane rescue (web search + a real step budget).
	laneIn := LaneInput{Goal: "北京支点互动科技有限公司"}
	lane := classifyChatLane(laneIn)
	_, contract := applyLaneOverrides(lane, laneIn, RouteUnspecified, CouncilOverlay{})
	if !contract.AllowWebSearch || contract.MaxMainToolSteps < 4 {
		t.Fatalf("bare company name must get web search + >=4 steps: %+v", contract)
	}
	defs := []llmadapter.ToolDefinition{
		{Name: "web.search"}, {Name: "web.fetch"}, {Name: "workspace.read"}, {Name: "user.ask"},
	}
	kept := applyLaneTools(defs, contract)
	if !toolDefinitionsHave(kept, "web.search") {
		t.Fatalf("rescued L1 surface must keep web.search: %v", kept)
	}
	// The semantic (flash) classifier's R1 conclusion must reach the lane
	// rescue even when keyword detectors see nothing.
	laneIn = LaneInput{Goal: "今天有什么大事"}
	lane = classifyChatLane(laneIn)
	_, contract = applyLaneOverrides(lane, laneIn, RouteR1, CouncilOverlay{})
	if !contract.AllowWebSearch || contract.MaxMainToolSteps < 4 {
		t.Fatalf("semantic R1 must feed the lane rescue: %+v", contract)
	}
}

func TestWebLookupRescueContract(t *testing.T) {
	// Replay of the 2026-10-19 failure: catalog discovery calls disarmed the
	// auto-lookup ladder because mcp.search counted as an attempted lookup.
	msgs := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "查询啊"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c1", Name: "mcp.search", Arguments: []byte(`{"query":"搜索公司信息"}`)},
			{ID: "c2", Name: "capability.discover", Arguments: []byte(`{"need":"web search for company info"}`)},
		}},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "无匹配"},
		{Role: llmadapter.RoleTool, ToolCallID: "c2", Content: "内置工具；本轮未挂载"},
	}
	if turnAttemptedWebLookup(msgs) {
		t.Fatal("discovery calls must not disarm the web lookup rescue")
	}
	if !discoveryAskedForWebLookup(msgs) {
		t.Fatal("capability.discover/mcp.search lookup intent must arm the rescue")
	}
	if !turnAttemptedAction(msgs, "lookup") {
		t.Fatal("legacy lookup tracking still sees the discovery call (regression guard)")
	}
	msgs = append(msgs,
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "c3", Name: "web.search", Arguments: []byte(`{"query":"x"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "c3", Content: "ok"},
	)
	if !turnAttemptedWebLookup(msgs) {
		t.Fatal("a real web.search receipt must disarm the rescue")
	}
	full := []llmadapter.ToolDefinition{
		{Name: "web.search"}, {Name: "web.fetch"}, {Name: "weather.get"}, {Name: "todo.write"},
	}
	narrow := []llmadapter.ToolDefinition{{Name: "user.ask"}}
	got := appendWebLookupTools(narrow, full)
	if !toolDefinitionsHave(got, "web.search") || !toolDefinitionsHave(got, "web.fetch") || !toolDefinitionsHave(got, "weather.get") {
		t.Fatalf("appendWebLookupTools must attach the web family: %v", got)
	}
	if toolDefinitionsHave(got, "todo.write") {
		t.Fatalf("appendWebLookupTools must not leak unrelated tools: %v", got)
	}
}

func TestAutoLookupQueryPrefersConversationSubject(t *testing.T) {
	msgs := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "北京支点互动科技有限公司"},
		{Role: llmadapter.RoleAssistant, Content: "找到可用的搜索工具了，现在实际查一下这家公司："},
		{Role: llmadapter.RoleUser, Content: "查询啊"},
	}
	if got := autoLookupQuery("查询啊", msgs); got != "北京支点互动科技有限公司" {
		t.Fatalf("follow-up command must reuse the earlier subject: %q", got)
	}
	if got := autoLookupQuery("北京支点互动科技有限公司", msgs); got != "北京支点互动科技有限公司" {
		t.Fatalf("full subject goal must stay the query: %q", got)
	}
	discovery := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "查询啊"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "c1", Name: "capability.discover", Arguments: []byte(`{"need":"搜索北京支点互动科技有限公司的信息"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "未挂载"},
	}
	if got := autoLookupQuery("查询啊", discovery); got != "搜索北京支点互动科技有限公司的信息" {
		t.Fatalf("discovery need text must back the query: %q", got)
	}
}

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
	for _, goal := range []string{"把已有资料整理成 Word，不要搜索。", "把已完成的新闻报告转为 Word，不联网，只使用附件内容。",
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
	for _, goal := range []string{"查今天合肥天气", "今天沪深指数怎么样", "搜索最新新闻并生成 Word", "将已有报告转为 Word，再查询最新股价", "Search latest news and create a Word report", "不要搜索旧新闻，请搜索今天最新新闻", "将已有报告转为 Word，并搜索最新新闻补充", "Convert the existing report to Word and search latest news", "帮我查询中航材利顿航空科技股份有限公司，相关信息", "帮我找点行业资讯", "搜一下市面上类似的产品"} {
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
