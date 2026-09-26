package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func receiptMessages(name, args, out string) []llmadapter.Message {
	return []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "本轮任务"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "current", Name: name, Arguments: json.RawMessage(args)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "current", Content: out},
	}
}

func TestMediaControlReceiptsDoNotBecomePlaybackFailures(t *testing.T) {
	for _, out := range []string{"sent next track", "sent previous track", "sent pause/play toggle", "sent stop"} {
		t.Run(out, func(t *testing.T) {
			messages := receiptMessages("media.play", `{}`, out)
			got := mediaTurnResultSpeech(messages)
			if !strings.Contains(got, "已发送") || strings.Contains(got, "不能算完成") || strings.Contains(got, "已经在播") {
				t.Fatal(got)
			}
			if shouldContinueIncompleteWork(got, out, []string{"media.play"}, true, 0) {
				t.Fatal("must not send next/pause twice")
			}
		})
	}
}

func TestUnconfirmedMediaSessionStopsScreenClicks(t *testing.T) {
	out := `media session action=pause; status=Paused; result unconfirmed`
	if speech := mediaControlReceiptSpeech(out); speech != "已暂停播放。" {
		t.Fatal(speech)
	}
	if !mediaKeyDelivered(out) {
		t.Fatal("pause receipt must count as delivered")
	}
	messages := receiptMessages("media.play", `{"action":"pause"}`, out)
	if err := guardCurrentTurnToolHistory("暂停歌曲", "computer.act", messages); err == nil {
		t.Fatal("computer.act followed a delivered pause")
	}
	if shouldContinueIncompleteWork("已暂停播放。", out, []string{"media.play"}, true, 0) {
		t.Fatal("must not continue after the pause key")
	}
}

func TestCompanionForegroundStopsAnotherComputerAct(t *testing.T) {
	messages := receiptMessages("computer.act", `{"action":"observe"}`, `{"count":0,"hint":"前台是月伴，没有其它窗口可操作。"}`)
	if err := guardCurrentTurnToolHistory("帮我点保存", "computer.act", messages); err == nil {
		t.Fatal("a second computer.act must stop when the foreground is still 月伴")
	}
	if speech := companionDesktopResultSpeech(`{"count":3,"frameId":"frm_1","nodes":[{"name":"保存"}]}`); speech != "先看了一下。" {
		t.Fatalf("observe json speech: %s", speech)
	}
	if speech := companionDesktopResultSpeech("clicked \"保存\"; 已点击，画面没有明显变化。 screen unchanged"); speech != "点过了，画面没有变化。" {
		t.Fatalf("unchanged speech: %s", speech)
	}
}

func TestOpenedDesktopBrowserStopsLaterBrowserTools(t *testing.T) {
	unverified := receiptMessages("desktop.browse", `{"query":"古天乐 最新新闻"}`, "已向系统默认桌面浏览器发送打开请求：https://www.bing.com/search?q=news")
	if err := guardCurrentTurnToolHistory("打开网页搜索古天乐最新新闻", "computer.act", unverified); err != nil {
		t.Fatal("an unverified browser handoff must still allow the window to be confirmed", err)
	}
	messages := receiptMessages("desktop.browse", `{"query":"古天乐 最新新闻"}`, "已打开桌面浏览器：https://www.bing.com/search?q=news")
	goal := "打开网页搜索古天乐最新新闻"
	for _, name := range []string{"computer.act", "browser.act"} {
		if _, ok := browseAlreadyOpenReceipt(goal, name, messages); !ok {
			t.Fatal("open search must settle without another desktop tool", name)
		}
		if err := guardCurrentTurnToolHistory(goal, name, messages); err == nil {
			t.Fatal("continued after the page was open", name)
		}
	}
	if err := guardCurrentTurnToolHistory("打开网站第一个新闻", "browser.act", messages); err != nil {
		t.Fatal(err)
	}
}

func TestOpenedDesktopSearchIsNotSpokenAsFailure(t *testing.T) {
	goal := "打开桌面浏览器，搜索周杰伦的最新新闻"
	opened := "已打开桌面浏览器：https://www.bing.com/search?q=jay\n" + `{"l0":{"kind":"process","passed":true,"uncertain":false}}`
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "browse", Name: "desktop.browse", Arguments: json.RawMessage(`{"query":"周杰伦 最新新闻"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "browse", Content: opened},
	}
	settled, ok := browseAlreadyOpenReceipt(goal, "computer.act", messages)
	if !ok || strings.Contains(settled, "ok:false") || strings.Contains(settled, "失败") {
		t.Fatalf("settled = %q ok=%v", settled, ok)
	}
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "act", Content: settled})
	got := companionFinalResult(messages, "好，我马上处理。", goal)
	if strings.Contains(got, "没成功") || strings.Contains(got, "未完成") || strings.Contains(got, "重试") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "桌面浏览器") {
		t.Fatal(got)
	}
	stale := companionFinalResult(messages, "好，我马上处理。无法执行：汽水音乐的播放三步都走完了仍没确认在放歌，且屏幕操作通道这轮被占用。", goal)
	if strings.Contains(stale, "汽水") || strings.Contains(stale, "放歌") || strings.Contains(stale, "播放控件") {
		t.Fatalf("previous playback failure leaked into the browser turn: %s", stale)
	}
	if !strings.Contains(stale, "桌面浏览器") {
		t.Fatal(stale)
	}
}

func TestDiskEditDoesNotProveEditorUpdated(t *testing.T) {
	messages := receiptMessages("workspace.edit", `{"path":"C:\\Users\\test\\Desktop\\notes.txt"}`, "edited notes.txt (1 replacement(s))")
	messages = append(messages,
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "see", Name: "computer.act", Arguments: json.RawMessage(`{"action":"screenshot"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "see", Content: "captured foreground window"})
	got := companionFinalResult(messages, "已经在记事本写好并确认了。", "打开记事本，在最后输入号码")
	if got != "已写入磁盘文件，但未确认当前编辑窗口已同步。" {
		t.Fatal(got)
	}
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleUser, Content: "今天天气"})
	if got = companionFinalResult(messages, "今天晴。", "今天天气"); got != "今天晴。" {
		t.Fatal("old edit leaked into new turn", got)
	}
}

func TestFreshLookupCannotExecuteOldFileOrMusicTask(t *testing.T) {
	ticket := "今天上海虹桥到合肥南站的火车票有哪些？"
	stock := "今天沪深指数怎么样"
	for _, goal := range []string{ticket, stock} {
		if !companionWantsTools(goal) || !looksLikeCurrentLookupTurn(goal) {
			t.Fatal("lookup lost tools", goal)
		}
		for _, name := range []string{"desktop.open", "media.play", "workspace.edit", "desktop.type"} {
			if guardCurrentTurnTool(goal, name) == nil {
				t.Fatal("old action allowed", goal, name)
			}
		}
	}
	if guardCurrentTurnTool(stock, "web.search") != nil || guardCurrentTurnTool(stock, "browser.act") != nil {
		t.Fatal("stock lookup tool blocked")
	}
	if guardCurrentTurnTool(ticket, "mcp.search") != nil {
		t.Fatal("ticket discovery blocked")
	}
	if guardCurrentTurnTool("打开图片，然后查车票", "desktop.open") != nil {
		t.Fatal("explicit combined task blocked")
	}
	now := time.Date(2026, 9, 8, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	instruction := currentTurnInstruction("查今天车票", now)
	if !strings.Contains(instruction, "2026-09-08") || !strings.Contains(instruction, "不是本轮待执行清单") || !strings.Contains(instruction, "只用一到三句") {
		t.Fatal(instruction)
	}
	stable := executionModeInstruction(executionModeAutoEdit) + chatSuggestionsInstruction
	if strings.Contains(stable, "当前本地时间") || strings.Contains(stable, "最新用户要求") {
		t.Fatal("stable prefix must not include the current-turn clock or goal")
	}
	composed := appendCurrentTurnBoundary(stable, "查今天车票", now)
	if !strings.HasPrefix(composed, stable) || strings.Index(composed, "当前本地时间") < len(stable) {
		t.Fatal("current-turn boundary must follow the stable prefix")
	}
}

func TestTypedAssistStaysOutOfVoice(t *testing.T) {
	typed := appendTypedStableBlocks("", "", "")
	for _, needle := range []string{"[打字协助]", "todo.write", "recommended=true", "不要弹确认卡"} {
		if !strings.Contains(typed, needle) {
			t.Fatalf("typed assist missing %q", needle)
		}
	}
	if strings.Contains(companionPersonaChatInstruction(), "[打字协助]") || strings.Contains(companionPersonaChatInstruction(), "recommended=true") {
		t.Fatal("voice persona picked up the typed decision card")
	}
}

func TestStableInstructionPrefixHashIgnoresClock(t *testing.T) {
	stable := typedDefaultStablePrefix()
	a := appendCurrentTurnBoundary(stable, "查今天车票", time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC))
	b := appendCurrentTurnBoundary(stable, "写周报", time.Date(2026, 9, 9, 23, 59, 59, 0, time.UTC))
	if a == b {
		t.Fatal("clock and goal must change the dynamic segment")
	}
	pa, pb := stableInstructionPrefix(a), stableInstructionPrefix(b)
	if pa != pb || pa != stable {
		t.Fatalf("stable prefix must ignore clock and goal: %q vs %q", pa, pb)
	}
	sumA := sha256.Sum256([]byte(pa))
	sumB := sha256.Sum256([]byte(pb))
	if sumA != sumB {
		t.Fatal("stable prefix hash must match across turns")
	}
	if typedDefaultStablePrefixHash() != fmt.Sprintf("%x", sumA) {
		t.Fatalf("typed-default hash must match extracted builder: %s", typedDefaultStablePrefixHash())
	}
}

func TestInventoryLiveLookupFailsClosedWithoutTicketAPI(t *testing.T) {
	goal := "今天上海虹桥到合肥南站的高铁票有哪些？"
	for _, name := range []string{"web.search", "web.fetch", "browser.act", "desktop.browse"} {
		if err := guardCurrentTurnTool(goal, name); err == nil {
			t.Fatal("inventory scrape allowed", name)
		}
	}
	if guardCurrentTurnTool(goal, "mcp.search") != nil {
		t.Fatal("mcp.search is the only live-ticket probe")
	}
	if len(fallbackWebSearchArgs(goal)) != 0 {
		t.Fatal("must not auto-inject web.search for live tickets")
	}
	if len(fallbackWebSearchArgs("今天合肥到上海虹桥站的火车")) != 0 {
		t.Fatal("train lookup must not fall back to public web search")
	}
	if !strings.Contains(companionPersonaToolsInstruction(), "立刻结束") || strings.Contains(companionPersonaToolsInstruction(), "需要补充或核实就继续查询") {
		t.Fatal("companion prompt still asks to keep scraping tickets")
	}
	blob := bundledWorkflowInjection(goal)
	if !strings.Contains(blob, "尚未接入") {
		t.Fatal("missing fail-closed ticket clause")
	}
	if strings.Contains(blob, "普通资料用 web.search") {
		t.Fatal("generic research clause fights ticket fail-closed")
	}
	if guardCurrentTurnTool("打开12306查今天上海到合肥高铁", "desktop.browse") != nil {
		t.Fatal("explicit 12306 open blocked")
	}
}

func TestInventoryOpen12306AllowsPageButNotScrapeT32(t *testing.T) {
	goal := "打开12306查今天上海到合肥高铁"
	if guardCurrentTurnTool(goal, "desktop.browse") != nil || guardCurrentTurnTool(goal, "desktop.open") != nil {
		t.Fatal("T32 must allow opening the page")
	}
	for _, name := range []string{"web.search", "web.fetch", "browser.act"} {
		if err := guardCurrentTurnTool(goal, name); err == nil {
			t.Fatal("T32 scrape allowed", name)
		}
	}
}

func TestInventoryFlightLookupFailsClosedWithoutTicketAPI(t *testing.T) {
	goal := "查明天上海到北京机票"
	for _, name := range []string{"web.search", "web.fetch", "browser.act", "desktop.browse"} {
		if err := guardCurrentTurnTool(goal, name); err == nil {
			t.Fatal("flight scrape allowed", name)
		}
	}
	if guardCurrentTurnTool(goal, "mcp.search") != nil {
		t.Fatal("mcp.search is the only live-ticket probe")
	}
	if len(fallbackWebSearchArgs(goal)) != 0 {
		t.Fatal("must not auto-inject web.search for flights")
	}
}

func TestGuardBrowserLaunchFailureStopsTheTurn(t *testing.T) {
	goal := "打开网页第一条动态新闻"
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "b1", Name: "browser.act"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "b1", Content: "ok:false\nBROWSER_LAUNCH_FAILED: async initializeServer: Chromium download failed"},
	}
	if err := guardCurrentTurnToolHistory(goal, "browser.act", messages); err == nil {
		t.Fatal("second browser.act after Chromium launch failure")
	}
	if err := guardCurrentTurnToolHistory(goal, "computer.act", messages); err == nil {
		t.Fatal("screen click allowed after the browser failed to start")
	}
	if got := companionToolResultSpeech("browser.act", messages[2].Content); got != "浏览器没能启动，这一步停在这里。" {
		t.Fatalf("speech = %q", got)
	}
}

func TestPlaybackTransportBlocksScreenClick(t *testing.T) {
	goal := "播放一首歌"
	delivered := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleTool, Content: "started playing in 汽水音乐 (media key)\n" + `{"l0":{"kind":"foreground","passed":false,"uncertain":true,"detail":"MEDIA_UNVERIFIED"}}`},
	}
	if err := guardCurrentTurnToolHistory(goal, "computer.act", delivered); err != nil {
		t.Fatal("an unconfirmed play must still allow the screen check", err)
	}
	if err := guardCurrentTurnToolHistory("下一曲", "computer.act", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "下一曲"},
		{Role: llmadapter.RoleTool, Content: "sent next track"},
	}); err == nil {
		t.Fatal("screen click allowed after next track was sent")
	}
	if err := guardCurrentTurnTool(goal, "computer.act"); err != nil {
		t.Fatal("a failed play may still look at the screen once")
	}
	if !quitOnlyGoal("关闭汽水音乐") {
		t.Fatal("named app close must be a quit")
	}
	args := fallbackDesktopQuitArgs("关闭汽水音乐")
	if !strings.Contains(string(args), "汽水音乐") || !strings.Contains(string(args), `"force":true`) {
		t.Fatalf("quit args = %s", args)
	}
	if fallbackDesktopQuitArgs("关闭窗口") != nil {
		t.Fatal("closing a window must not quit a process")
	}
	if quitOnlyGoal("关掉这个文档") || fallbackDesktopQuitArgs("关掉这个文档") != nil {
		t.Fatal("closing the open document must stay on the window, not a process name")
	}
	if quitOnlyGoal("帮我关闭当前浏览器网页") {
		t.Fatal("closing the current browser page is not an app quit")
	}
	if err := guardCurrentTurnTool("帮我关闭当前浏览器网页", "browser.act"); err == nil {
		t.Fatal("closing the browser must not start browser.act")
	}
	if err := guardCurrentTurnTool("帮我关闭当前浏览器网页", "computer.act"); err != nil {
		t.Fatal("closing the browser may use the open window", err)
	}
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "关掉这个文档"},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: `clicked "关闭"; screen updated 100x100`},
	}
	if speech := companionSucceededBeforeModelError(true, "关掉这个文档", []string{"computer.act"}, messages); speech != "已经关掉了。" {
		t.Fatalf("settled close speech = %q", speech)
	}
	if speech := companionSucceededBeforeModelError(true, "关掉这个文档", []string{"computer.act"}, []llmadapter.Message{
		{Role: llmadapter.RoleTool, Content: "ok:false\n无法执行"},
	}); speech != "" {
		t.Fatal("a failed close must stay a failure")
	}
	if speech := companionSucceededBeforeModelError(true, "打开豆包", []string{"desktop.open"}, []llmadapter.Message{
		{Role: llmadapter.RoleTool, Content: `opened "豆包"`},
	}); speech != "已经打开了。" {
		t.Fatalf("settled open speech = %q", speech)
	}
}

func TestGuardBrowserActAfterMCPNotReady(t *testing.T) {
	goal := "打开这个网页点登录"
	if err := guardCurrentTurnToolHistory(goal, "browser.act", nil); err != nil {
		t.Fatal("first browser.act blocked", err)
	}
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "b1", Name: "browser.act"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "b1", Content: "ok:false\nBROWSER_MCP_NOT_READY: Playwright MCP 未就绪"},
	}
	if err := guardCurrentTurnToolHistory(goal, "browser.act", messages); err == nil {
		t.Fatal("second browser.act after BROWSER_MCP_NOT_READY allowed")
	}
	if err := guardCurrentTurnToolHistory(goal, "web.search", messages); err != nil {
		t.Fatal("non-browser tool blocked after MCP not ready", err)
	}
}

func TestGuardOpenOnlyBlocksWorkspaceAndCommand(t *testing.T) {
	goal := "打开桌面上的日常操作功能增补文档"
	if !companionGoalIsOpenOnly(goal) {
		t.Fatal("open-only goal")
	}
	for _, name := range []string{"workspace.list", "workspace.search", "workspace.read", "workspace.write", "command.run"} {
		if err := guardCurrentTurnTool(goal, name); err == nil {
			t.Fatal("open-only allowed", name)
		}
	}
	if guardCurrentTurnTool(goal, "desktop.open") != nil {
		t.Fatal("desktop.open blocked for open-only")
	}
	for _, name := range []string{"computer.act", "desktop.type", "browser.act", "desktop.browse"} {
		if err := guardCurrentTurnTool("打开桌面的《企业AI智能助手》txt文件", name); err == nil {
			t.Fatal("open-only allowed extra hand", name)
		}
	}
}

func TestGuardOpenPageAllowsDesktopBrowse(t *testing.T) {
	goal := "打开网页百度"
	if !companionGoalIsOpenPage(goal) {
		t.Fatal("page-open goal")
	}
	if err := guardCurrentTurnTool(goal, "desktop.browse"); err != nil {
		t.Fatal(err)
	}
	if guardCurrentTurnTool("打开记事本", "desktop.browse") == nil {
		t.Fatal("app-open must not browse")
	}
}

func TestGuardWebsiteFirstResultBlocksComputerAct(t *testing.T) {
	goal := "打开网站第一个新闻"
	if err := guardCurrentTurnTool(goal, "computer.act"); err == nil {
		t.Fatal("in-page first news must not use computer.act")
	}
	if guardCurrentTurnTool(goal, "browser.act") != nil {
		t.Fatal("browser.act blocked for website first result")
	}
}

func TestSystemBrowserFirstResultStaysInTheOpenBrowser(t *testing.T) {
	goal := "打开浏览器的第一个新闻"
	if err := guardSystemBrowserClick(true, goal, "browser.act"); err == nil {
		t.Fatal("an already-open system browser must not launch the automation Chrome")
	}
	if err := guardSystemBrowserClick(true, goal, "computer.act"); err != nil {
		t.Fatal(err)
	}
	if err := guardSystemBrowserClick(false, goal, "browser.act"); err != nil {
		t.Fatal("without a system browser, browser.act stays available", err)
	}
	if err := guardCurrentTurnTool(goal, "computer.act"); err != nil {
		t.Fatal("clicking the open browser result must be allowed", err)
	}
}

func TestCapabilityAuthoringDoesNotConsumeSpecialistNames(t *testing.T) {
	for _, goal := range []string{"[引用专家 Excel表格制作专家|id] 做半年财报表", "[引用专家 系统测试专家|id] 写 E2E 场景", "请小说编写专家写个短篇"} {
		if capabilityWorkTask(goal) {
			t.Fatal("specialist use confused with creation", goal)
		}
	}
	for _, goal := range []string{"帮我创建一个短剧编剧专家", "测试一下新建的周报生成技能", "先试用这个专家"} {
		if !capabilityWorkTask(goal) {
			t.Fatal("authoring/trial missed", goal)
		}
	}
}

type taskResultAdapter struct {
	continueAdapter
	stubborn bool
}

func (a *taskResultAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	text := "稍等，我这就帮你查一下今天沪深指数的行情，马上告诉你涨跌情况。"
	if a.calls > 1 && !a.stubborn {
		text = "这次查询失败，未取得实时行情。"
	}
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestVoiceTaskWaitIsNotSpokenAsFinalResult(t *testing.T) {
	// All three ASR/TTS paths share this chat stream contract.
	for _, mode := range []string{"cloud", "volc", "local"} {
		for _, stubborn := range []bool{false, true} {
			t.Run(mode+map[bool]string{true: "-stalled", false: "-result"}[stubborn], func(t *testing.T) {
				adapter := &taskResultAdapter{stubborn: stubborn}
				e := NewEngineWithGateway(nil, "test", streamTestLease{})
				e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				state := &streamState{cancel: cancel, state: streamRunning, companion: true}
				e.streams[mode] = state
				var spoken strings.Builder
				starts, terminal := 0, 0
				req := llmadapter.Request{Model: "m", DisableReasoning: true,
					Tools:    []llmadapter.ToolDefinition{{Name: "web.search"}},
					Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "今天沪深指数怎么样"}}}
				e.runStream(ctx, mode, state, provider.Provider{Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com"}, req, func(ev bridge.Event) error {
					if ev.Type == bridge.EventDelta && ev.Delta != nil {
						spoken.WriteString(ev.Delta.Text)
					}
					if ev.Type == bridge.EventToolStarted {
						starts++
					}
					if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
						terminal++
					}
					return nil
				}, "")
				if starts != 1 || terminal != 1 || adapter.calls < 2 {
					t.Fatalf("starts=%d terminal=%d calls=%d", starts, terminal, adapter.calls)
				}
				if strings.Contains(spoken.String(), "稍等") || len([]rune(spoken.String())) > 100 {
					t.Fatal("verbose/promise-only response", spoken.String())
				}
				if !strings.Contains(spoken.String(), "失败") && !strings.Contains(spoken.String(), "未完成") {
					t.Fatal("missing terminal outcome", spoken.String())
				}
			})
		}
	}
}

func TestLookupStopsAfterTheBrowserSearchReturns(t *testing.T) {
	goal := "打开浏览器，查询古天乐的最新新闻"
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "browse", Name: "desktop.browse", Arguments: json.RawMessage(`{"query":"古天乐 最新新闻"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "browse", Content: "已向系统默认桌面浏览器发送打开请求：https://www.bing.com/search?q=news"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "search", Name: "web.search", Arguments: json.RawMessage(`{"query":"古天乐 最新新闻"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "search", Content: "query: 古天乐 最新新闻\nresults_url: https://cn.bing.com/search?q=news\n\n1. 古天乐新片定档\n   https://news.example/koo\n"},
	}
	if _, ok := settledLookupSpeech(goal, messages); ok {
		t.Fatal("an unverified browser handoff is not a finished lookup")
	}
	if err := guardCurrentTurnToolHistory(goal, "computer.act", messages); err != nil {
		t.Fatal("the window still has to be confirmed", err)
	}
	confirmed := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "browse", Name: "desktop.browse"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "browse", Content: "已打开桌面浏览器：https://www.bing.com/search?q=news"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "search", Name: "web.search"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "search", Content: "1. 古天乐新片定档\n"},
	}
	if speech, ok := settledLookupSpeech(goal, confirmed); !ok || !strings.Contains(speech, "查询完成") || !strings.Contains(speech, "古天乐新片定档") {
		t.Fatalf("confirmed browser speech=%q ok=%v", speech, ok)
	}
	if err := guardCurrentTurnToolHistory(goal, "web.search", confirmed); err == nil {
		t.Fatal("a second search must not run after the window and the titles are both back")
	}
	if err := guardCurrentTurnToolHistory(goal, "computer.act", confirmed); err == nil {
		t.Fatal("a click must not run after the window and the titles are both back")
	}
	empty := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "查询古天乐的最新新闻"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "search", Name: "web.search"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "search", Content: "ok"},
	}
	if err := guardCurrentTurnToolHistory("查询古天乐的最新新闻", "web.search", empty); err != nil {
		t.Fatal("an empty search must still allow another search until there is a result")
	}
	if _, ok := settledWorkSpeech("查询古天乐的最新新闻然后打开第一条", messages); ok {
		t.Fatal("search plus open-the-first-link is not finished at the search")
	}
	opened := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开第一个新闻链接"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "fetch", Name: "web.fetch", Arguments: json.RawMessage(`{"url":"https://news.example/koo"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "fetch", Content: "url: https://news.example/koo\nfirst_hit: true\n已打开第一条。"},
	}
	got, ok := settledLookupSpeech("打开第一个新闻链接", opened)
	if !ok || got != "已经打开第一条。" {
		t.Fatalf("speech=%q ok=%v", got, ok)
	}
	unconfirmedOpen := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开记事本"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "open", Name: "desktop.open", Arguments: json.RawMessage(`{"target":"notepad"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "open", Content: "opened notepad"},
	}
	if _, ok := settledWorkSpeech("打开记事本", unconfirmedOpen); ok {
		t.Fatal("an unconfirmed open is not success; the turn must keep going until the window is verified")
	}
	if err := guardCurrentTurnToolHistory("打开记事本", "computer.act", unconfirmedOpen); err != nil {
		t.Fatal("an unconfirmed open must still allow the next step that can finish it")
	}
	launched := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开记事本"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "open", Name: "desktop.open", Arguments: json.RawMessage(`{"name":"记事本"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "open", Content: "opened notepad\n" + `{"l0":{"kind":"unverified","passed":true,"uncertain":false}}`},
	}
	if _, ok := settledWorkSpeech("打开记事本", launched); ok {
		t.Fatal("a launch without a confirmed window is not finished")
	}
	if err := guardCurrentTurnToolHistory("打开记事本", "computer.act", launched); err != nil {
		t.Fatal("an unconfirmed window must still allow the step that can confirm it")
	}
	confirmedOpen := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开记事本"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "open", Name: "desktop.open", Arguments: json.RawMessage(`{"name":"记事本"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "open", Content: "opened notepad\n" + `{"l0":{"kind":"foreground","passed":true,"uncertain":false}}`},
	}
	got, ok = settledWorkSpeech("打开记事本", confirmedOpen)
	if !ok || !strings.Contains(got, "已打开目标") {
		t.Fatalf("confirmed open speech=%q ok=%v", got, ok)
	}
	if err := guardCurrentTurnToolHistory("打开记事本", "computer.act", confirmedOpen); err == nil {
		t.Fatal("a verified open must stop")
	}
	if _, ok := settledWorkSpeech("打开记事本然后写入你好", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开记事本然后写入你好"},
		{Role: llmadapter.RoleTool, ToolCallID: "open", Content: "opened notepad"},
	}); ok {
		t.Fatal("open plus type is not finished at the open step")
	}
	play := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "播放电影"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "play", Name: "media.play"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "play", Content: "已交给媒体中心播放。\nMEDIA_CENTER\nkind: video\ntitle: Night of the Living Dead (1968)\n"},
	}
	got, ok = settledWorkSpeech("播放电影", play)
	if !ok || !strings.Contains(got, "已交给媒体中心播放") || !strings.Contains(got, "Night of the Living Dead (1968)") {
		t.Fatalf("play speech=%q ok=%v", got, ok)
	}
	if err := guardCurrentTurnToolHistory("播放电影", "web.search", play); err == nil {
		t.Fatal("playback must not continue into search")
	}
	quit := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "彻底退出微信"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "quit", Name: "desktop.quit", Arguments: json.RawMessage(`{"name":"微信","force":true}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "quit", Content: "已彻底退出微信，已确认目标进程不再运行\n" + `{"l0":{"kind":"process","passed":true,"uncertain":false}}`},
	}
	got, ok = settledWorkSpeech("彻底退出微信", quit)
	if !ok || !strings.Contains(got, "已彻底退出微信") {
		t.Fatalf("quit speech=%q ok=%v", got, ok)
	}
	if err := guardCurrentTurnToolHistory("彻底退出微信", "computer.act", quit); err == nil {
		t.Fatal("quit must not continue into a click")
	}
	typed := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "在记事本的号码字段输入123"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "type", Name: "desktop.type", Arguments: json.RawMessage(`{"text":"123","after":"号码"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "type", Content: "typed \"123\"\n" + `{"l0":{"kind":"field","passed":true,"uncertain":false}}`},
	}
	got, ok = settledWorkSpeech("在记事本的号码字段输入123", typed)
	if !ok || !strings.Contains(got, "已在目标输入框写入并核对") {
		t.Fatalf("type speech=%q ok=%v", got, ok)
	}
	if _, still := settledWorkSpeech("在记事本输入123", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "在记事本输入123"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "act", Name: "computer.act", Arguments: json.RawMessage(`{"action":"type","text":"123"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "act", Content: "typed 3 character(s); screen updated 100x100"},
	}); still {
		t.Fatal("an unverified type is not finished")
	}
	next := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "切换下一首歌曲"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "next", Name: "media.play"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "next", Content: "verified next in player\n" + `{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`},
	}
	got, ok = settledWorkSpeech("切换下一首歌曲", next)
	if !ok || !strings.Contains(got, "已切换到下一首") {
		t.Fatalf("next speech=%q ok=%v", got, ok)
	}
	sent := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "给小王发一条消息"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "im", Name: "im.send"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "im", Content: "sent via 飞书 webhook"},
	}
	got, ok = settledWorkSpeech("给小王发一条消息", sent)
	if !ok || got != "已经发出去了。" {
		t.Fatalf("send speech=%q ok=%v", got, ok)
	}
	if _, still := settledWorkSpeech("跟微信里的张三聊天，聊10分钟", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "跟微信里的张三聊天，聊10分钟"},
		{Role: llmadapter.RoleTool, ToolCallID: "type", Content: "typed \"你好\" submitted"},
	}); still {
		t.Fatal("a timed chat is not finished after the first send")
	}
	pdf := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "生成 PDF"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "pdf", Name: "pdf.gen"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "pdf", Content: "generated report.pdf (1000 bytes)"},
	}
	got, ok = settledWorkSpeech("生成 PDF", pdf)
	if !ok || got != "文件已经生成。" {
		t.Fatalf("file speech=%q ok=%v", got, ok)
	}
	if err := guardCurrentTurnToolHistory("生成 PDF", "web.search", pdf); err == nil {
		t.Fatal("a finished file must not start another step")
	}
	if _, ok := settledWorkSpeech("生成文档并最后回读核对", pdf); ok {
		t.Fatal("a requested read-back is not finished at generation")
	}
	canvas := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "帮我写一份调研报告"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "canvas", Name: "canvas.present"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "canvas", Content: "canvas ready\ngenerated canvas.html (1200 bytes)"},
	}
	got, ok = settledWorkSpeech("帮我写一份调研报告", canvas)
	if !ok || got != "已经放到画布上。" {
		t.Fatalf("canvas speech=%q ok=%v", got, ok)
	}
	if _, still := settledWorkSpeech("做个PPT然后发给我", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "做个PPT然后发给我"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "ppt", Name: "pptx.gen"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "ppt", Content: "generated deck.pptx (1000 bytes)"},
	}); still {
		t.Fatal("generate-then-send is not finished at the file")
	}
	saved := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "修改周报技能"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "skill", Name: "skill.manage"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "skill", Content: "ok:true skillId=01ARZ3NDEKTSV4RRFFQ69G5FAV"},
	}
	got, ok = settledWorkSpeech("修改周报技能", saved)
	if !ok || got != "技能已经保存。" {
		t.Fatalf("skill speech=%q ok=%v", got, ok)
	}
	if err := guardCurrentTurnToolHistory("修改周报技能", "skill.view", saved); err == nil {
		t.Fatal("a saved skill must not keep reading")
	}
	if _, ok := settledWorkSpeech("修改周报技能", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "修改周报技能"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "skill", Name: "skill.manage"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "skill", Content: "ok:true"},
	}); ok {
		t.Fatal("a skill save without an id is not success")
	}
	confirmedBrowse := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开默认浏览器"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "browse", Name: "desktop.browse"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "browse", Content: "已打开桌面浏览器：https://www.bing.com"},
	}
	got, ok = settledWorkSpeech("打开默认浏览器", confirmedBrowse)
	if !ok || !strings.Contains(got, "已在系统浏览器打开") {
		t.Fatalf("browser open speech=%q ok=%v", got, ok)
	}
	if err := guardCurrentTurnToolHistory("打开默认浏览器", "computer.act", confirmedBrowse); err == nil {
		t.Fatal("a confirmed browser open must not keep clicking")
	}
	if _, ok := settledWorkSpeech("生成 PDF", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "生成 PDF"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "pdf", Name: "pdf.gen"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "pdf", Content: "generated report.pdf (0 bytes)"},
	}); ok {
		t.Fatal("an empty file is not a finished document")
	}
	unconfirmedPlay := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "播放一首歌"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "play", Name: "media.play"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "play", Content: "started playing in 汽水音乐\nMEDIA_UNVERIFIED\nplayback not confirmed"},
	}
	if _, ok := settledWorkSpeech("播放一首歌", unconfirmedPlay); ok {
		t.Fatal("an unconfirmed play must keep going until playback is verified")
	}
	heard := append(unconfirmedPlay,
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "see", Name: "computer.act", Arguments: json.RawMessage(`{"action":"observe"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "see", Content: `{"count":2,"frameId":"frm_1","nodes":[{"name":"暂停"}]}`},
	)
	if speech, ok := settledWorkSpeech("播放一首歌", heard); !ok || speech != "已经在播了。" {
		t.Fatalf("visible playback must finish the turn: %q %v", speech, ok)
	}
	if _, ok := settledWorkSpeech("暂停播放", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "暂停播放"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "pause", Name: "media.play"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "pause", Content: "sent pause/play toggle"},
	}); ok {
		t.Fatal("sending a pause key is not a confirmed pause")
	}
	if _, ok := settledWorkSpeech("关掉这个文档", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "关掉这个文档"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "click", Name: "computer.act"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "click", Content: "clicked close; screen updated"},
	}); ok {
		t.Fatal("a click is not a closed document")
	}
	closed := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "关掉这个文档"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "close", Name: "computer.act"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "close", Content: "window close 文档; screen updated"},
	}
	got, ok = settledWorkSpeech("关掉这个文档", closed)
	if !ok || got != "已经关掉了。" {
		t.Fatalf("close speech=%q ok=%v", got, ok)
	}
	if _, ok := settledWorkSpeech("查询古天乐的最新新闻", []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "查询古天乐的最新新闻"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "search", Name: "web.search"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "search", Content: "ok"},
	}); ok {
		t.Fatal("a search with no results is not a finished lookup")
	}
}
