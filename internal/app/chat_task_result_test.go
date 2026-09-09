package app

import (
	"context"
	"encoding/json"
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
	for _, goal := range []string{"今天上海虹桥到合肥南站的火车票有哪些？", "今天沪深指数怎么样"} {
		if !companionWantsTools(goal) || !looksLikeCurrentLookupTurn(goal) {
			t.Fatal("lookup lost tools", goal)
		}
		for _, name := range []string{"desktop.open", "media.play", "workspace.edit", "desktop.type"} {
			if guardCurrentTurnTool(goal, name) == nil {
				t.Fatal("old action allowed", goal, name)
			}
		}
		if guardCurrentTurnTool(goal, "web.search") != nil || guardCurrentTurnTool(goal, "browser.act") != nil {
			t.Fatal("lookup tool blocked")
		}
	}
	if guardCurrentTurnTool("打开图片，然后查车票", "desktop.open") != nil {
		t.Fatal("explicit combined task blocked")
	}
	instruction := currentTurnInstruction("查今天车票", time.Date(2026, 9, 8, 14, 0, 0, 0, time.FixedZone("CST", 8*3600)))
	if !strings.Contains(instruction, "2026-09-08") || !strings.Contains(instruction, "不是本轮待执行清单") || !strings.Contains(instruction, "只用一到三句") {
		t.Fatal(instruction)
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
