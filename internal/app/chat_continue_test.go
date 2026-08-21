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
	"github.com/lunitide/lunitide/internal/gateway"
)

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
}

type continueAdapter struct {
	calls    int
	sawNudge bool
}

func (a *continueAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *continueAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *continueAdapter) Stream(_ context.Context, _ []byte, req gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.calls++
	for _, m := range req.Messages {
		if m.Role == gateway.RoleSystem && strings.Contains(m.Content, "继续执行用户的指令直到完成") {
			a.sawNudge = true
		}
	}
	switch a.calls {
	case 1:
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	case 2:
		text := "找到 59 个技能目录，请确认是否继续安装。"
		if err := emit(gateway.Delta{Text: text}); err != nil {
			return gateway.Response{}, err
		}
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, Content: text}}, nil
	default:
		text := "已全部安装完成。"
		if err := emit(gateway.Delta{Text: text}); err != nil {
			return gateway.Response{}, err
		}
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, Content: text}}, nil
	}
}

func runContinueStream(t *testing.T, adapter *continueAdapter, req gateway.Request) []string {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
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
	deltas := runContinueStream(t, adapter, gateway.Request{Model: "m"})
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

func TestRunStreamDoesNotNudgeCompanion(t *testing.T) {
	adapter := &continueAdapter{}
	_ = runContinueStream(t, adapter, gateway.Request{Model: "m", DisableReasoning: true})
	if adapter.sawNudge {
		t.Fatal("companion turns must not inject a continue nudge")
	}
	if adapter.calls != 2 {
		t.Fatalf("calls = %d, want 2 (tool then pause, no third turn)", adapter.calls)
	}
}

type finishAdapter struct{ calls int }

func (a *finishAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *finishAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *finishAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.calls++
	if a.calls == 1 {
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	}
	text := "已全部安装完成，共 3 个技能。"
	if err := emit(gateway.Delta{Text: text}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, Content: text}}, nil
}

func TestRunStreamDoesNotNudgeWhenTaskFinished(t *testing.T) {
	adapter := &finishAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-finished"
	e.streams[id] = state
	done := make(chan struct{})
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{Model: "m"}, func(event bridge.Event) error {
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

func (a *skillCreateAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *skillCreateAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *skillCreateAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if a.turn == 0 {
		a.turn++
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-create", Name: "skill.create", Arguments: []byte(`{"name":"folder-reader","displayName":"Folder Reader","description":"read folders","permissions":["read_only"],"entryPoint":"SKILL.md","manifestJson":"{\"prompt\":\"read\",\"triggers\":[\"读取\"]}"}`)},
		}}}, nil
	}
	if err := emit(gateway.Delta{Text: "技能已创建"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Usage: gateway.Usage{OutputTokens: 2, TotalTokens: 2}}, nil
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
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return &skillCreateAdapter{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-skill-create"
	e.streams[id] = state
	done := make(chan struct{})
	var summaries []string
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{Model: "m"}, func(event bridge.Event) error {
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
