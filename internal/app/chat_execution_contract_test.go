package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestDesktopExecutionParityAcrossTypedAndVoice(t *testing.T) {
	for _, mode := range []string{"typed", "local", "cloud", "volc"} {
		for _, scenario := range []struct {
			name, goal, tool, args, output, answer, want string
			duplicate                                    bool
		}{
			{"open", "打开记事本", "desktop.open", `{"name":"记事本"}`, "opened notepad\n" + `{"l0":{"kind":"foreground","passed":true,"uncertain":false}}`, "打开失败。", "已打开目标", false},
			{"type", "在记事本的号码字段输入123", "desktop.type", `{"text":"123","after":"号码","window":"记事本"}`, `typed "123"` + "\n" + `{"l0":{"kind":"field","passed":true,"uncertain":false}}`, "这次写入失败。", "写入并核对", false},
			{"type-unverified", "在记事本输入123", "computer.act", `{"action":"type","text":"123","frameId":"fresh"}`, "typed 3 character(s); screen updated 100x100", "已经写好并确认了。", "未确认目标输入框", false},
			{"observe-only", "在记事本输入123", "computer.act", `{"action":"observe"}`, `{"nodes":[],"frameId":"fresh"}`, "已经写好并确认了。", "尚未取得文字写入", false},
			{"next-duplicate", "切换下一首歌曲", "media.play", `{"action":"next","app":"汽水音乐"}`, "verified next in player\n" + `{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`, "没能播放。", "已切换到下一首", true},
			{"type-duplicate", "在记事本的号码字段输入123", "desktop.type", `{"text":"123","after":"号码","window":"记事本"}`, `typed "123"` + "\n" + `{"l0":{"kind":"field","passed":true,"uncertain":false}}`, "写入失败。", "写入并核对", true},
			{"quit", "彻底退出微信", "desktop.quit", `{"name":"微信","force":true}`, "已彻底退出微信，已确认目标进程不再运行\n" + `{"l0":{"kind":"process","passed":true,"uncertain":false}}`, "退出失败。", "已彻底退出微信", false},
		} {
			t.Run(mode+"/"+scenario.name, func(t *testing.T) {
				var visible strings.Builder
				calls, executed, completed := 0, 0, 0
				adapter := &lookupLatencyAdapter{stream: func(req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
					calls++
					if calls == 1 || (scenario.duplicate && calls == 2) {
						if calls == 1 {
							if err := emit(llmadapter.Delta{Text: "操作已经完成了。"}); err != nil {
								return llmadapter.Response{}, err
							}
							if visible.Len() > 0 {
								t.Error("speculative success escaped before tool")
							}
						}
						return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: fmt.Sprint(calls), Name: scenario.tool, Arguments: json.RawMessage(scenario.args)}}}}, nil
					}
					if scenario.duplicate && lastNamedToolOutput(req.Messages, scenario.tool) != scenario.output {
						t.Error("duplicate guard lost original receipt")
					}
					if err := emit(llmadapter.Delta{Text: scenario.answer}); err != nil {
						return llmadapter.Response{}, err
					}
					return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: scenario.answer}}, nil
				}}
				req := llmadapter.Request{Model: "m", DisableReasoning: true, Tools: []llmadapter.ToolDefinition{{Name: scenario.tool}}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: scenario.goal}}}
				runCompanionLatencyStream(t, req, adapter, func(string) string { executed++; return scenario.output }, func(ev bridge.Event) error {
					if ev.Delta != nil {
						visible.WriteString(ev.Delta.Text)
					}
					if ev.Type == bridge.EventCompleted {
						completed++
					}
					if ev.Type == bridge.EventFailed {
						t.Errorf("unexpected failure: %+v", ev.Error)
					}
					return nil
				}, mode != "typed")
				if executed != 1 || completed != 1 || !strings.Contains(visible.String(), scenario.want) || strings.Contains(visible.String(), "操作已经完成了") {
					t.Fatalf("executed=%d completed=%d calls=%d visible=%s", executed, completed, calls, visible.String())
				}
			})
		}
	}
}

func TestDesktopMutationRetriesAreBoundedAcrossFreshFrames(t *testing.T) {
	for _, output := range []string{"ok:false target missing", `{"l0":{"kind":"target","passed":false,"uncertain":true}}`} {
		for _, companion := range []bool{false, true} {
			t.Run(fmt.Sprint(companion), func(t *testing.T) {
				calls, executed := 0, 0
				adapter := &lookupLatencyAdapter{stream: func(req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
					calls++
					if calls <= 4 {
						args, _ := json.Marshal(map[string]any{"action": "click", "name": "确认", "frameId": fmt.Sprint(calls)})
						return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: fmt.Sprint(calls), Name: "computer.act", Arguments: args}}}}, nil
					}
					const reply = "无法执行：没有找到目标按钮。"
					if err := emit(llmadapter.Delta{Text: reply}); err != nil {
						return llmadapter.Response{}, err
					}
					return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: reply}}, nil
				}}
				req := llmadapter.Request{Model: "m", DisableReasoning: true, Tools: []llmadapter.ToolDefinition{{Name: "computer.act"}}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "点击当前窗口的确认按钮"}}}
				runExecutionContractStream(t, req, adapter, companion, func(name string, args json.RawMessage) (toolruntime.Result, error) {
					if liveDesktopObservation(name, args) {
						return toolruntime.Result{Output: `{"nodes":[],"frameId":"fresh"}`}, nil
					}
					executed++
					return toolruntime.Result{Output: output}, nil
				}, func(bridge.Event) error { return nil })
				if executed != 2 {
					t.Fatalf("same mutation physically attempted %d times", executed)
				}
			})
		}
	}
}

func TestFallbackRescueRecognizesEquivalentActions(t *testing.T) {
	for _, tc := range []struct{ action, name, args string }{
		{"open", "desktop.browse", `{"query":"news"}`},
		{"open", "browser.act", `{"op":"navigate"}`},
		{"type", "computer.act", `{"action":"type","text":"123"}`},
		{"type", "cc.paste", `{"text":"123"}`},
		{"lookup", "web.fetch", `{"url":"https://example.com"}`},
		{"observe", "cc.observe_ui", `{}`},
	} {
		messages := receiptMessages(tc.name, tc.args, "ok:false already attempted")
		if !turnAttemptedAction(messages, tc.action) {
			t.Error(tc)
		}
		messages = append(messages, llmadapter.Message{Role: llmadapter.RoleUser, Content: "next task"})
		if turnAttemptedAction(messages, tc.action) {
			t.Error("old task suppressed new action", tc)
		}
	}
}

func TestBrowserObservationsRemainFreshAndMutationsRemainBounded(t *testing.T) {
	for _, args := range []string{`{"op":"snapshot"}`, `{"op":"read"}`, `{"op":"tabs"}`, `{"op":"tabs","tab":"list"}`, `{"op":"wait"}`} {
		if !liveDesktopObservation("browser.act", json.RawMessage(args)) || desktopMutation("browser.act", json.RawMessage(args)) {
			t.Error("fresh browser read treated as a mutation", args)
		}
	}
	for _, args := range []string{`{"op":"navigate"}`, `{"op":"type"}`, `{"op":"click"}`, `{"op":"tabs","tab":"close"}`} {
		if liveDesktopObservation("browser.act", json.RawMessage(args)) || !desktopMutation("browser.act", json.RawMessage(args)) {
			t.Error("browser mutation escaped retry accounting", args)
		}
	}
}

func TestBrowserSearchRefinementDoesNotRelaunchEntry(t *testing.T) {
	const goal = "打开桌面浏览器搜索今天的新闻，然后查询内容给我"
	const browse = "已向系统默认桌面浏览器发送打开请求：https://example.com/search?q=news"
	for _, companion := range []bool{false, true} {
		calls, opens := 0, 0
		adapter := &lookupLatencyAdapter{stream: func(req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
			calls++
			if calls <= 3 {
				name, args := "desktop.browse", `{"query":"news"}`
				switch calls {
				case 2:
					name, args = "web.search", `{"query":"news"}`
				case 3:
					args = `{"query":"refined news"}`
				}
				return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: fmt.Sprint(calls), Name: name, Arguments: json.RawMessage(args)}}}}, nil
			}
			if !strings.Contains(lastNamedToolOutput(req.Messages, "desktop.browse"), "没有重复启动") {
				t.Error("search refinement did not reuse launch receipt")
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "已查到新闻：测试事件。来源：测试站。"}}, nil
		}}
		req := llmadapter.Request{Model: "m", DisableReasoning: true, Tools: []llmadapter.ToolDefinition{{Name: "desktop.browse"}, {Name: "web.search"}}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}}}
		runExecutionContractStream(t, req, adapter, companion, func(name string, args json.RawMessage) (toolruntime.Result, error) {
			if name == "desktop.browse" {
				opens++
				return toolruntime.Result{Output: browse}, nil
			}
			return toolruntime.Result{Output: "test news evidence"}, nil
		}, func(bridge.Event) error { return nil })
		if opens != 1 {
			t.Fatalf("companion=%v physical browser opens=%d", companion, opens)
		}
	}
	messages := receiptMessages("desktop.browse", `{"query":"news"}`, browse)
	messages = append(messages,
		llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "lookup", Name: "web.search", Arguments: json.RawMessage(`{"query":"news"}`)}}},
		llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "lookup", Content: "news evidence"})
	for _, tc := range []struct{ goal, args string }{
		{goal, `{"url":"https://example.com/article"}`},
		{"打开浏览器分别搜索两个新闻并查询内容", `{"query":"other news"}`},
	} {
		if reuseBrowserSearchEntry(tc.goal, json.RawMessage(tc.args), messages) != "" {
			t.Error("explicit navigation incorrectly skipped", tc)
		}
	}
}

func TestCompoundResultsAreNotReplacedByOneOperation(t *testing.T) {
	media := receiptMessages("media.play", `{"action":"next"}`, "verified next shuffle=false\n"+`{"l0":{"kind":"media-session","passed":true,"uncertain":false}}`)
	const reply = "已切歌，查询结果也已找到。"
	if got := companionFinalResult(media, reply, "随机切换下一首歌曲，然后查询天气"); got != reply {
		t.Fatal("media receipt erased compound result", got)
	}
	browse := receiptMessages("desktop.browse", `{"query":"news"}`, "已向系统默认桌面浏览器发送打开请求")
	const compound = "已查到新闻，文件保存失败。"
	if got := companionFinalResult(browse, compound, "打开浏览器查新闻然后保存文件"); !strings.Contains(got, compound) || !strings.Contains(got, "页面尚未核验") {
		t.Fatal("browser receipt erased remaining result", got)
	}
}

func TestDesktopEvidenceSurvivesLongSummaryAndJSONBraces(t *testing.T) {
	const proof = `{"l0":{"kind":"field","passed":true,"uncertain":false,"detail":"value contains {braces} and quotes: \"ok\""}}`
	output := strings.Repeat("typed data ", toolSummaryMaxBytes) + "\n" + proof
	clipped := clipExecutionToolSummary("desktop.type", output)
	l0, ok := extractL0(clipped)
	if !ok || !l0.Passed || l0.Uncertain || l0.Kind != "field" || !strings.Contains(l0.Detail, "{braces}") || len(clipped) > toolSummaryMaxBytes {
		t.Fatalf("proof lost: %+v present=%v len=%d", l0, ok, len(clipped))
	}
}

func TestDesktopResultRejectsNoActionAndDoesNotSettleCompoundWork(t *testing.T) {
	if got := companionFinalResult(nil, "已经打开并写完。", "打开记事本输入123"); strings.Contains(got, "已经打开") || !strings.Contains(got, "没有取得") {
		t.Fatal(got)
	}
	for _, goal := range []string{"播放歌曲然后写入文档", "下一首歌曲，然后查询天气", "打开记事本输入123并发送给同事"} {
		if playbackOnlyGoal(goal) || typedFieldOnlyGoal(goal) {
			t.Error("compound goal settled as one step", goal)
		}
	}
}

func TestDesktopRecoveryUsesLatestSuccessInBothInputModes(t *testing.T) {
	for _, companion := range []bool{false, true} {
		t.Run(fmt.Sprint(companion), func(t *testing.T) {
			calls, writes := 0, 0
			var visible strings.Builder
			adapter := &lookupLatencyAdapter{stream: func(req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
				calls++
				if calls <= 2 {
					args, _ := json.Marshal(map[string]string{"text": "123", "after": "号码", "window": fmt.Sprintf("记事本%d", calls)})
					return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: fmt.Sprint(calls), Name: "desktop.type", Arguments: args}}}}, nil
				}
				const reply = "这次写入失败，没有完成。"
				if err := emit(llmadapter.Delta{Text: reply}); err != nil {
					return llmadapter.Response{}, err
				}
				return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: reply}}, nil
			}}
			req := llmadapter.Request{Model: "m", DisableReasoning: true, Tools: []llmadapter.ToolDefinition{{Name: "desktop.type"}}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "在记事本的号码字段输入123"}}}
			runExecutionContractStream(t, req, adapter, companion, func(name string, args json.RawMessage) (toolruntime.Result, error) {
				if liveDesktopObservation(name, args) {
					return toolruntime.Result{Output: `{"nodes":[],"frameId":"fresh"}`}, nil
				}
				writes++
				if writes == 1 {
					return toolruntime.Result{Output: "ok:false wrong window"}, nil
				}
				return toolruntime.Result{Output: "typed 123\n" + `{"l0":{"kind":"field","passed":true,"uncertain":false}}`}, nil
			}, func(ev bridge.Event) error {
				if ev.Delta != nil {
					visible.WriteString(ev.Delta.Text)
				}
				return nil
			})
			if writes != 2 || !strings.Contains(visible.String(), "写入并核对") || strings.Contains(visible.String(), "写入失败") {
				t.Fatalf("writes=%d reply=%s", writes, visible.String())
			}
		})
	}
}

func TestDesktopToolRoutesHaveTypedVoiceParity(t *testing.T) {
	defs := []llmadapter.ToolDefinition{{Name: "desktop.open"}, {Name: "desktop.browse"}, {Name: "desktop.quit"}, {Name: "desktop.type"}, {Name: "computer.act"}, {Name: "media.play"}, {Name: "weather.get"}, {Name: "web.search"}, {Name: "web.fetch"}}
	for _, goal := range []string{"打开桌面浏览器，搜索新闻", "打开浏览器查天气", "打开桌面文件然后查询天气", "在记事本输入123", "打开汽水音乐随机播放一首歌", "彻底退出微信"} {
		typed := assembleRoutedTools(defs, goal, false, true)
		voice := assembleRoutedTools(defs, goal, true, true)
		x, _ := json.Marshal(typed)
		y, _ := json.Marshal(voice)
		if string(x) != string(y) {
			t.Errorf("different tools for same goal %s: typed=%s voice=%s", goal, x, y)
		}
		if !computerExecutionTurn(goal) {
			t.Error("desktop contract missing", goal)
		}
	}
}

func TestDesktopCapabilityDeniedIsNotRetried(t *testing.T) {
	failed := map[string]int{}
	args := json.RawMessage(`{"text":"hi"}`)
	if desktopMutationRetryBlocked(failed, "desktop.type", args) {
		t.Fatal("first attempt must run")
	}
	recordDesktopMutationFailure(failed, "desktop.type", args, "CAPABILITY_NOT_READY: 请先在设置中启用电脑控制")
	if !desktopMutationRetryBlocked(failed, "desktop.type", args) {
		t.Fatal("missing computer control must not be retried")
	}
	locked := map[string]int{}
	recordDesktopMutationFailure(locked, "desktop.type", args, "锁屏中，已停止桌面输入，未向界面发送按键")
	if !desktopMutationRetryBlocked(locked, "desktop.type", args) {
		t.Fatal("lock screen must not be retried")
	}
	transient := map[string]int{}
	recordDesktopMutationFailure(transient, "desktop.type", args, "无法执行：目标未出现")
	if desktopMutationRetryBlocked(transient, "desktop.type", args) {
		t.Fatal("a single transient failure may retry once")
	}
	recordDesktopMutationFailure(transient, "desktop.type", args, "无法执行：目标未出现")
	if !desktopMutationRetryBlocked(transient, "desktop.type", args) {
		t.Fatal("two identical failures must stop repeating")
	}
}
