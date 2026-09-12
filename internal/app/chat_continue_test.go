package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
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

func TestPlaybackClaimsRequireThisTurnsMatchingToolReceipt(t *testing.T) {
	for _, out := range []string{"", "ok:true", "opened player and sent play", `opened https://music.example\n{"l0":{"passed":true,"uncertain":false}}`} {
		if !unverifiedMediaPlay("media.play", out, "音乐已经在播放") {
			t.Fatalf("unverified output accepted: %q", out)
		}
	}
	started := `started playing in 汽水音乐 (media key)` + "\n" + `{"l0":{"kind":"foreground","passed":true,"uncertain":false,"detail":"汽水音乐"}}`
	if unverifiedMediaPlay("media.play", started, "还没有确认开始播放") {
		t.Fatal("started playing with passed foreground l0 must close the play loop")
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
	if !shouldContinueIncompleteWork("好，我来播放。", "media.play started player", []string{"media.play"}, true, 0) {
		t.Fatal("unverified media.play must continue")
	}
	if shouldContinueIncompleteWork("好，我来播放。", "media.play started player", []string{"media.play"}, true, 1) {
		t.Fatal("unverified media.play continues only once")
	}
	if !shouldContinueIncompleteWork("正在播放周杰伦", "media.play started player", []string{"media.play"}, true, 0) {
		t.Fatal("assistant claim is not playback evidence")
	}
	if pickTurnContinueKind("这次没有完成。", "这次没有完成。", "ok:false\nnot found", []string{"media.play"}, true, true, true, false, 0, "放一首复古公路风", true) != "" {
		t.Fatal("failed media.play must not desktop-continue")
	}
	if pickTurnContinueKind("好，我再点一下。", "好，我再点一下。", "clicked 播放", []string{"media.play", "computer.act"}, true, true, true, false, 0, "打开汽水音乐随机播放一首歌曲", true) != "" {
		t.Fatal("playback-only turns must stop after media.play even if computer.act followed")
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
	long := "合肥今天的天气我手头没有实时数据，没法给你准确温度。你要是不急，我可以帮你查一下，稍等。"
	if got := pickTurnContinueKind("", long, "", nil, false, false, true, true, 0, "今天合肥的天气怎么样", true); got != "wait" {
		t.Fatalf("long wait promise must continue, got %q", got)
	}
	if got := pickTurnContinueKind("", "稍等。", "", nil, false, false, true, true, 0, "你好", false); got != "" {
		t.Fatal("idle wait without tools must not loop")
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

func runContinueStream(t *testing.T, adapter *continueAdapter, req llmadapter.Request) []string {
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
	for _, want := range []string{"不能再调用任何工具", "最终总结", "还有哪些没做完"} {
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
