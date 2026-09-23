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
	messages := receiptMessages("desktop.browse", `{"query":"古天乐 最新新闻"}`, "已向系统默认桌面浏览器发送打开请求：https://www.bing.com/search?q=news")
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
	if err := guardCurrentTurnToolHistory(goal, "computer.act", delivered); err == nil {
		t.Fatal("screen click allowed after the play command was delivered")
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
