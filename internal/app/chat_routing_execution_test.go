package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type routedExecutionAdapter struct {
	stream func(llmadapter.Request) (llmadapter.Response, error)
}

func (*routedExecutionAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("unexpected non-stream completion")
}
func (*routedExecutionAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("unexpected discovery")
}
func (a *routedExecutionAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	resp, err := a.stream(req)
	if err == nil && resp.Message.Content != "" {
		err = emit(llmadapter.Delta{Text: resp.Message.Content})
	}
	return resp, err
}

func runRoutedExecution(t *testing.T, e *Engine, goal string, adapter *routedExecutionAdapter) []bridge.Event {
	t.Helper()
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	events := make(chan bridge.Event, 256)
	payload, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "executionMode": "full-access", "messages": []map[string]string{{"role": "user", "content": goal}}})
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", string(payload)), func(ev bridge.Event) error { events <- ev; return nil })
	if !response.OK {
		t.Fatalf("start: %+v", response)
	}
	frames := collectFramedChatEvents(t, response, events)
	if frames[len(frames)-1].Type != bridge.EventCompleted {
		t.Fatalf("terminal: %+v", frames[len(frames)-1])
	}
	return frames
}

func routedRequestHasTool(req llmadapter.Request, name string) bool {
	for _, tool := range req.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func TestMusicTurnDoesNotStreamUnverifiedPlaybackClaims(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	adapter := &routedExecutionAdapter{stream: func(llmadapter.Request) (llmadapter.Response, error) {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "音乐已经帮你放起来啦。"}}, nil
	}}
	frames := runRoutedExecution(t, e, "帮我打开汽水音乐，随机播放一首歌曲", adapter)
	var spoken strings.Builder
	for _, frame := range frames {
		if frame.Delta != nil {
			spoken.WriteString(frame.Delta.Text)
		}
	}
	if strings.Contains(spoken.String(), "已经帮你放起来") {
		t.Fatalf("speculative playback escaped: %s", spoken.String())
	}
	if spoken.Len() == 0 {
		t.Fatal("music turn ended silently")
	}
}

func TestNamedPlaySkipsARejectedModel(t *testing.T) {
	for _, goal := range []string{"帮我播放一首生所爱", "播放一部周星驰的电影，九品芝麻官"} {
		t.Run(goal, func(t *testing.T) {
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			runtime, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { runtime.Close() })
			e.SetToolRuntime(runtime)
			modelCalls := 0
			var played string
			e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, args json.RawMessage) (toolruntime.Result, error) {
				if name != "media.play" {
					t.Fatalf("tool %s", name)
				}
				played = string(args)
				if strings.Contains(goal, "电影") {
					return toolruntime.Result{}, errors.New("找不到《九品芝麻官》，没有这部片子")
				}
				return toolruntime.Result{Output: "已交给媒体中心播放。\nMEDIA_CENTER\nurl: https://archive.org/download/example/shengsuoai.mp3\nkind: audio\ntitle: 生所爱\n"}, nil
			}
			adapter := &routedExecutionAdapter{stream: func(llmadapter.Request) (llmadapter.Response, error) {
				modelCalls++
				return llmadapter.Response{}, &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: "rejected"}
			}}
			frames := runRoutedExecution(t, e, goal, adapter)
			var spoken strings.Builder
			for _, frame := range frames {
				if frame.Delta != nil {
					spoken.WriteString(frame.Delta.Text)
				}
				if frame.Error != nil && strings.Contains(frame.Error.Message, "供应商拒绝了请求") {
					t.Fatalf("provider rejection reached the user: %+v", frame.Error)
				}
			}
			if modelCalls != 0 {
				t.Fatalf("model calls=%d speech=%s", modelCalls, spoken.String())
			}
			if strings.Contains(spoken.String(), "供应商拒绝了请求") || strings.Contains(played, "Night of the Living Dead") {
				t.Fatalf("speech=%s played=%s", spoken.String(), played)
			}
			if strings.Contains(goal, "电影") {
				if !strings.Contains(spoken.String(), "找不到") || !strings.Contains(spoken.String(), "没有这部片子") || strings.Contains(spoken.String(), "Night of the Living Dead") || strings.Contains(spoken.String(), "爱奇艺") || strings.Contains(spoken.String(), "网易云") || strings.Contains(spoken.String(), "优酷") || !strings.Contains(played, "九品芝麻官") && !strings.Contains(played, `\u4e5d\u54c1\u829d\u9ebb\u5b98`) || !strings.Contains(played, `"target":"center"`) {
					t.Fatalf("speech=%s played=%s", spoken.String(), played)
				}
				return
			}
			if !strings.Contains(spoken.String(), "已交给媒体中心播放") || !strings.Contains(spoken.String(), "生所爱") || strings.Contains(spoken.String(), "music.163.com") || !strings.Contains(played, `"target":"center"`) || !strings.Contains(played, "生所爱") {
				t.Fatalf("speech=%s played=%s", spoken.String(), played)
			}
		})
	}
}

func TestChatBoxDesktopActionsDoNotWaitOnARejectedModel(t *testing.T) {
	cases := []struct {
		goal   string
		speech string
		want   string
	}{
		{"在这个桌面这个豆包的输入对话框当中输入你好然后发送", "已在豆包的输入框写好并发送。", `"window":"豆包"`},
		{"打开桌面上的协议，在证件号码后面写204040，然后保存", documentSavedSpeech, `"save":true`},
		{"和微信的_穆_聊5分钟", "怎么这么晚还不睡", `"after":"_穆_"`},
	}
	for _, tc := range cases {
		t.Run(tc.goal, func(t *testing.T) {
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			runtime, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { runtime.Close() })
			e.SetToolRuntime(runtime)
			var called []string
			e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, args json.RawMessage) (toolruntime.Result, error) {
				called = append(called, name+":"+string(args))
				switch name {
				case "desktop.open":
					return toolruntime.Result{Output: "opened 协议"}, nil
				case "desktop.type":
					if strings.Contains(tc.goal, "微信") {
						return toolruntime.Result{Output: "opened chat \"_穆_\" and sent \"你好\"\nvisible:\n怎么这么晚还不睡"}, nil
					}
					if strings.Contains(tc.goal, "豆包") {
						return toolruntime.Result{Output: `typed "你好" and submitted in 豆包`}, nil
					}
					return toolruntime.Result{Output: `typed "204040" after "证件号码" and saved`}, nil
				default:
					t.Fatalf("tool %s", name)
					return toolruntime.Result{}, nil
				}
			}
			modelCalls := 0
			adapter := &routedExecutionAdapter{stream: func(llmadapter.Request) (llmadapter.Response, error) {
				modelCalls++
				return llmadapter.Response{}, &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: "rejected"}
			}}
			frames := runRoutedExecution(t, e, tc.goal, adapter)
			var spoken strings.Builder
			for _, frame := range frames {
				if frame.Delta != nil {
					spoken.WriteString(frame.Delta.Text)
				}
				if frame.Error != nil && strings.Contains(frame.Error.Message, "供应商拒绝了请求") {
					t.Fatalf("provider rejection reached the user: %+v", frame.Error)
				}
			}
			joined := strings.Join(called, "\n")
			if !strings.Contains(spoken.String(), tc.speech) || !strings.Contains(joined, tc.want) || strings.Contains(spoken.String(), "供应商拒绝了请求") {
				t.Fatalf("speech=%s called=%s model=%d", spoken.String(), joined, modelCalls)
			}
			if !strings.Contains(tc.goal, "微信") && modelCalls != 0 {
				t.Fatalf("model calls=%d speech=%s", modelCalls, spoken.String())
			}
		})
	}
}

func TestTypedVideoRequestExecutesPublicReaderAndReturnsActualEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, source, evidence string
		status                 int
		captions               bool
	}{
		{"public-captions", "mixed", "字幕中记录的唯一事实", 200, true},
		{"metadata-only", "page_meta", "没有公开字幕", 200, false},
		{"rate-limit", "empty", "http_429", 429, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			runtime, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { runtime.Close() })
			e.SetToolRuntime(runtime)
			fetchCalls, modelCalls := 0, 0
			runtime.SetWebFetcher(func(ctx context.Context, raw string) (networkpolicy.FetchResult, error) {
				fetchCalls++
				if _, ok := ctx.Deadline(); !ok {
					return networkpolicy.FetchResult{}, errors.New("missing deadline")
				}
				if strings.Contains(raw, "aisubtitle.hdslb.com") {
					return networkpolicy.FetchResult{FinalURL: raw, Status: 200, ContentType: "application/json", Body: []byte(`{"body":[{"content":"字幕中记录的唯一事实"}]}`)}, nil
				}
				body := `<meta property="og:title" content="隔离测试标题"><meta property="og:description" content="只有测试简介">`
				if tc.captions {
					body += `<script>window.__INITIAL_STATE__={"videoData":{"title":"隔离测试标题","subtitle":{"list":[{"subtitle_url":"https://aisubtitle.hdslb.com/test.json"}]}}};</script>`
				}
				return networkpolicy.FetchResult{FinalURL: raw, Status: tc.status, ContentType: "text/html", Body: []byte(body)}, nil
			})
			adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
				modelCalls++
				if modelCalls == 1 {
					if !routedRequestHasTool(req, "video.understand") || !strings.Contains(req.Messages[0].Content, "[本轮视频链接]") {
						return llmadapter.Response{}, errors.New("video route or task instruction missing")
					}
					if routedRequestHasTool(req, "browser.act") || routedRequestHasTool(req, "media.play") {
						return llmadapter.Response{}, errors.New("analysis routed to playback")
					}
					return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "public-video", Name: "video.understand", Arguments: json.RawMessage(`{"url":"https://www.bilibili.com/video/BVfixture"}`)}}}}, nil
				}
				for _, msg := range req.Messages {
					if msg.Role == llmadapter.RoleTool && msg.ToolCallID == "public-video" {
						if !strings.Contains(msg.Content, "source: "+tc.source) || !strings.Contains(msg.Content, tc.evidence) || !strings.Contains(msg.Content, "这不是逐帧看完视频") {
							return llmadapter.Response{}, fmt.Errorf("wrong evidence: %s", msg.Content)
						}
						return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "本次可读取内容的依据：" + tc.evidence + "。"}}, nil
					}
				}
				return llmadapter.Response{}, errors.New("video tool result never reached the model")
			}}
			frames := runRoutedExecution(t, e, "分析这个视频 https://www.bilibili.com/video/BVfixture", adapter)
			completed := false
			for _, frame := range frames {
				if frame.Type == bridge.EventToolCompleted && frame.Tool != nil && frame.Tool.CallID == "public-video" {
					completed = true
				}
				if frame.Type == bridge.EventApprovalRequired {
					t.Fatal("read-only analysis requested approval")
				}
			}
			wantFetches := 1
			if tc.captions {
				wantFetches = 2
			}
			if !completed || modelCalls != 2 || fetchCalls != wantFetches {
				t.Fatalf("completed=%v model=%d fetch=%d", completed, modelCalls, fetchCalls)
			}
		})
	}
}

func TestTypedAutomaticExpertEquipmentInvokesMatchingSkill(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	const goal = "请 Excel表格制作专家分析 Excel 表格的异常值，先用相应技能处理"
	const skillID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	stub := &skillInvokeRecordingStub{skillCatalogStub: skillCatalogStub{items: []skill.Skill{{ID: skillID, Name: "tpl-excel-analyst", DisplayName: "表格分析师", EntryPoint: "builtin://excel-analyst", Description: "分析 Excel 数据", Status: skill.SkillStatusPublished}}}}
	e.skills = stub
	adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
		for _, msg := range req.Messages {
			if msg.Role == llmadapter.RoleTool && msg.ToolCallID == "auto-skill" {
				if !strings.Contains(msg.Content, "技能执行结果") {
					return llmadapter.Response{}, errors.New("missing skill result")
				}
				return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "技能已完成表格检查，已识别重复字段。"}}, nil
			}
		}
		instruction := req.Messages[0].Content
		if !strings.Contains(instruction, "Excel表格制作专家") || !strings.Contains(instruction, "skillId="+skillID) || !routedRequestHasTool(req, "skill.invoke") {
			return llmadapter.Response{}, errors.New("automatic expert/skill routing missing")
		}
		args, _ := json.Marshal(map[string]string{"skillId": skillID, "input": goal})
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "auto-skill", Name: "skill.invoke", Arguments: args}}}}, nil
	}}
	runRoutedExecution(t, e, goal, adapter)
	if !stub.executed || stub.seenInput != goal || !stub.seenApproved {
		t.Fatalf("skill not actually executed: %+v", stub)
	}
}

func TestMountedExpertAndVisibleEquipmentUseSameResolver(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "equipment.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e := NewEngine(nil, "test")
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	e.SetM8ExpertService(svc)
	expertID := createScenarioExpert(t, e, ctx)
	if _, err = svc.ReplaceBoundSkills(ctx, expertID, []string{"web-researcher", "mcp:playwright"}); err != nil {
		t.Fatal(err)
	}
	e.sessionExperts = stubSessionExperts{ids: []string{expertID}}
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	const goal = "分析 Excel 表格的异常值"
	eq := e.turnEquipmentFor(ctx, session, goal, false)
	names, skills, missing := e.turnEquipInfo(ctx, session, goal)
	if !reflect.DeepEqual(eq.Names, []string{"Database Optimizer"}) || !reflect.DeepEqual(names, eq.Names) || !reflect.DeepEqual(skills, []string{"web-researcher"}) || !reflect.DeepEqual(missing, []string{"playwright"}) {
		t.Fatalf("mounted equipment diverged: eq=%+v names=%v skills=%v missing=%v", eq, names, skills, missing)
	}
	if got := e.composeExpertNames(ctx, session, goal); !reflect.DeepEqual(got, eq.Names) {
		t.Fatalf("compose=%v", got)
	}
	chip := "[引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAC] 分析 Excel 表格的异常值"
	if got := e.turnEquipmentFor(ctx, session, chip, false); !reflect.DeepEqual(got.Names, []string{"PPT专家"}) {
		t.Fatalf("explicit chip lost: %+v", got)
	}
	e.sessionExperts = stubSessionExperts{}
	if got := e.turnEquipmentFor(ctx, session, goal, false); len(got.Names) != 0 {
		t.Fatalf("ordinary chat must not fuzzy-route experts: %+v", got)
	}
}

func TestVideoInstructionPreservesExplicitBrowserRequest(t *testing.T) {
	if got := videoTaskInstruction("用浏览器打开 https://www.bilibili.com/video/BVfixture"); got != "" {
		t.Fatal(got)
	}
	if got := videoTaskInstruction("总结我的普通文档"); got != "" {
		t.Fatal(got)
	}
}

func TestTypedWeeklyReportAskOmitsSearchTools(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	var captured llmadapter.Request
	adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
		captured = req
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "好的，请补充本周完成和风险。"}}, nil
	}}
	_ = runRoutedExecution(t, e, "写周报", adapter)
	if routedRequestHasTool(captured, "web.search") {
		t.Fatalf("写周报 kept search: %#v", captured.Tools)
	}
	if !routedRequestHasTool(captured, "workspace.write") || !routedRequestHasTool(captured, "docx.gen") {
		t.Fatal("写周报 must keep write and docx.gen so the file can land")
	}
	sys := ""
	if len(captured.Messages) > 0 {
		sys = captured.Messages[0].Content
	}
	if !strings.Contains(sys, "问缺什么") && !strings.Contains(sys, "问完即停") {
		t.Fatalf("missing L2-ask clause:\n%s", sys)
	}
}

func TestTypedPolishOmitsDocxGen(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	var captured llmadapter.Request
	adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
		captured = req
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "今天的会议开得很顺利，节奏清楚。"}}, nil
	}}
	_ = runRoutedExecution(t, e, "把这段话润色得更顺：今天开会很顺利", adapter)
	if routedRequestHasTool(captured, "docx.gen") || routedRequestHasTool(captured, "web.search") {
		t.Fatalf("L1 polish kept scrape/gen: %#v", captured.Tools)
	}
}

func TestTypedCouncilInviteLeadsReplyT15(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	adapter := &routedExecutionAdapter{stream: func(llmadapter.Request) (llmadapter.Response, error) {
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "我已经综合两位专家的意见，结论如下。"}}, nil
	}}
	frames := runRoutedExecution(t, e, "请两位一起评这份稿", adapter)
	var spoken strings.Builder
	for _, frame := range frames {
		if frame.Delta != nil {
			spoken.WriteString(frame.Delta.Text)
		}
	}
	if !strings.HasPrefix(strings.TrimLeft(spoken.String(), " \n"), councilInviteSpeech()) {
		t.Fatalf("T15 first sentence = %q", spoken.String())
	}
}

func TestExpertMcpHintOnlyClaimsActuallyAllowedConnections(t *testing.T) {
	eq := turnEquipment{Names: []string{"PPT专家"}, McpIDs: []string{"fetch", "playwright"}}
	ready := []string{"fetch", "context7"}
	if got := connectedMcpForEquipment(eq, ready); !reflect.DeepEqual(got, []string{"fetch"}) {
		t.Fatalf("advertised unbound or disconnected MCP: %v", got)
	}
	eq.Companion = true
	if got := connectedMcpForEquipment(eq, ready); !reflect.DeepEqual(got, ready) {
		t.Fatalf("companion lost unrestricted ready tools: %v", got)
	}
}
