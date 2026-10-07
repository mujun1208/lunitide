package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestExtendToolLoopLimit(t *testing.T) {
	// Far from the ceiling: no extension.
	if got := extendToolLoopLimit(24, 5); got != 24 {
		t.Fatalf("early step extended: got %d want 24", got)
	}
	// Within two steps of the ceiling and still productive: extend by a chunk.
	if got := extendToolLoopLimit(24, 23); got != 24+toolLoopExtendChunk {
		t.Fatalf("near ceiling not extended: got %d want %d", got, 24+toolLoopExtendChunk)
	}
	if got := extendToolLoopLimit(24, 22); got != 24+toolLoopExtendChunk {
		t.Fatalf("two-from-ceiling not extended: got %d want %d", got, 24+toolLoopExtendChunk)
	}
	// Never exceed the hard cap.
	if got := extendToolLoopLimit(maxToolLoopStepsHard, maxToolLoopStepsHard-1); got != maxToolLoopStepsHard {
		t.Fatalf("exceeded hard cap: got %d want %d", got, maxToolLoopStepsHard)
	}
	if got := extendToolLoopLimit(maxToolLoopStepsHard-4, maxToolLoopStepsHard-4); got != maxToolLoopStepsHard {
		t.Fatalf("extension overshot cap: got %d want %d", got, maxToolLoopStepsHard)
	}
}

func TestUnfinishedToolBudgetContinuesAndCapabilityWorkEarnsSteps(t *testing.T) {
	if !turnAdmitsUnfinishedToolBudget("本轮工具额度耗尽，只完成了技能列表查询") {
		t.Fatal("budget excuse must start another round")
	}
	if !turnAdmitsUnfinishedToolBudget("2.0 重写被工具步数限制打断，尚未落盘") {
		t.Fatal("unlanded file must start another round")
	}
	if !turnAdmitsUnfinishedToolBudget("周报还没写入文件") {
		t.Fatal("unwritten deliverable must start another round")
	}
	if turnAdmitsUnfinishedToolBudget("任务已完成，文件在对话文件夹里") {
		t.Fatal("finished turn must not start another round")
	}
	lane := buildLaneContract(LaneL1, RouteUnspecified, CouncilOverlay{})
	if !turnMayEarnMoreSteps(false, false, "试用技能: POC 快速构建", lane) {
		t.Fatal("skill trial on a one-step lane cannot continue")
	}
}

func TestPlaybackClaimsRequireThisTurnsMatchingToolReceipt(t *testing.T) {
	for _, out := range []string{"", "ok:true", "opened player and sent play", `opened https://music.example\n{"l0":{"passed":true,"uncertain":false}}`} {
		if !unverifiedMediaPlay("media.play", out, "音乐已经在播放") {
			t.Fatalf("unverified output accepted: %q", out)
		}
	}
	started := `started playing in 汽水音乐 (media key)` + "\n" + `{"l0":{"kind":"foreground","passed":false,"uncertain":true,"detail":"MEDIA_UNVERIFIED"}}`
	if !unverifiedMediaPlay("media.play", started, "还没有确认开始播放") {
		t.Fatal("key-only started playing must stay unverified")
	}
	verified := `verified playing in 汽水音乐; title="x"; artist=""; shuffle=false` + "\n" + `{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`
	if unverifiedMediaPlay("media.play", verified, "") {
		t.Fatal("verified media-session must close the play loop")
	}
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "放一首歌"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "music", Name: "media.play"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "music", Content: "sent play"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "other", Name: "desktop.open"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "other", Content: "verified playing"},
	}
	if got := lastNamedToolOutput(messages, "media.play"); got != "sent play" {
		t.Fatalf("wrong receipt: %q", got)
	}
	if got := mediaTurnResultSpeech(messages); strings.Contains(got, "已经在播") {
		t.Fatal(got)
	}
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleUser, Content: "再播放"})
	if got := lastNamedToolOutput(messages, "media.play"); got != "" {
		t.Fatalf("old turn reused: %q", got)
	}
}

func TestAssistantPausedMidTask(t *testing.T) {
	paused := []string{
		"找到 59 个技能目录，请确认是否继续安装。",
		"要不要我继续执行？",
		"Shall I proceed with the install?",
		"Waiting for your confirmation before I continue.",
	}
	for _, s := range paused {
		if !assistantPausedMidTask(s) {
			t.Fatalf("expected pause: %q", s)
		}
	}
	done := []string{
		"已全部安装完成，共 59 个技能。",
		"All done, successfully installed.",
		"这是一份代码审查，没有后续动作。",
		"接下来我会用命令行查看 SKILL.md。",
		"先看一下桌面再操作。",
		"找到 59 个技能目录，安装前要先弄清结构和安装方式。",
		"文件已写入，下一步可以打开看看。",
	}
	for _, s := range done {
		if assistantPausedMidTask(s) {
			t.Fatalf("did not expect pause: %q", s)
		}
	}
	if shouldContinueTurn("文件写好了，下一步打开网页。", true, 0, false) {
		t.Fatal("succeeded tools plus 下一步 must not nudge")
	}
	if !shouldContinueTurn("请确认是否继续安装", true, 0, false) {
		t.Fatal("explicit ask after tools should still nudge")
	}
	if !shouldContinueIncompleteWork("", "COMPUTER_STALE_FRAME: display layout changed", []string{"computer.act"}, true, 0) {
		t.Fatal("stale frame must continue")
	}
	if !shouldContinueIncompleteWork("", "stale ref e12; snapshot again", []string{"browser.act"}, true, 0) {
		t.Fatal("stale browser ref must continue")
	}
	delivered := "started playing in 汽水音乐 (media key)\n" + `{"l0":{"kind":"foreground","passed":false,"uncertain":true,"detail":"MEDIA_UNVERIFIED"}}`
	if shouldContinueIncompleteWork("好，我来播放。", delivered, []string{"media.play"}, true, 0) {
		t.Fatal("delivered media key must not continue into another tool")
	}
	if got := pickTurnContinueKind("好，我来播放。", "好，我来播放。", delivered, []string{"media.play"}, true, true, true, false, 0, "播放一首歌", true); got != "ladder" {
		t.Fatalf("unconfirmed media key must keep going until playback is verified, got %q", got)
	}
	if !shouldContinueIncompleteWork("好，我来播放。", "media.play started player", []string{"media.play"}, true, 0) {
		t.Fatal("unverified media.play must continue")
	}
	if shouldContinueIncompleteWork("好，我来播放。", "media.play started player", []string{"media.play"}, true, 1) {
		t.Fatal("unverified media.play continues only once")
	}
	if !shouldContinueIncompleteWork("正在播放周杰伦", "media.play started player", []string{"media.play"}, true, 0) {
		t.Fatal("assistant claim is not playback evidence")
	}
	if got := pickTurnContinueKind("这次没有完成。", "这次没有完成。", "ok:false\nnot found", []string{"media.play"}, true, true, true, false, 0, "放一首复古公路风", true); got != "ladder" {
		t.Fatalf("failed media.play must escalate to named computer control, got %q", got)
	}
	if pickTurnContinueKind("好，我再点一下。", "好，我再点一下。", "clicked 播放", []string{"media.play", "computer.act"}, true, true, true, false, 0, "打开汽水音乐随机播放一首歌曲", true) != "" {
		t.Fatal("named click already ran; do not keep looping the same desktop act")
	}
	if shouldContinueIncompleteWork("文件写好了，下一步打开网页。", "ok:true\nwritten", []string{"workspace.write"}, true, 0) {
		t.Fatal("successful write plus 下一步 must not extra-loop")
	}
	if pickTurnContinueKind("好，我来操作电脑。", "好，我来操作电脑。", "ok:false\nCAPABILITY_NOT_READY: 请先在设置中启用电脑控制", []string{"desktop.type"}, true, true, false, false, 0, "帮我点确定", true) != "" {
		t.Fatal("capability denial must not desktop-nudge or empty-spin")
	}
	if !shouldContinueDesktopTurn("好，我来操作电脑。", 0) {
		t.Fatal("lead-in after desktop tools must continue")
	}
	if !shouldContinueDesktopTurn("", 0) {
		t.Fatal("silent stop after desktop tools must continue")
	}
	if shouldContinueDesktopTurn("Word 里已经写上号码了。", 0) {
		t.Fatal("result sentence must settle")
	}
	if shouldContinueDesktopTurn("请你在保存对话框点保存。", 0) {
		t.Fatal("file-dialog handoff must settle")
	}
	if !shouldContinueDesktopTurn("已经打开记事本。", 0) {
		t.Fatal("opened an app is not the user goal")
	}
	if companionGoalIsOpenOnly("帮我打开桌面汽水") != true {
		t.Fatal("open-only")
	}
	if companionGoalIsOpenOnly("打开记事本然后填身份证") != false {
		t.Fatal("open-then-act")
	}
	if shouldContinueDesktopTurnGoal("已经打开了汽水音乐。", "打开汽水", 0) {
		t.Fatal("open-only result must settle")
	}
	if !shouldContinueDesktopTurnGoal("已经打开记事本。", "打开记事本帮我写号码", 0) && !companionGoalIsOpenOnly("打开记事本帮我写号码") {
		t.Fatal("open-then-act must still continue after open")
	}
	if got := pickTurnContinueKind("已经打开了。", "已经打开了。", "opened C:\\\\x\\\\汽水音乐.lnk", []string{"desktop.open"}, true, true, true, true, 0, "打开汽水", true); got != "" {
		t.Fatalf("open-only must not return desktop, got %q", got)
	}
	if got := pickTurnContinueKind("已经打开第一条。", "已经打开第一条。", "url: https://news.example/first\nfirst_hit: true\n已打开第一条。", []string{"web.fetch"}, true, true, true, true, 0, "帮我打开网站上的第一个新闻", true); got != "" {
		t.Fatalf("opened first news must stop, got %q", got)
	}
	if got := pickTurnContinueKind("周杰伦有这些新闻。", "周杰伦有这些新闻。", "results_url: https://cn.bing.com/search?q=jay", []string{"web.search"}, true, false, true, true, 0, "打开网站搜索周杰伦最新新闻", true); got != "" {
		t.Fatalf("search result must stop, got %q", got)
	}
	if drop := dropCompanionFailedTail([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: "打开汽水"}, {Role: llmadapter.RoleAssistant, Content: "无法执行。窗口没到前台"}}); len(drop) != 1 || drop[0].Role != llmadapter.RoleUser {
		t.Fatal("fresh visit must drop last 无法执行 assistant")
	}
	// C7-6: assembled chat.start is [system, prior user, failed assistant, new user].
	c76 := dropCompanionFailedTail([]llmadapter.Message{
		{Role: llmadapter.RoleSystem, Content: "月伴身份"},
		{Role: llmadapter.RoleUser, Content: "打开汽水"},
		{Role: llmadapter.RoleAssistant, Content: "无法执行。窗口没到前台"},
		{Role: llmadapter.RoleUser, Content: "再打开一次汽水"},
	})
	if len(c76) != 3 || c76[0].Role != llmadapter.RoleSystem || c76[1].Content != "打开汽水" || c76[2].Content != "再打开一次汽水" {
		t.Fatalf("C7-6 must drop the failed assistant sitting before the new user turn: %#v", c76)
	}
	for _, m := range c76 {
		if strings.Contains(m.Content, "无法执行") {
			t.Fatal("C7-6 first-visit messages must not carry 无法执行")
		}
	}
	keep := dropCompanionFailedTail([]llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "今晚月色如何"},
		{Role: llmadapter.RoleAssistant, Content: "今晚是满月，适合抬头。"},
		{Role: llmadapter.RoleUser, Content: "再讲一句"},
	})
	if len(keep) != 3 || keep[1].Content != "今晚是满月，适合抬头。" {
		t.Fatalf("settled chat must stay: %#v", keep)
	}
	if !shouldContinueDesktopTurn("点完了。", 0) {
		t.Fatal("clicked is process, not done")
	}
	if !shouldContinueDesktopTurn("点了一下。", 3) {
		t.Fatal("desktop nudge budget is 5")
	}
	if shouldContinueDesktopTurn("点了一下。", 5) {
		t.Fatal("nudge budget exhausted")
	}
	if !companionWantsDesktopControl("帮我点保存") || companionWantsDesktopControl("今晚月色如何") {
		t.Fatal("desktop-control intent")
	}
	if !companionWantsDesktopControl("打开网易云") || !companionWantsDesktopControl("播放周杰伦") {
		t.Fatal("open/play must use the 24-step desktop loop")
	}
	if !isDesktopControlTool("desktop.open") || !isDesktopControlTool("media.play") || !isDesktopControlTool("browser.act") {
		t.Fatal("open/play/browser must raise the companion tool budget")
	}
	if got := pickTurnContinueKind("好，我来操作电脑。", "好，我来操作电脑。", "screenshot frameId=01ARZ3NDEKTSV4RRFFQ69G5FAV", []string{"computer.act"}, true, true, true, true, 0, "", true); got != "desktop" {
		t.Fatalf("screenshot + lead-in must keep desktop loop, got %q", got)
	}
	if got := pickTurnContinueKind("好，我帮你查一下。", "好，我帮你查一下。", "ok", []string{"web.search"}, true, false, true, true, 0, "", true); got != "leadin" {
		t.Fatalf("non-desktop lead-in must ask for a spoken result, got %q", got)
	}
	if got := pickTurnContinueKind("Word 里已经写上号码了。", "Word 里已经写上号码了。", `typed "204040"`, []string{"desktop.type"}, true, true, true, true, 0, "", true); got != "" {
		t.Fatalf("settled desktop result must stop, got %q", got)
	}
	wechatOut := "opened chat \"_穆_\" and sent \"你好\"\nvisible:\n怎么这么晚还不睡"
	wechatGoal := "和微信的_穆_聊5分钟"
	if got := pickTurnContinueKind("已经发给他了。", "已经发给他了。", wechatOut, []string{"desktop.type"}, true, true, true, false, 0, wechatGoal, true); got != "desktop" {
		t.Fatalf("wechat chat must keep going after the first send, got %q", got)
	}
	if got := pickTurnContinueKind("已经发给他了。", "已经发给他了。", wechatOut, []string{"desktop.type"}, true, true, true, false, 5, wechatGoal, true); got != "" {
		t.Fatalf("wechat chat must stop at the minute cap, got %q", got)
	}
	if got := pickTurnContinueKind("已经发给他了。", "已经发给他了。", wechatOut, []string{"desktop.type"}, true, true, true, false, 6, "跟微信里的张三聊天，聊10分钟", true); got != "desktop" {
		t.Fatalf("a 10 minute wechat chat must still continue at nudge 6, got %q", got)
	}
	if speech := wechatChatProgressSpeech(wechatGoal, []string{"desktop.type"}, []llmadapter.Message{{Role: llmadapter.RoleTool, Content: wechatOut}}); !strings.Contains(speech, "怎么这么晚还不睡") || strings.Contains(speech, "供应商拒绝了请求") {
		t.Fatalf("wechat progress speech=%q", speech)
	}
	long := "合肥今天的天气我手头没有实时数据，没法给你准确温度。你要是不急，我可以帮你查一下，稍等。"
	if got := pickTurnContinueKind("", long, "", nil, false, false, true, true, 0, "今天合肥的天气怎么样", true); got != "wait" {
		t.Fatalf("long wait promise must continue, got %q", got)
	}
	if got := pickTurnContinueKind("", "稍等。", "", nil, false, false, true, true, 0, "你好", false); got != "" {
		t.Fatal("idle wait without tools must not loop")
	}
	folder := "帮我在桌面创建一个文件夹，名字叫可可"
	if got := pickTurnContinueKind("I'll create that folder on your desktop.", "I'll create that folder on your desktop.", "", nil, false, false, false, true, 0, folder, true); got != "wait" {
		t.Fatalf("english mkdir lead-in with tools attached must continue, got %q", got)
	}
	if got := pickTurnContinueKind("好，我来创建。", "好，我来创建。", "", nil, false, false, false, true, 0, folder, true); got != "wait" {
		t.Fatalf("chinese mkdir lead-in with tools attached must continue, got %q", got)
	}
	del := "帮我删掉桌面上的可可"
	if got := pickTurnContinueKind("I'll delete that folder on your desktop.", "I'll delete that folder on your desktop.", "", nil, false, false, false, true, 0, del, true); got != "wait" {
		t.Fatalf("english delete lead-in must continue, got %q", got)
	}
	if got := pickTurnContinueKind("好，我来删除。", "好，我来删除。", "", nil, false, false, false, true, 0, del, true); got != "wait" {
		t.Fatalf("chinese delete lead-in must continue, got %q", got)
	}
	if got := pickTurnContinueKind("I'll copy that file to your desktop.", "I'll copy that file to your desktop.", "", nil, false, false, false, true, 0, "帮我把这个文件复制到桌面", true); got != "wait" {
		t.Fatalf("english copy lead-in must continue, got %q", got)
	}
	if got := pickTurnContinueKind("I'll rename that folder.", "I'll rename that folder.", "", nil, false, false, false, true, 0, "帮我把桌面上的可可改名叫可可2", true); got != "wait" {
		t.Fatalf("english rename lead-in must continue, got %q", got)
	}
	if !companionWantsDesktopControl(del) || !companionWantsTools(del) {
		t.Fatal("desktop delete must attach tools and desktop control")
	}
	if !computerExecutionTurn(del) {
		t.Fatal("desktop delete must use the computer execution contract")
	}
	if got := pickTurnContinueKind("", "手头没有那本书。", "", nil, false, false, true, true, 0, "今晚月色如何", true); got != "" {
		t.Fatal("book-missing chat must not wait")
	}
	if got := pickTurnContinueKind("", long, "", nil, false, false, true, true, 3, "今天合肥的天气怎么样", true); got != "" {
		t.Fatal("wait budget exhausted must stop kind")
	}
	if got := pickTurnContinueKind("", "", "", nil, false, false, true, true, 0, "今天合肥的天气怎么样", true); got != "wait" {
		t.Fatal("empty lead-in with tools attached must wait")
	}
	if got := pickTurnContinueKind("好，我来执行。", "好，我来执行。", "", nil, false, false, true, true, 0, "没成功，能不能换一种方式？", false); got != "" {
		t.Fatalf("lead-in without tools must not wait-loop, got %q", got)
	}
	review := "已按技能复核。我来执行修改并给出流程图。\n```mermaid\nflowchart TD\nA-->B"
	if got := pickTurnContinueKind(review, review, `{"hasMore":false,"text":"ok"}`, []string{"skill.view"}, true, false, false, false, 0, "修改周报技能", true); got != "" {
		t.Fatalf("typed skill review containing 我来执行 must not wait-loop, got %q", got)
	}
	if got := pickTurnContinueKind("技能已保存。", "技能已保存。", "ok:true skillId=01ARZ3NDEKTSV4RRFFQ69G5FAV", []string{"skill.manage"}, true, false, false, false, 0, "修改周报技能", true); got != "" {
		t.Fatalf("successful skill.manage must stop, got %q", got)
	}
	if !strings.Contains(companionStuckLeadInSpeech("播放周杰伦", "没成功，能不能换一种方式？"), "播放") {
		t.Fatal("lead-in without a tool call must speak a playback failure, not freeze")
	}
}

func TestDesktopFilenameFragmentWithoutOpenIsNotOpenOnly(t *testing.T) {
	frag := "桌面上的日常操作功能增补文档"
	if companionGoalIsOpenOnly(frag) {
		t.Fatal("T29 fragment without 打开 is not open-only")
	}
	if err := guardCurrentTurnTool(frag, "workspace.list"); err == nil {
		t.Fatal("T29 fragment must not list the workspace")
	}
}

func TestCompanionGoalIsOpenOnly(t *testing.T) {
	if !companionGoalIsOpenOnly("打开桌面上的日常操作功能增补文档") {
		t.Fatal("filename 增补文档 is not a write command")
	}
	if !companionGoalIsOpenOnly("打开桌面上的手写文档") {
		t.Fatal("filename 手写文档 is not a write command")
	}
	if companionGoalIsOpenOnly("打开记事本帮我写号码") {
		t.Fatal("open then write is not open-only")
	}
	if companionGoalIsOpenOnly("打开记事本并写你好") {
		t.Fatal("open+type is not open-only")
	}
	if companionGoalIsOpenOnly("打开记事本然后填身份证") {
		t.Fatal("open-then-act")
	}
}

func TestOpenOnlyStopsAfterSuccessfulDesktopOpenEvenIfLaterTool(t *testing.T) {
	goal := "打开记事本"
	out := "opened notepad\n{\"l0\":{\"kind\":\"foreground\",\"passed\":true}}"
	tools := []string{"desktop.open", "workspace.read"}
	if !desktopOpenSucceeded(out, tools) {
		t.Fatal("successful desktop.open this turn must count even if it is not last")
	}
	if got := pickTurnContinueKind("已经打开记事本。", "已经打开记事本。", out, tools, true, true, true, true, 0, goal, true); got != "" {
		t.Fatalf("open-only must stop after opened receipt, got %q", got)
	}
	stale := "opened notepad\nok:false\nCOMPUTER_STALE_FRAME"
	if got := pickTurnContinueKind("好，我来操作电脑。", "好，我来操作电脑。", stale, []string{"desktop.open", "computer.act"}, true, true, true, true, 0, "打开桌面的《企业AI智能助手》txt文件", true); got != "" {
		t.Fatalf("open-only must not keep looping after a later stale click, got %q", got)
	}
}

type continueAdapter struct {
	calls    int
	sawNudge bool
}

func (a *continueAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *continueAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *continueAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, "继续执行用户的指令直到完成") {
			a.sawNudge = true
		}
	}
	switch a.calls {
	case 1:
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	case 2:
		text := "找到 59 个技能目录，请确认是否继续安装。"
		if err := emit(llmadapter.Delta{Text: text}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
	default:
		text := "已全部安装完成。"
		if err := emit(llmadapter.Delta{Text: text}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
	}
}

func runContinueStream(t *testing.T, adapter llmadapter.Adapter, req llmadapter.Request) []string {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-continue"
	e.streams[id] = state
	events := make(chan bridge.Event, 32)
	done := make(chan struct{})
	var deltas []string
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas = append(deltas, ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	return deltas
}

func TestRunStreamContinuesAfterPrematureStop(t *testing.T) {
	adapter := &continueAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{Model: "m"})
	joined := strings.Join(deltas, "")
	if !adapter.sawNudge {
		t.Fatal("expected a continue nudge after the model paused mid-task")
	}
	if adapter.calls < 3 {
		t.Fatalf("calls = %d, want at least 3 (tool, pause, finish)", adapter.calls)
	}
	if !strings.Contains(joined, "已全部安装完成") {
		t.Fatalf("final answer missing: %q", joined)
	}
}

func TestRunStreamContinuesAskWhenReasoningDisabled(t *testing.T) {
	adapter := &continueAdapter{}
	_ = runContinueStream(t, adapter, llmadapter.Request{Model: "m", DisableReasoning: true})
	if !adapter.sawNudge {
		t.Fatal("DisableReasoning must not block a mid-task continue nudge")
	}
	if adapter.calls < 3 {
		t.Fatalf("calls = %d, want at least 3 (tool, pause, finish)", adapter.calls)
	}
}

type finishAdapter struct{ calls int }

func (a *finishAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *finishAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *finishAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	}
	text := "已全部安装完成，共 3 个技能。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

type waitPromiseAdapter struct{ calls int }

func (a *waitPromiseAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *waitPromiseAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *waitPromiseAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	text := "合肥今天的天气我手头没有实时数据，没法给你准确温度。你要是不急，我可以帮你查一下，稍等。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestRunStreamPromiseTriggersHostToolFallback(t *testing.T) {
	adapter := &waitPromiseAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, companion: true}
	id := "stream-wait-close"
	e.streams[id] = state
	done := make(chan struct{})
	var deltas []string
	toolStarts := 0
	req := llmadapter.Request{
		Model:            "m",
		DisableReasoning: true,
		Tools:            []llmadapter.ToolDefinition{{Name: "web.search"}},
		Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "今天合肥的天气怎么样"}},
	}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error {
		if event.Type == bridge.EventDelta && event.Delta != nil {
			deltas = append(deltas, event.Delta.Text)
		}
		if event.Type == bridge.EventToolStarted && event.Tool != nil && event.Tool.Name == "web.search" {
			toolStarts++
		}
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(done)
		}
		return nil
	}, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for wait-closeout")
	}
	joined := strings.Join(deltas, "")
	if toolStarts != 1 {
		t.Fatalf("promise-only response must trigger one web.search, starts=%d text=%q", toolStarts, joined)
	}
	if adapter.calls < 2 {
		t.Fatalf("calls=%d, want the model to receive the fallback result", adapter.calls)
	}
}

func TestRunStreamTypedPromiseTriggersHostToolFallback(t *testing.T) {
	adapter := &waitPromiseAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, companion: false}
	id := "stream-typed-fallback"
	e.streams[id] = state
	done := make(chan struct{})
	toolStarts := 0
	req := llmadapter.Request{
		Model:    "m",
		Tools:    []llmadapter.ToolDefinition{{Name: "web.search"}},
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "今天合肥的天气怎么样"}},
	}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error {
		if event.Type == bridge.EventToolStarted && event.Tool != nil && event.Tool.Name == "web.search" {
			toolStarts++
		}
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(done)
		}
		return nil
	}, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed fallback")
	}
	if toolStarts != 1 {
		t.Fatalf("typed promise-only response must trigger one web.search, starts=%d", toolStarts)
	}
}

func TestRunStreamL2AskDoesNotAutoWebSearch(t *testing.T) {
	adapter := &waitPromiseAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{
		cancel: cancel,
		state:  streamRunning,
		lane:   buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{}),
	}
	id := "stream-l2ask-no-search"
	e.streams[id] = state
	done := make(chan struct{})
	toolStarts := 0
	req := llmadapter.Request{
		Model:            "m",
		DisableReasoning: true,
		Tools:            []llmadapter.ToolDefinition{{Name: "web.search"}},
		Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "今天合肥的天气怎么样"}},
	}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error {
		if event.Type == bridge.EventToolStarted && event.Tool != nil && event.Tool.Name == "web.search" {
			toolStarts++
		}
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(done)
		}
		return nil
	}, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for L2-ask closeout")
	}
	if toolStarts != 0 {
		t.Fatalf("L2-ask must not auto-inject web.search, starts=%d", toolStarts)
	}
}

func TestRunStreamInventoryLookupDoesNotAutoWebSearch(t *testing.T) {
	adapter := &waitPromiseAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, companion: true}
	id := "stream-inventory-no-scrape"
	e.streams[id] = state
	done := make(chan struct{})
	webStarts, mcpStarts := 0, 0
	req := llmadapter.Request{
		Model:            "m",
		DisableReasoning: true,
		Tools:            []llmadapter.ToolDefinition{{Name: "web.search"}, {Name: "mcp.search"}},
		Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "今天上海到合肥高铁票有哪些"}},
	}
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, req, func(event bridge.Event) error {
		if event.Type == bridge.EventToolStarted && event.Tool != nil {
			switch event.Tool.Name {
			case "web.search":
				webStarts++
			case "mcp.search":
				mcpStarts++
			}
		}
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(done)
		}
		return nil
	}, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for inventory fail-closed")
	}
	if webStarts != 0 {
		t.Fatalf("live tickets must not auto-inject web.search, starts=%d", webStarts)
	}
	if mcpStarts != 1 {
		t.Fatalf("live tickets should probe mcp.search once, starts=%d", mcpStarts)
	}
}

func TestRunStreamDoesNotNudgeWhenTaskFinished(t *testing.T) {
	adapter := &finishAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-finished"
	e.streams[id] = state
	done := make(chan struct{})
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "m"}, func(event bridge.Event) error {
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(done)
		}
		return nil
	}, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	if adapter.calls != 2 {
		t.Fatalf("calls = %d, want 2 (no extra continue turn)", adapter.calls)
	}
}

type skillCreateAdapter struct{ turn int }

// UX-05 #3: the forced end-of-turn summary pass must run WITHOUT tools and
// carry a system nudge that forbids further tool calls and requires a
// natural-language wrap-up, so a budget-exhausted multi-tool loop never
// finishes silently.
func TestForceSummaryNudgeMessageContract(t *testing.T) {
	msg := forceSummaryNudgeMessage()
	if msg.Role != llmadapter.RoleSystem {
		t.Fatalf("role = %q, want system", msg.Role)
	}
	for _, want := range []string{"最终总结", "还有哪些没做完", "不要提到步数"} {
		if !strings.Contains(msg.Content, want) {
			t.Fatalf("nudge missing %q: %q", want, msg.Content)
		}
	}
}

func (a *skillCreateAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *skillCreateAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *skillCreateAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if a.turn == 0 {
		a.turn++
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "call-create", Name: "skill.create", Arguments: []byte(`{"name":"folder-reader","displayName":"Folder Reader","description":"read folders","permissions":["read_only"],"entryPoint":"SKILL.md","manifestJson":"{\"prompt\":\"read\",\"triggers\":[\"读取\"]}"}`)},
		}}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "技能已创建"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Usage: llmadapter.Usage{OutputTokens: 2, TotalTokens: 2}}, nil
}

type skillCreateRecordingStub struct {
	skillCatalogStub
	created skill.Skill
}

func (s *skillCreateRecordingStub) Create(_ context.Context, sk skill.Skill) (skill.Skill, error) {
	sk.ID = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	sk.Status = skill.SkillStatusDraft
	s.created = sk
	return sk, nil
}

func TestSkillCreateToolCreatesDraft(t *testing.T) {
	stub := &skillCreateRecordingStub{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.skills = stub
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return &skillCreateAdapter{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-skill-create"
	e.streams[id] = state
	done := make(chan struct{})
	var summaries []string
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "m"}, func(event bridge.Event) error {
		if event.Type == bridge.EventToolCompleted && event.Tool != nil {
			summaries = append(summaries, event.Tool.Summary)
		}
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(done)
		}
		return nil
	}, "01ARZ3NDEKTSV4RRFFQ69G5FAV", executionModeFullAccess)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	if stub.created.Name != "folder-reader" {
		t.Fatalf("created = %#v", stub.created)
	}
	if len(summaries) != 1 || !strings.Contains(summaries[0], "已创建") {
		t.Fatalf("summaries = %#v", summaries)
	}
}

func TestOfficeInspectWithoutGenerateContinuesTenPagePPT(t *testing.T) {
	goal := "帮我参考附件，自己思考做一个10页的PPT"
	if got := pickTurnContinueKind("已读取附件。", "已读取附件。", `{"view":"text"}`, []string{"office.inspect"}, true, false, false, true, 0, goal, true); got != "incomplete" {
		t.Fatalf("inspect-only PPT goal continue=%q want incomplete", got)
	}
	if got := pickTurnContinueKind("PPT 已生成。", "PPT 已生成。", "ok:true", []string{"office.inspect", "office.generate"}, true, false, false, true, 0, goal, true); got != "" {
		t.Fatalf("generate receipt still continued: %q", got)
	}
	if got := pickTurnContinueKind("附件主要是问卷。", "附件主要是问卷。", `{"view":"text"}`, []string{"office.inspect"}, true, false, false, true, 0, "帮我看看这个附件写了什么", true); got != "" {
		t.Fatalf("review-only inspect continued: %q", got)
	}
	if got := pickTurnContinueKind("已读完材料。", "已读完材料。", `{"view":"text"}`, []string{"office.inspect"}, true, false, false, true, 0, "写周报", true); got != "incomplete" {
		t.Fatalf("weekly-report inspect-only continue=%q want incomplete", got)
	}
}

func TestAnnouncedWriteAfterReadKeepsTheTurnGoing(t *testing.T) {
	text := "继续收尾。并行执行两件独立任务：① 写入自测脚本（用真实浏览器内核跑交互链验证）；② 技能固化——四条硬性规则写入「POC 快速构建」："
	got := pickTurnContinueKind(text, text, "ok", []string{"workspace.read", "skill.list"}, true, false, false, false, 0, "把拒绝的，前面没做完的做完", true)
	if got != "act" {
		t.Fatalf("announced work continue=%q want act", got)
	}
	done := "自测脚本已经写入，技能规则也已经写入「POC 快速构建」。"
	if got := pickTurnContinueKind(done, done, "ok", []string{"workspace.write"}, true, false, false, false, 0, "把拒绝的，前面没做完的做完", true); got != "" {
		t.Fatalf("finished write still continued: %q", got)
	}
	// A checklist is the plan, not the file. Stopping here is what made the
	// same paragraph come back after every 继续.
	if got := pickTurnContinueKind(text, text, "ok", []string{"todo.write"}, true, false, false, false, 0, "把拒绝的，前面没做完的做完", true); got != "act" {
		t.Fatalf("plan-only continue=%q want act", got)
	}
}

func TestStepLimitExcuseKeepsTheTurnGoing(t *testing.T) {
	text := "这轮还没能播上：工具调用步数达到上限，media.play 没有实际执行。你只需再发一句。"
	if !turnAdmitsUnfinishedToolBudget(text) {
		t.Fatal("a step-limit handoff must continue in the same turn")
	}
	if !shouldExtendPastPreparatoryStep([]string{"skill.invoke"}, 0, 1, 0) {
		t.Fatal("loading a skill must not be the last tool step")
	}
	if !shouldExtendPastPreparatoryStep([]string{"todo.write"}, 0, 1, 0) {
		t.Fatal("writing the checklist must not be the last tool step")
	}
	if shouldExtendPastPreparatoryStep([]string{"media.play"}, 0, 1, 0) {
		t.Fatal("a finished play call must not extend the step budget")
	}
	if shouldExtendPastPreparatoryStep([]string{"skill.invoke"}, 3, 1, 0) {
		t.Fatal("preparatory extension stops after three waves")
	}
	if !mediaCenterPlayStillPending("在媒体中心播放《大都会》", []string{"skill.invoke"}) {
		t.Fatal("media center playback is still pending after the skill loads")
	}
	if mediaCenterPlayStillPending("在媒体中心播放《大都会》", []string{"media.play"}) {
		t.Fatal("playback already started")
	}
	if announcedWorkStillPending("请问脚本要写到哪个文件？我再写入。", []string{"workspace.read"}) {
		t.Fatal("a question must wait for the user")
	}
	if !shouldContinueTurn("找到 59 个技能目录，请确认是否继续安装。", true, 0, false) {
		t.Fatal("a continue handoff still finishes the batch")
	}
}

type wireLimitAdapter struct {
	calls       int
	secondLevel string
	secondThink bool
}

func (a *wireLimitAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *wireLimitAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *wireLimitAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		_ = emit(llmadapter.Delta{Reasoning: "先把整篇论证在脑子里写完"})
		return llmadapter.Response{}, &llmadapter.Error{Code: "RESPONSE_BODY_TOO_LARGE", Stage: llmadapter.StageStream}
	}
	if a.calls == 2 {
		// Continuation waves re-think from scratch; production traces
		// (2026-10-06/07) show multi-minute thinking before a cut, so the
		// re-dispatch demotes effort to low instead of keeping "max".
		a.secondLevel = req.ReasoningLevel
		a.secondThink = !req.DisableReasoning
	}
	text := "论文正文：时间与空间可以交错，人可以穿过它们。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestRunStreamWritesTheAnswerAfterTheResponseIsCut(t *testing.T) {
	adapter := &wireLimitAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "m",
		ReasoningLevel: "max",
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我写一个时间空间论证，人可以穿越时空的，长篇分析论文"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.calls > 12 || adapter.secondLevel != "low" || !adapter.secondThink {
		t.Fatalf("calls=%d secondLevel=%q secondThink=%v, want the cut wave to continue at demoted (low, enabled) effort", adapter.calls, adapter.secondLevel, adapter.secondThink)
	}
	if !strings.Contains(joined, "论文正文") || strings.Contains(joined, "无法执行") {
		t.Fatalf("paper missing or replaced by the failure notice: %q", joined)
	}
}

type wireLimitToolAdapter struct {
	calls       int
	keptTools   bool
	secondLevel string
	secondThink bool
}

func (a *wireLimitToolAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *wireLimitToolAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *wireLimitToolAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		_ = emit(llmadapter.Delta{Reasoning: "先把整份改动在脑子里重写一遍"})
		return llmadapter.Response{}, &llmadapter.Error{Code: "RESPONSE_BODY_TOO_LARGE", Stage: llmadapter.StageStream}
	}
	hasWrite, hasTodo, hasAsk := false, false, false
	for _, tool := range req.Tools {
		switch tool.Name {
		case "workspace.write":
			hasWrite = true
		case "todo.write":
			hasTodo = true
		case "user.ask":
			hasAsk = true
		}
	}
	if a.calls == 2 {
		a.keptTools = hasWrite && hasTodo && hasAsk
		// Same demote contract as wireLimitAdapter: the re-dispatched wave
		// keeps every tool but drops to low effort so it starts writing
		// instead of re-thinking for minutes.
		a.secondLevel = req.ReasoningLevel
		a.secondThink = !req.DisableReasoning
	}
	text := "已加上客户管理，并写入一条商机。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestRunStreamKeepsWorkingAfterAnyTaskIsCut(t *testing.T) {
	adapter := &wireLimitToolAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "m",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "workspace.write"}, {Name: "todo.write"}, {Name: "user.ask"}},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "在现有页面上加上客户管理，并添加一条商机数据"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || !adapter.keptTools || adapter.secondLevel != "low" || !adapter.secondThink {
		t.Fatalf("calls=%d keptTools=%v secondLevel=%q secondThink=%v, want the task to continue with its tools at demoted (low, enabled) effort", adapter.calls, adapter.keptTools, adapter.secondLevel, adapter.secondThink)
	}
	if !strings.Contains(joined, "已加上客户管理") || strings.Contains(joined, "无法执行") {
		t.Fatalf("result missing or replaced by the failure notice: %q", joined)
	}
	if strings.Count(joined, "已加上客户管理") != 1 {
		t.Fatalf("landed result was written again: %q", joined)
	}
}

func TestLandedAnswerIsNotPendingWork(t *testing.T) {
	if announcedWorkStillPending("已加上客户管理，并写入一条商机。", nil) {
		t.Fatal("a finished edit must not be scheduled again")
	}
	if announcedWorkStillPending("论文正文：时间与空间可以交错，人可以穿过它们。这一段论证已经写好，可以直接交给读者。", nil) {
		t.Fatal("a finished essay must not be scheduled again")
	}
	if !announcedWorkStillPending("我准备把脚本写入文件。", nil) {
		t.Fatal("a promise that has not landed must still continue")
	}
}

func TestAnyChatAnswersOnceAfterTheResponseIsCut(t *testing.T) {
	adapter := &wireLimitToolAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:    "m",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我把这三件事排个顺序：买菜、回邮件、给妈妈打电话"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls != 2 {
		t.Fatalf("calls=%d, an ordinary question must finish on the next sample", adapter.calls)
	}
	if strings.Count(joined, "已加上客户管理") != 1 || strings.Contains(joined, "无法执行") {
		t.Fatalf("ordinary answer repeated or failed: %q", joined)
	}
}

type repeatPaperAdapter struct {
	calls int
}

func (a *repeatPaperAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *repeatPaperAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *repeatPaperAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		_ = emit(llmadapter.Delta{Reasoning: "先把整篇论证在脑子里写完"})
		return llmadapter.Response{}, &llmadapter.Error{Code: "RESPONSE_BODY_TOO_LARGE", Stage: llmadapter.StageStream}
	}
	text := "论文正文：时间与空间可以交错，人可以穿过它们。这一段论证已经写好，可以直接交给读者。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

const longGenFailure = "我按论文流水线写完了正文。生成失败：小说缺少章节标题，请按章使用一级标题。请检查文档结构后再生成。这一步没有把文件落到工作区，任务停在这里。正文已经在对话里，缺的只是按一级标题重新生成那一个文件。不要再从受众和调研重新写一遍。请不要让用户再点一次继续。文件仍未生成。"

func TestGenerationFailureIsNotAFinishedAnswer(t *testing.T) {
	if answerAlreadyDelivered(longGenFailure) {
		t.Fatal("a generation failure narration is not a finished answer")
	}
}

func TestRunStreamRetriesAFailedDocumentWithoutRestarting(t *testing.T) {
	adapter := &genFailAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "m",
		ReasoningLevel: "max",
		Messages: []llmadapter.Message{{
			Role:    llmadapter.RoleUser,
			Content: "帮我写一个时间空间论证，人可以穿越时空的，长篇分析论文",
		}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.calls > 4 {
		t.Fatalf("calls=%d, want the failed generate to continue once in the same turn", adapter.calls)
	}
	if adapter.quietOnRetry || adapter.level != "max" || !adapter.sawStay {
		t.Fatalf("retry quiet=%v level=%q stay=%v, the selected level or the same essay was dropped", adapter.quietOnRetry, adapter.level, adapter.sawStay)
	}
	if !strings.Contains(joined, "已生成文档") || strings.Contains(joined, "无法执行") {
		t.Fatalf("file was not finished in the same turn: %q", joined)
	}
}

func TestResumeAfterGenerationFailureDoesNotStartOver(t *testing.T) {
	adapter := &resumeFileAdapter{}
	failure := "生成失败：小说缺少章节标题，请按章使用一级标题。正文已经写好，文件没有落到工作区。"
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "m",
		ReasoningLevel: "max",
		Messages: []llmadapter.Message{
			{Role: llmadapter.RoleUser, Content: "帮我写一个时间空间论证，人可以穿越时空的，长篇分析论文"},
			{Role: llmadapter.RoleAssistant, Content: failure},
			{Role: llmadapter.RoleUser, Content: "继续上次未完成的工作。结合任务清单、已完成步骤和我补充过的说明，接着做到完成。"},
		},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls != 1 || adapter.quiet || adapter.level != "max" || !adapter.sawStay {
		t.Fatalf("calls=%d quiet=%v level=%q stay=%v, resume started the paper over or dropped the level", adapter.calls, adapter.quiet, adapter.level, adapter.sawStay)
	}
	if !strings.Contains(joined, "已生成文档") || strings.Contains(joined, failure) {
		t.Fatalf("resume did not finish the file: %q", joined)
	}
}

type genFailAdapter struct {
	calls        int
	quietOnRetry bool
	level        string
	sawStay      bool
}

func (a *genFailAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *genFailAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *genFailAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		text := longGenFailure
		if err := emit(llmadapter.Delta{Text: text}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
	}
	a.quietOnRetry = req.DisableReasoning
	a.level = req.ReasoningLevel
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, "不要从头") {
			a.sawStay = true
		}
	}
	text := "已生成文档，并写到工作区。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

type resumeFileAdapter struct {
	calls   int
	quiet   bool
	level   string
	sawStay bool
}

func (a *resumeFileAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *resumeFileAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *resumeFileAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, "不要从头") {
			a.sawStay = true
		}
	}
	text := "已生成文档，并写到工作区。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestRunStreamDoesNotRepeatALandedAnswer(t *testing.T) {
	adapter := &repeatPaperAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:    "m",
		Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我写一个时间空间论证，人可以穿越时空的，长篇分析论文"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls > 3 {
		t.Fatalf("calls=%d, the same answer was sent back to the model", adapter.calls)
	}
	if strings.Count(joined, "论文正文") != 1 || strings.Contains(joined, "无法执行") {
		t.Fatalf("answer repeated or replaced: calls=%d text=%q", adapter.calls, joined)
	}
}

type paperEmptyAdapter struct {
	calls       int
	firstQuiet  bool
	firstLevel  string
	secondQuiet bool
	secondLevel string
	toldToWrite bool
}

func (a *paperEmptyAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *paperEmptyAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *paperEmptyAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	told := false
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, "这一轮直接写成文件") {
			told = true
		}
	}
	if a.calls == 1 {
		a.firstQuiet = req.DisableReasoning
		a.firstLevel = req.ReasoningLevel
		a.toldToWrite = told
		_ = emit(llmadapter.Delta{Reasoning: "先把整篇论文在脑子里写完，先不落文件"})
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant}}, nil
	}
	a.secondQuiet = req.DisableReasoning
	a.secondLevel = req.ReasoningLevel
	if told {
		a.toldToWrite = true
	}
	text := "已生成文档，并写到工作区。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestRunStreamWritesAPaperWithoutALongThink(t *testing.T) {
	adapter := &paperEmptyAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我写一个时间空间论证，人可以穿越时空的，长篇分析论文，写成 Word"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls != 2 || adapter.firstQuiet || adapter.firstLevel != "max" || adapter.secondQuiet || adapter.secondLevel != "max" || !adapter.toldToWrite {
		t.Fatalf("calls=%d firstQuiet=%v firstLevel=%q secondQuiet=%v secondLevel=%q told=%v", adapter.calls, adapter.firstQuiet, adapter.firstLevel, adapter.secondQuiet, adapter.secondLevel, adapter.toldToWrite)
	}
	if !strings.Contains(joined, "已生成文档") || strings.Contains(joined, "无法执行") {
		t.Fatalf("paper did not finish in the same turn: %q", joined)
	}
}

func openPageEditRequest() llmadapter.Request {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	body := "加上客户管理，再加一条商机\n\n[正在看的页面文件]\n" + path + "\n" + openPageFileInstruction + "\n\n[浏览器页面]\n" + strings.Repeat("商机赢单率", 80)
	return llmadapter.Request{
		Model:          "m",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "workspace.edit"}},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: body}},
	}
}

func TestOpenPageEditDropsThePageDumpAfterProviderRejects(t *testing.T) {
	adapter := &pageRejectAdapter{}
	deltas := runContinueStream(t, adapter, openPageEditRequest())
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.calls > 4 {
		t.Fatalf("calls=%d, the rejected edit did not continue", adapter.calls)
	}
	if adapter.quiet || adapter.level != "max" || !adapter.keptTools || adapter.stillDump {
		t.Fatalf("quiet=%v level=%q tools=%v dump=%v", adapter.quiet, adapter.level, adapter.keptTools, adapter.stillDump)
	}
	if strings.Contains(joined, "无法执行") || !strings.Contains(joined, "已加上客户管理") {
		t.Fatalf("edit did not finish: %q", joined)
	}
}

func TestOpenPageEditDoesNotStopAtALongNarration(t *testing.T) {
	adapter := &pageNarrationAdapter{}
	deltas := runContinueStream(t, adapter, openPageEditRequest())
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.calls > 4 || !adapter.sawEdit {
		t.Fatalf("calls=%d sawEdit=%v, the narration ended the edit", adapter.calls, adapter.sawEdit)
	}
	if strings.Contains(joined, "无法执行") || !strings.Contains(joined, "已加上客户管理") {
		t.Fatalf("edit did not finish: %q", joined)
	}
}

type landedPageRejectAdapter struct {
	calls    int
	quiet    bool
	level    string
	argRunes int
}

func (a *landedPageRejectAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *landedPageRejectAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *landedPageRejectAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		text := "已写入东航商机。"
		if err := emit(llmadapter.Delta{Text: text}); err != nil {
			return llmadapter.Response{}, err
		}
		args := `{"path":"index.html","oldText":"` + strings.Repeat("商机", 2000) + `","newText":"东航"}`
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text, ToolCalls: []llmadapter.ToolCall{{
			ID: "edit-landed-1", Name: "workspace.edit", Arguments: []byte(args),
		}}}}, nil
	}
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	for _, m := range req.Messages {
		for _, call := range m.ToolCalls {
			if call.Name == "workspace.edit" && len(call.Arguments) > a.argRunes {
				a.argRunes = len(call.Arguments)
			}
		}
	}
	return llmadapter.Response{}, &llmadapter.Error{Code: "STREAM_BAD_REQUEST", Stage: llmadapter.StageStream, Message: "upstream reported a stream error"}
}

type silentPageEditAdapter struct{ calls int }

func (a *silentPageEditAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *silentPageEditAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *silentPageEditAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
			ID: "edit-silent-1", Name: "workspace.edit", Arguments: []byte(`{"path":"index.html","oldText":"a","newText":"b"}`),
		}}}}, nil
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant}}, nil
}

func TestLandedPageEditSpeaksWhenTheModelSaysNothing(t *testing.T) {
	adapter := &silentPageEditAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	e.toolExecHook = func(context.Context, executionMode, string, string, json.RawMessage) (toolruntime.Result, error) {
		return toolruntime.Result{Output: "edited index.html (1 replacement(s))"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-silent-page"
	e.streams[id] = state
	events := make(chan bridge.Event, 32)
	done := make(chan struct{})
	var deltas []string
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas = append(deltas, ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, openPageEditRequest(), func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	joined := strings.Join(deltas, "")
	if !strings.Contains(joined, "已经写进正在看的页面") {
		t.Fatalf("a landed edit stayed silent: %q", joined)
	}
	if strings.Contains(joined, "无法执行") || strings.Contains(joined, "商机赢单率") {
		t.Fatalf("silent edit leaked a failure or the page dump: %q", joined)
	}
}

func TestZodiacReportLandsInOneCall(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return &canvasOnceAdapter{}, nil
	})
	e.toolExecHook = func(context.Context, executionMode, string, string, json.RawMessage) (toolruntime.Result, error) {
		return toolruntime.Result{Output: "generated canvas.html (12000 bytes)"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-zodiac-once"
	e.streams[id] = state
	events := make(chan bridge.Event, 32)
	done := make(chan struct{})
	var deltas []string
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas = append(deltas, ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	goal := "帮我写一个关于，12星座，不同星座和不同星座的爱情，哪里匹配，哪里合适，哪里不匹配，不合适，相互之间在一起能得多少分的，长篇分析报告论文。"
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "canvas.present"}, {Name: "user.ask"}},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}},
	}, func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	joined := strings.Join(deltas, "")
	if !strings.Contains(joined, "已经放到画布上") {
		t.Fatalf("one canvas write did not report: %q", joined)
	}
}

type canvasOnceAdapter struct{ calls int }

func (a *canvasOnceAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *canvasOnceAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *canvasOnceAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls > 1 {
		return llmadapter.Response{}, errors.New("second model call")
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
		ID: "canvas-once-1", Name: "canvas.present", Arguments: []byte(`{"title":"星座","sections":[{"heading":"白羊","body":"匹配说明"}]}`),
	}}}}, nil
}

func TestLandedPageEditSurvivesSupplierRejection(t *testing.T) {
	adapter := &landedPageRejectAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	e.toolExecHook = func(context.Context, executionMode, string, string, json.RawMessage) (toolruntime.Result, error) {
		return toolruntime.Result{Output: "edited index.html (1 replacement(s))"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-landed-page"
	e.streams[id] = state
	events := make(chan bridge.Event, 32)
	done := make(chan struct{})
	var deltas []string
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas = append(deltas, ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, openPageEditRequest(), func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	joined := strings.Join(deltas, "")
	if adapter.calls != 2 || adapter.quiet || adapter.level != "max" || adapter.argRunes > 800 {
		t.Fatalf("calls=%d quiet=%v level=%q argRunes=%d", adapter.calls, adapter.quiet, adapter.level, adapter.argRunes)
	}
	if strings.Contains(joined, "无法执行") || !strings.Contains(joined, "已写入东航商机") {
		t.Fatalf("landed edit was reported as a failure: %q", joined)
	}
}

type pageRejectAdapter struct {
	calls     int
	quiet     bool
	level     string
	keptTools bool
	stillDump bool
}

func (a *pageRejectAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *pageRejectAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *pageRejectAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		return llmadapter.Response{}, &llmadapter.Error{Code: "STREAM_BAD_REQUEST", Stage: llmadapter.StageStream, Message: "upstream reported a stream error"}
	}
	if a.calls == 2 {
		a.quiet = req.DisableReasoning
		a.level = req.ReasoningLevel
		a.keptTools = len(req.Tools) > 0
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "[浏览器页面]") {
				a.stillDump = true
			}
		}
	}
	text := "已加上客户管理。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

type pageNarrationAdapter struct {
	calls   int
	sawEdit bool
}

func (a *pageNarrationAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *pageNarrationAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *pageNarrationAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		text := "我先看完整个页面，再规划客户模块的位置、字段和交互。这一段只是说明，文件还没有改。页面上已有的商机清单保持不动，等下一步再写入客户管理。"
		if err := emit(llmadapter.Delta{Text: text}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
	}
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, "只改正在看的这个文件") {
			a.sawEdit = true
		}
	}
	text := "已加上客户管理。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

type anyTaskRejectAdapter struct {
	calls    int
	quiet    bool
	level    string
	keptPlan bool
	keptAsk  bool
}

func (a *anyTaskRejectAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *anyTaskRejectAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *anyTaskRejectAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		return llmadapter.Response{}, &llmadapter.Error{Code: "STREAM_BAD_REQUEST", Stage: llmadapter.StageStream, Message: "upstream reported a stream error"}
	}
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	hasTodo, hasAsk := false, false
	for _, tool := range req.Tools {
		switch tool.Name {
		case "todo.write":
			hasTodo = true
		case "user.ask":
			hasAsk = true
		}
	}
	a.keptPlan = hasTodo
	a.keptAsk = hasAsk
	text := "计划还在，继续做完。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestAnyTaskContinuesAfterSupplierRejection(t *testing.T) {
	adapter := &anyTaskRejectAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "todo.write"}, {Name: "user.ask"}, {Name: "workspace.write"}},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "把这份周报拆成步骤并写完"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.calls > 4 || adapter.quiet || adapter.level != "max" || !adapter.keptPlan || !adapter.keptAsk {
		t.Fatalf("calls=%d quiet=%v level=%q plan=%v ask=%v", adapter.calls, adapter.quiet, adapter.level, adapter.keptPlan, adapter.keptAsk)
	}
	if strings.Contains(joined, "无法执行") || strings.Contains(joined, "纯对话模式") || !strings.Contains(joined, "计划还在") {
		t.Fatalf("task broke after the supplier rejection: %q", joined)
	}
}

type compactRejectAdapter struct {
	calls      int
	quiet      bool
	level      string
	keptPlan   bool
	keptAsk    bool
	droppedOld bool
	keptGoal   bool
}

func (a *compactRejectAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *compactRejectAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *compactRejectAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls == 1 {
		_ = emit(llmadapter.Delta{Text: "先看到旧的自测记录"})
		return llmadapter.Response{}, &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: "rejected"}
	}
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	a.droppedOld = true
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "旧自测记录-早") {
			a.droppedOld = false
		}
		if strings.Contains(m.Content, "把这份周报写完") {
			a.keptGoal = true
		}
	}
	for _, tool := range req.Tools {
		switch tool.Name {
		case "todo.write":
			a.keptPlan = true
		case "user.ask":
			a.keptAsk = true
		}
	}
	text := "周报已经写完。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

type canvasWriteAdapter struct {
	calls   int
	level   string
	quiet   bool
	told    bool
	sawStay bool
}

func (a *canvasWriteAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *canvasWriteAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *canvasWriteAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "canvas.present") && strings.Contains(m.Content, "不要只思考") {
			a.told = true
		}
		if strings.Contains(m.Content, "不要从头再想") {
			a.sawStay = true
		}
	}
	if a.calls == 1 {
		return llmadapter.Response{}, errTurnGenerationBudget
	}
	text := "星座报告已经写在画布上。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

type resumeThinkAdapter struct {
	calls   int
	sawBody bool
	level   string
	quiet   bool
}

func (a *resumeThinkAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *resumeThinkAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *resumeThinkAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "Section 7 水象：巨蟹和双鱼") && strings.Contains(m.Content, "不要重写") {
			a.sawBody = true
		}
	}
	if a.calls == 1 {
		if err := emit(llmadapter.Delta{Reasoning: "Section 7 水象：巨蟹和双鱼已经写到这里。"}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{}, &llmadapter.Error{Code: "TIMEOUT", Stage: llmadapter.StageHTTP, Message: "timeout"}
	}
	text := "已经接到上文，放上画布。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestCutThinkContinuesFromTheWrittenText(t *testing.T) {
	adapter := &resumeThinkAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "canvas.present"}, {Name: "user.ask"}},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我写一个关于12星座爱情匹配的长篇分析报告论文"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.quiet || adapter.level != "max" || !adapter.sawBody {
		t.Fatalf("calls=%d quiet=%v level=%q sawBody=%v", adapter.calls, adapter.quiet, adapter.level, adapter.sawBody)
	}
	if strings.Contains(joined, "无法执行") || !strings.Contains(joined, "已经接到上文") {
		t.Fatalf("cut think restarted instead of continuing: %q", joined)
	}
}

type pageGiveUpAdapter struct {
	calls  int
	edited bool
}

func (a *pageGiveUpAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *pageGiveUpAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *pageGiveUpAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.edited {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant}}, nil
	}
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem && strings.Contains(m.Content, "不要对用户说没成功") {
			a.edited = true
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
				ID: "edit-after-giveup", Name: "workspace.edit", Arguments: []byte(`{"path":"index.html","oldText":"a","newText":"b"}`),
			}}}}, nil
		}
	}
	if a.calls == 1 {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
			ID: "read-1", Name: "workspace.read", Arguments: []byte(`{"path":"index.html"}`),
		}}}}, nil
	}
	text := "这次操作没成功，请再说具体一点让我重试。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestPageEditDoesNotStopWhenTheModelGivesUp(t *testing.T) {
	adapter := &pageGiveUpAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	e.toolExecHook = func(_ context.Context, _ executionMode, _ string, name string, _ json.RawMessage) (toolruntime.Result, error) {
		if name == "workspace.edit" {
			return toolruntime.Result{Output: "edited index.html (1 replacement(s))"}, nil
		}
		return toolruntime.Result{Output: "read index.html"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-page-giveup"
	e.streams[id] = state
	events := make(chan bridge.Event, 64)
	done := make(chan struct{})
	var deltas []string
	go func() {
		for ev := range events {
			if ev.Type == bridge.EventDelta && ev.Delta != nil {
				deltas = append(deltas, ev.Delta.Text)
			}
			if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
				close(done)
				return
			}
		}
	}()
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, openPageEditRequest(), func(event bridge.Event) error { events <- event; return nil }, "")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	joined := strings.Join(deltas, "")
	if !adapter.edited || !strings.Contains(joined, "已经写进正在看的页面") {
		t.Fatalf("give-up ended the page edit: edited=%v text=%q", adapter.edited, joined)
	}
}

func TestPaperContinuesAfterTheTimeCut(t *testing.T) {
	adapter := &canvasWriteAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "canvas.present"}, {Name: "todo.write"}},
		Messages:       []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "帮我写一个关于12星座爱情匹配的长篇分析报告论文"}},
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.quiet || adapter.level != "max" || !adapter.told || !adapter.sawStay {
		t.Fatalf("calls=%d quiet=%v level=%q told=%v stay=%v", adapter.calls, adapter.quiet, adapter.level, adapter.told, adapter.sawStay)
	}
	if strings.Contains(joined, "无法执行") || !strings.Contains(joined, "星座报告已经写在画布上") {
		t.Fatalf("time cut ended the paper: %q", joined)
	}
}

// The stream watchdogs must be tight enough to catch a dead connection
// within minutes (production: stuck Ark Plan streams sit silent 6-12
// minutes) yet loose enough that a healthy thinking pause between deltas
// never trips them. Four idle minutes and a one-minute header wait sit
// in that band.
func TestStreamWatchdogsCatchDeadStreamsFast(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", nil)
	if e.network.IdleReadTimeout < 4*time.Minute || e.network.IdleReadTimeout > 5*time.Minute {
		t.Fatalf("idle=%s, want 4-5 minutes so a dead stream is retried while a quiet reasoning pause never trips", e.network.IdleReadTimeout)
	}
	if e.network.ResponseHeaderTimeout < 30*time.Second || e.network.ResponseHeaderTimeout > 90*time.Second {
		t.Fatalf("header=%s, want 30-90 seconds so a request that never reached a model fails fast", e.network.ResponseHeaderTimeout)
	}
	if !e.network.DisableOverallTimeout {
		t.Fatal("overall timeout must stay off: the turn budget, not the socket, owns the long-task clock")
	}
}

func TestChatWorkOutlivesTheTenMinuteLease(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	parent, cancel := context.WithTimeout(context.Background(), turnGenerationHardTime)
	defer cancel()
	err := e.withProviderLease(parent, provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
	}, secretlease.OperationChat, func(ctx context.Context, _ []byte) error {
		deadline, ok := ctx.Deadline()
		left := time.Duration(0)
		if ok {
			left = time.Until(deadline)
		}
		if !ok || left < 2*time.Hour {
			t.Fatalf("the turn is still cut at %s", left.Round(time.Second))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type timeoutFoldAdapter struct {
	calls    int
	quiet    bool
	level    string
	dropped  bool
	keptGoal bool
}

func (a *timeoutFoldAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *timeoutFoldAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *timeoutFoldAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	a.quiet = req.DisableReasoning
	a.level = req.ReasoningLevel
	a.dropped = true
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "旧自测记录-早") {
			a.dropped = false
		}
		if strings.Contains(m.Content, "长篇分析报告") {
			a.keptGoal = true
		}
	}
	if !a.dropped {
		return llmadapter.Response{}, &llmadapter.Error{Code: "TIMEOUT", Stage: llmadapter.StageHTTP, Message: "quiet"}
	}
	text := "星座报告已经写在画布上。"
	if err := emit(llmadapter.Delta{Text: text}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, nil
}

func TestTimeoutFoldsTheThreadAndFinishes(t *testing.T) {
	messages := make([]llmadapter.Message, 0, 17)
	for i := 0; i < 16; i++ {
		content := "近期记录"
		if i < 8 {
			content = "旧自测记录-早 selftest.mjs"
		}
		messages = append(messages, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: content})
	}
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleUser, Content: "帮我写一个长篇分析报告"})
	adapter := &timeoutFoldAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "todo.write"}, {Name: "user.ask"}, {Name: "canvas.present"}},
		Messages:       messages,
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.quiet || adapter.level != "max" || !adapter.dropped || !adapter.keptGoal {
		t.Fatalf("calls=%d quiet=%v level=%q dropped=%v goal=%v", adapter.calls, adapter.quiet, adapter.level, adapter.dropped, adapter.keptGoal)
	}
	if strings.Contains(joined, "无法执行") || !strings.Contains(joined, "星座报告已经写在画布上") {
		t.Fatalf("timeout ended the task: %q", joined)
	}
}

func TestSupplierRejectionCompactsEveryTaskAndFinishes(t *testing.T) {
	messages := make([]llmadapter.Message, 0, 17)
	for i := 0; i < 16; i++ {
		content := "近期记录"
		if i < 8 {
			content = "旧自测记录-早 selftest.mjs"
		}
		messages = append(messages, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: content})
	}
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleUser, Content: "把这份周报写完并落到文件"})
	adapter := &compactRejectAdapter{}
	deltas := runContinueStream(t, adapter, llmadapter.Request{
		Model:          "glm-5.3",
		ReasoningLevel: "max",
		Tools:          []llmadapter.ToolDefinition{{Name: "todo.write"}, {Name: "user.ask"}, {Name: "workspace.write"}},
		Messages:       messages,
	})
	joined := strings.Join(deltas, "")
	if adapter.calls < 2 || adapter.quiet || adapter.level != "max" || !adapter.keptPlan || !adapter.keptAsk || !adapter.droppedOld || !adapter.keptGoal {
		t.Fatalf("calls=%d quiet=%v level=%q plan=%v ask=%v dropped=%v goal=%v", adapter.calls, adapter.quiet, adapter.level, adapter.keptPlan, adapter.keptAsk, adapter.droppedOld, adapter.keptGoal)
	}
	if strings.Contains(joined, "无法执行") || strings.Contains(joined, "纯对话模式") || !strings.Contains(joined, "周报已经写完") {
		t.Fatalf("task did not finish after the supplier rejection: %q", joined)
	}
}
