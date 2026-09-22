package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestDesktopLadderEscalatesFailedPlay(t *testing.T) {
	t.Parallel()
	goal := "打开汽水音乐，随机播放一首歌曲"
	if !desktopLadderApplies(goal) {
		t.Fatal("play+open must use the method ladder")
	}
	if desktopLadderApplies("在记事本输入123") {
		t.Fatal("typed-field closeout must not enter the play ladder")
	}
	if desktopLadderApplies("打开记事本") {
		t.Fatal("open-only closeout must not enter the play ladder")
	}
	if desktopLadderApplies("彻底退出微信") {
		t.Fatal("quit-only closeout must not enter the play ladder")
	}
	if desktopLadderApplies("写周报") || desktopLadderApplies("打开Word写周报") || desktopLadderApplies("根据附件做一份PPT") {
		t.Fatal("office deliverables must not enter the desktop method ladder")
	}
	if desktopLadderApplies("打开桌面浏览器，搜索新闻，然后查询内容给我") ||
		desktopLadderApplies("打开桌面浏览器搜索今天的新闻，然后查询内容给我") {
		t.Fatal("browser news lookup must not escalate to observe or named click")
	}
	if desktopLadderShouldContinue("打开Word写周报", "ok:true\nopened Word", []string{"desktop.open"}, 0) {
		t.Fatal("opening Word for a weekly report must not escalate to observe")
	}
	weekly := bundledWorkflowInjection("写周报")
	if strings.Contains(weekly, "电脑操作只走 1-2-3") || strings.Contains(weekly, "失败立刻②observe") {
		t.Fatalf("weekly-report workflow must not inherit the desktop hand: %q", weekly)
	}
	news := bundledWorkflowInjection("打开桌面浏览器，搜索新闻，然后查询内容给我")
	if strings.Contains(news, "电脑操作只走 1-2-3") || strings.Contains(news, "失败立刻②observe") {
		t.Fatalf("browser news lookup must not inherit the desktop hand: %q", news)
	}
	newsReply := companionFinalResult(receiptMessages("desktop.browse", `{}`, "已向系统默认桌面浏览器发送打开请求：https://example.com"), "今天的新闻：测试展会开幕。", "打开桌面浏览器，搜索新闻，然后查询内容给我")
	if newsReply != "今天的新闻：测试展会开幕。" {
		t.Fatalf("news lookup must not append page-verify speech: %q", newsReply)
	}
	if strings.Contains(incompleteContinueNudgeText, "按 1-2-3") || strings.Contains(incompleteContinueNudgeText, "observe 按名字") {
		t.Fatal("office inspect continue must not tell the model to click the screen")
	}
	if desktopLadderWantsDedicated("点击当前窗口的确认按钮") {
		t.Fatal("click-only starts at observe, not dedicated tools")
	}
	if desktopLadderNext(nil, "点击当前窗口的确认按钮") != ladderStep2Observe {
		t.Fatal("click-only first step is observe")
	}
	if !desktopLadderDedicatedUnresolved(goal, "ok:false\nnot found", []string{"media.play"}) {
		t.Fatal("failed media.play must be unresolved")
	}
	if !desktopLadderShouldContinue(goal, "ok:false\nnot found", []string{"media.play"}, 0) {
		t.Fatal("failed media.play must continue to named observe")
	}
	if desktopLadderShouldContinue(goal, "ok:false\nCAPABILITY_NOT_READY: 请先在设置中启用电脑控制", []string{"media.play"}, 0) {
		t.Fatal("capability denial must stop the ladder")
	}
	if desktopLadderShouldContinue(goal, "clicked 播放", []string{"media.play", "computer.act"}, 0) {
		t.Fatal("named click is not another observe loop")
	}
	if desktopLadderShouldContinue(goal, `{"count":0,"frameId":"frm_1"}`, []string{"media.play", "computer.act"}, 0) {
		t.Fatal("empty tree must go to GUI, not another model step")
	}
	if !desktopLadderShouldContinue(goal, `{"count":3,"frameId":"frm_1"}`, []string{"media.play", "computer.act"}, 0) {
		t.Fatal("observe with names must allow one click")
	}
}

func TestDesktopLadderSettlesOnlyAfterNamedActOrSuccess(t *testing.T) {
	t.Parallel()
	goal := "放一首复古公路风"
	failed := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "p1", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "p1", Content: "ok:false\nnot found"},
	}
	if desktopLadderSettled(failed, goal) {
		t.Fatal("failed media.play alone must not settle")
	}
	if !desktopLadderWantsNamedObserve(goal, failed) {
		t.Fatal("failed media.play must request observe")
	}
	observed := append(append([]llmadapter.Message{}, failed...),
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "o1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"observe"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "o1", Content: `{"count":0,"frameId":"frm_1"}`},
	)
	if desktopLadderSettled(observed, goal) {
		t.Fatal("observe-only must not settle before a named click or GUI act")
	}
	if !desktopLadderWantsGUIAfterObserve(goal, observed, 1) {
		t.Fatal("empty tree after failed play must open GUI")
	}
	clicked := append(append([]llmadapter.Message{}, observed...),
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "c1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"click","name":"播放"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "clicked 播放"},
	)
	if !desktopLadderSettled(clicked, goal) {
		t.Fatal("named click exhausts the dedicated→named rungs")
	}
	if desktopLadderWantsNamedObserve(goal, clicked) {
		t.Fatal("observe already happened")
	}
}

func TestDesktopLadderKeepsVerifiedPlayClosed(t *testing.T) {
	t.Parallel()
	goal := "随机播放一首歌曲"
	verified := `verified playing in 汽水音乐` + "\n" + `{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`
	if desktopLadderShouldContinue(goal, verified, []string{"media.play"}, 0) {
		t.Fatal("verified play must not escalate")
	}
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "p1", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "p1", Content: verified},
	}
	if !desktopLadderSettled(messages, goal) {
		t.Fatal("verified play must settle")
	}
	if closeout := computerReceiptCloseout(messages, goal); !strings.Contains(closeout, "已经在播") && !strings.Contains(closeout, "播放") {
		t.Fatalf("verified closeout: %q", closeout)
	}
}

func TestDesktopLadderCloseoutWaitsForNextMethod(t *testing.T) {
	t.Parallel()
	goal := "打开汽水音乐随机播放一首歌曲"
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "p1", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "p1", Content: `started playing in 汽水音乐 (media key)` + "\n" + `{"l0":{"kind":"foreground","passed":false,"uncertain":true,"detail":"MEDIA_UNVERIFIED"}}`},
	}
	if got := computerReceiptCloseout(messages, goal); got != "已经让汽水音乐播放了。" {
		t.Fatalf("delivered media key must close the turn: %q", got)
	}
	if desktopLadderShouldContinue(goal, messages[2].Content, []string{"media.play"}, 0) {
		t.Fatal("delivered media key must not climb to computer.act")
	}
}

func TestPickTurnContinueKindUsesLadder(t *testing.T) {
	t.Parallel()
	if got := pickTurnContinueKind("这次未能确认开始播放。", "这次未能确认开始播放。", "ok:false\nnot found", []string{"media.play"}, true, true, true, false, 0, "打开汽水音乐，随机播放一首歌曲", true); got != "ladder" {
		t.Fatalf("got %q", got)
	}
	if got := pickTurnContinueKind("好，我来操作电脑。", "好，我来操作电脑。", `{"count":3,"frameId":"frm_1"}`, []string{"media.play", "computer.act"}, true, true, true, false, 0, "打开汽水音乐，随机播放一首歌曲", true); got != "ladder" {
		t.Fatalf("observe must keep the ladder, got %q", got)
	}
}

func TestDesktopExecutionInstructionNamesTheLadder(t *testing.T) {
	t.Parallel()
	inst := desktopExecutionInstruction()
	for _, need := range []string{"1-2-3", "每步一次", "成功即停"} {
		if !strings.Contains(inst, need) {
			t.Fatalf("shared contract missing %q", need)
		}
	}
	if strings.Contains(inst, "不为同一个目标换工具重复操作") {
		t.Fatal("old no-switch rule must not remain")
	}
	media := bundledWorkflowInjection("随便播一首歌")
	if !strings.Contains(media, "computer.act observe") {
		t.Fatalf("play workflow must name the observe fallback: %q", media)
	}
	if strings.Contains(media, "禁止改用网页或 computer.act") {
		t.Fatal("play workflow must not forbid computer.act after media.play")
	}
}

func TestDesktopLadderOpenDoesNotFinishFollowUp(t *testing.T) {
	t.Parallel()
	goal := "打开记事本然后输入123"
	if !desktopLadderApplies(goal) {
		t.Fatal("open-then-type must stay on the ladder")
	}
	opened := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "o1", Name: "desktop.open", Arguments: json.RawMessage(`{"name":"记事本"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "o1", Content: "ok:true\nopened 记事本"},
	}
	if desktopLadderSucceeded(opened, goal) {
		t.Fatal("desktop.open must not finish an open-then-act goal")
	}
	if desktopLadderNext(opened, goal) != ladderStep2Observe {
		t.Fatal("after open, continue to observe for the follow-up act")
	}
}

func TestDesktopLadderUnverifiedPlayObservePlayingSettles(t *testing.T) {
	t.Parallel()
	goal := "打开汽水音乐，随机播放一首歌曲"
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "p1", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "p1", Content: `started playing` + "\n" + `{"l0":{"kind":"foreground","passed":false,"uncertain":true,"detail":"MEDIA_UNVERIFIED"}}`},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "o1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"observe"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "o1", Content: `{"count":3,"frameId":"frm_1","nodes":[{"name":"暂停"}]}`},
	}
	if desktopLadderShouldContinue(goal, `{"count":3,"frameId":"frm_1","nodes":[{"name":"暂停"}]}`, []string{"media.play", "computer.act"}, 0) {
		t.Fatal("observe that already shows pause must not click play")
	}
	if !desktopLadderSucceeded(messages, goal) {
		t.Fatal("pause on the tree means the track is already playing")
	}
}

func TestDesktopLadderFailedSkillEscalates(t *testing.T) {
	t.Parallel()
	goal := "打开记事本点保存"
	if !desktopLadderDedicatedUnresolved(goal, "ok:false\nskill missing", []string{"skill.invoke"}) {
		t.Fatal("failed skill.invoke must escalate to named observe")
	}
	if !desktopLadderDedicatedUnresolved(goal, "ok:false\nmcp down", []string{"mcp.call"}) {
		t.Fatal("failed mcp.call must escalate to named observe")
	}
	if !desktopLadderDedicatedUnresolved(goal, "ok:false\nnot found", []string{"desktop.open"}) {
		t.Fatal("failed desktop.open must escalate to named observe")
	}
}

func TestDesktopLadderOnePassNoBacktrack(t *testing.T) {
	t.Parallel()
	goal := "打开汽水音乐，随机播放一首歌曲"
	failed := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "p1", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "p1", Content: "ok:false\nnot found"},
	}
	if desktopLadderAllowsDedicated(goal, failed) {
		t.Fatal("after step 1 fail, do not run dedicated tools again")
	}
	if desktopLadderNext(failed, goal) != ladderStep2Observe {
		t.Fatal("step 1 fail must go to observe")
	}
	if !desktopLadderQuiet(failed, goal) {
		t.Fatal("must not speak failure after step 1")
	}
	kept := desktopLadderKeepCalls([]llmadapter.ToolCall{
		{ID: "again", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)},
		{ID: "see", Name: "computer.act", Arguments: json.RawMessage(`{"action":"observe"}`)},
	}, failed, goal)
	if len(kept) != 1 || kept[0].Name != "computer.act" {
		t.Fatalf("must drop replay of step 1, keep observe: %+v", kept)
	}
	clickedFail := append(append([]llmadapter.Message{}, failed...),
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "o1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"observe"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "o1", Content: `{"count":3,"frameId":"frm_1"}`},
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "c1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"click","name":"播放"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "ok:false\nno named target"},
	)
	if desktopLadderSettled(clickedFail, goal) {
		t.Fatal("named click fail must not settle before GUI")
	}
	if desktopLadderNext(clickedFail, goal) != ladderStep3 {
		t.Fatal("named click fail must go to GUI once")
	}
	if desktopLadderWantsNamedObserve(goal, clickedFail) {
		t.Fatal("do not observe again")
	}
	if !strings.Contains(desktopLadderNudgeTextFor(ladderStep2Observe), "observe") {
		t.Fatal("step 2 observe nudge must name observe")
	}
	if !strings.Contains(desktopLadderNudgeTextFor(ladderStep2Click), "点一次") {
		t.Fatal("step 2 click nudge must ask for one named click")
	}
	if !strings.Contains(desktopLadderNudgeTextFor(ladderStep3), "第3步") {
		t.Fatal("step 3 nudge must be GUI only")
	}
}

func TestDesktopLadderStreamCCOffSpeaksSettings(t *testing.T) {
	adapter := &ladderSuccessAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return adapter, nil
	})
	e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, _ json.RawMessage) (toolruntime.Result, error) {
		if name == "media.play" {
			return toolruntime.Result{Output: "ok:false\nnot found"}, nil
		}
		return toolruntime.Result{Output: "ok:true"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, taskRoute: RouteR2, companion: true, lane: LaneContract{Lane: LaneL4, ContinueNudges: true, MaxMainToolSteps: maxToolLoopSteps}}
	e.streams["stream-ladder-cc-off"] = state
	var mu sync.Mutex
	var visible strings.Builder
	done := make(chan struct{})
	go func() {
		e.runStream(ctx, "stream-ladder-cc-off", state, provider.Provider{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
			BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
		}, llmadapter.Request{
			Model:            "m",
			DisableReasoning: true,
			Tools:            []llmadapter.ToolDefinition{{Name: "media.play"}},
			Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "打开汽水音乐，随机播放一首歌曲"}},
		}, func(event bridge.Event) error {
			mu.Lock()
			if event.Delta != nil {
				visible.WriteString(event.Delta.Text)
			}
			mu.Unlock()
			if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
				close(done)
			}
			return nil
		}, "", executionModeFullAccess)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
	mu.Lock()
	got := visible.String()
	mu.Unlock()
	if !strings.Contains(got, "电脑控制未启用") || !strings.Contains(got, "设置") {
		t.Fatalf("CC-off after dedicated fail must name settings: %q", got)
	}
}

func TestDesktopLadderCloseoutSuccessAfterNamedClick(t *testing.T) {
	t.Parallel()
	goal := "打开汽水音乐随机播放一首歌曲"
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: goal},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "p1", Name: "media.play", Arguments: json.RawMessage(`{"query":"random"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "p1", Content: "ok:false\nnot found"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "o1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"observe"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "o1", Content: `{"count":3,"frameId":"frm_1"}`},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "c1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"click","name":"播放"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "c1", Content: "clicked 播放"},
	}
	if !desktopLadderSucceeded(messages, goal) {
		t.Fatal("named click success must stop the ladder")
	}
	if desktopLadderNext(messages, goal) != ladderStepNone {
		t.Fatal("success must not continue to GUI")
	}
	if got := computerReceiptCloseout(messages, goal); got != "已经在播了。" {
		t.Fatalf("success closeout: %q", got)
	}
}

type ladderStreamAdapter struct {
	mu    sync.Mutex
	round int
}

func (a *ladderStreamAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}
func (a *ladderStreamAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (a *ladderStreamAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.mu.Lock()
	a.round++
	round := a.round
	a.mu.Unlock()
	if !usedAnyTool(toolNamesFromMessages(req.Messages), "media.play") {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c-play", Name: "media.play", Arguments: json.RawMessage(`{"query":"random","target":"foreground"}`)},
		}}}, nil
	}
	if !turnAttemptedAction(req.Messages, "observe") && round < 8 {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: ""}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "控件树为空，已改用屏幕读号继续。"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: "控件树为空，已改用屏幕读号继续。"}}, nil
}

func toolNamesFromMessages(messages []llmadapter.Message) []string {
	names := make([]string, 0, 8)
	for _, receipt := range currentTurnReceipts(messages) {
		names = append(names, receipt.Name)
	}
	return names
}

func TestDesktopLadderStreamFailedPlayObservesThenGUI(t *testing.T) {
	for _, companion := range []bool{true, false} {
		t.Run(map[bool]string{true: "voice", false: "typed"}[companion], func(t *testing.T) {
			adapter := &ladderStreamAdapter{}
			e := NewEngineWithGateway(nil, "test", streamTestLease{})
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
				return adapter, nil
			})
			var play, observe int
			e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, args json.RawMessage) (toolruntime.Result, error) {
				switch name {
				case "media.play":
					play++
					return toolruntime.Result{Output: "ok:false\nnot found"}, nil
				case "computer.act":
					if computerActIsObserve(name, args) {
						observe++
						return toolruntime.Result{Output: `{"count":0,"frameId":"frm_1"}`}, nil
					}
					return toolruntime.Result{Output: "ok:false\nno named target"}, nil
				default:
					return toolruntime.Result{Output: "ok:true"}, nil
				}
			}
			var hookMu sync.Mutex
			hookCalls := 0
			e.guiFallbackHook = func(context.Context, executionMode, string, string, string, *streamState, []llmadapter.Image, bool, bool, bool) (toolruntime.Result, json.RawMessage, bool) {
				hookMu.Lock()
				hookCalls++
				hookMu.Unlock()
				return toolruntime.Result{Output: "clicked play via GUI"}, json.RawMessage(`{"action":"click","id":"B1"}`), true
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			state := &streamState{cancel: cancel, state: streamRunning, taskRoute: RouteR2, companion: companion, lane: LaneContract{Lane: LaneL4, ContinueNudges: true, MaxMainToolSteps: maxToolLoopSteps}}
			id := "stream-ladder"
			if companion {
				id += "-voice"
			}
			e.streams[id] = state
			var mu sync.Mutex
			var events []bridge.Event
			var visible strings.Builder
			done := make(chan struct{})
			goal := "打开汽水音乐，随机播放一首歌曲"
			go func() {
				e.runStream(ctx, id, state, provider.Provider{
					ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
					BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
				}, llmadapter.Request{
					Model:            "m",
					DisableReasoning: true,
					Tools:            []llmadapter.ToolDefinition{{Name: "media.play"}, {Name: "desktop.open"}, {Name: "computer.act"}},
					Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}},
				}, func(event bridge.Event) error {
					mu.Lock()
					events = append(events, event)
					if event.Delta != nil {
						visible.WriteString(event.Delta.Text)
					}
					mu.Unlock()
					if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
						close(done)
					}
					return nil
				}, "", executionModeFullAccess)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out")
			}
			if play != 1 {
				t.Fatalf("media.play calls=%d want 1 (must switch methods)", play)
			}
			if observe != 1 {
				t.Fatalf("named observe calls=%d want 1", observe)
			}
			hookMu.Lock()
			calls := hookCalls
			hookMu.Unlock()
			if calls != 1 {
				t.Fatalf("GUI hook calls=%d want 1 after empty tree", calls)
			}
			var sawGUI bool
			for _, ev := range events {
				if ev.Type == bridge.EventToolCompleted && ev.Tool != nil && strings.HasPrefix(ev.Tool.CallID, "gui-") {
					sawGUI = true
				}
			}
			if !sawGUI {
				t.Fatal("GUI writeback missing")
			}
			mu.Lock()
			got := visible.String()
			mu.Unlock()
			if strings.Contains(got, "未能确认") || strings.Contains(got, "播放失败") {
				t.Fatalf("must not speak failure before the last rung: %q", got)
			}
			if !strings.Contains(got, "已经在播") {
				t.Fatalf("success after last rung: %q", got)
			}
		})
	}
}

type ladderSuccessAdapter struct {
	mu    sync.Mutex
	round int
}

func (a *ladderSuccessAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}
func (a *ladderSuccessAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (a *ladderSuccessAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.mu.Lock()
	a.round++
	round := a.round
	a.mu.Unlock()
	if !usedAnyTool(toolNamesFromMessages(req.Messages), "media.play") {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c-play", Name: "media.play", Arguments: json.RawMessage(`{"query":"random","target":"foreground"}`)},
		}}}, nil
	}
	if !turnAttemptedAction(req.Messages, "observe") {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: ""}}, nil
	}
	if !desktopLadderNamedMutationAttempted(req.Messages) && round < 8 {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "c-click", Name: "computer.act", Arguments: json.RawMessage(`{"action":"click","name":"播放"}`)},
		}}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "这次未能确认开始播放。"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: "这次未能确认开始播放。"}}, nil
}

func TestDesktopLadderStreamNamedClickSuccessStops(t *testing.T) {
	adapter := &ladderSuccessAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return adapter, nil
	})
	var play, observe, click int
	e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, args json.RawMessage) (toolruntime.Result, error) {
		switch name {
		case "media.play":
			play++
			return toolruntime.Result{Output: "ok:false\nnot found"}, nil
		case "computer.act":
			if computerActIsObserve(name, args) {
				observe++
				return toolruntime.Result{Output: `{"count":3,"frameId":"frm_1"}`}, nil
			}
			click++
			return toolruntime.Result{Output: "clicked 播放"}, nil
		default:
			return toolruntime.Result{Output: "ok:true"}, nil
		}
	}
	hookCalls := 0
	e.guiFallbackHook = func(context.Context, executionMode, string, string, string, *streamState, []llmadapter.Image, bool, bool, bool) (toolruntime.Result, json.RawMessage, bool) {
		hookCalls++
		return toolruntime.Result{}, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, taskRoute: RouteR2, companion: true, lane: LaneContract{Lane: LaneL4, ContinueNudges: true, MaxMainToolSteps: maxToolLoopSteps}}
	e.streams["stream-ladder-success"] = state
	var mu sync.Mutex
	var visible strings.Builder
	done := make(chan struct{})
	goal := "打开汽水音乐，随机播放一首歌曲"
	go func() {
		e.runStream(ctx, "stream-ladder-success", state, provider.Provider{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
			BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
		}, llmadapter.Request{
			Model:            "m",
			DisableReasoning: true,
			Tools:            []llmadapter.ToolDefinition{{Name: "media.play"}, {Name: "computer.act"}},
			Messages:         []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}},
		}, func(event bridge.Event) error {
			mu.Lock()
			if event.Delta != nil {
				visible.WriteString(event.Delta.Text)
			}
			mu.Unlock()
			if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
				close(done)
			}
			return nil
		}, "", executionModeFullAccess)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
	if play != 1 || observe != 1 || click != 1 {
		t.Fatalf("play=%d observe=%d click=%d want 1,1,1", play, observe, click)
	}
	if hookCalls != 0 {
		t.Fatalf("GUI must not run after named click success, calls=%d", hookCalls)
	}
	mu.Lock()
	got := visible.String()
	mu.Unlock()
	if strings.Contains(got, "未能确认") || strings.Contains(got, "播放失败") {
		t.Fatalf("success must replace mid-ladder failure talk: %q", got)
	}
	if !strings.Contains(got, "已经在播") {
		t.Fatalf("named click success must report success: %q", got)
	}
}
