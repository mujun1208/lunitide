// P4 companion-capability coverage: the model-callable skill.invoke tool
// rides the governed skillapp pipeline. Full-access conversations
// auto-approve; other modes keep the risk gate and answer a plain error
// (never parking the stream in an approval flow the voice companion
// cannot answer).
package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/skillapp"
)

type skillInvokeAdapter struct{ turn int }

func (a *skillInvokeAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *skillInvokeAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *skillInvokeAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if a.turn == 0 {
		a.turn++
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{
			{ID: "call-skill", Name: "skill.invoke", Arguments: []byte(`{"skillId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","input":"帮我解析手册章节"}`)},
		}}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "skill done"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Usage: llmadapter.Usage{OutputTokens: 2, TotalTokens: 2}}, nil
}

type skillInvokeRecordingStub struct {
	skillCatalogStub
	seenMode     string
	seenInput    string
	seenApproved bool
	executed     bool
	requires     bool
}

func (s *skillInvokeRecordingStub) Invoke(_ context.Context, skillID, session, input, mode string) (skillapp.Invocation, error) {
	s.seenMode, s.seenInput = mode, input
	return skillapp.Invocation{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA0", SkillID: skillID, Risk: "medium", RequiresApproval: s.requires}, nil
}
func (s *skillInvokeRecordingStub) Execute(_ context.Context, invocationID, session string, approved bool) (skillapp.Execution, error) {
	s.executed = true
	s.seenApproved = approved
	return skillapp.Execution{InvocationID: invocationID, Output: "技能执行结果：工卡字段已抽取"}, nil
}

func runSkillInvokeChat(t *testing.T, stub *skillInvokeRecordingStub, mode executionMode) []bridge.Event {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.skills = stub
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return &skillInvokeAdapter{}, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-skill-invoke"
	e.streams[id] = state
	var events []bridge.Event
	terminal := make(chan struct{})
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "m"}, func(event bridge.Event) error {
		events = append(events, event)
		if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
			close(terminal)
		}
		return nil
	}, "01ARZ3NDEKTSV4RRFFQ69G5FAV", mode)
	select {
	case <-terminal:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for terminal event")
	}
	return events
}

func toolCompletedSummary(t *testing.T, events []bridge.Event) string {
	t.Helper()
	for _, ev := range events {
		if ev.Type == bridge.EventToolCompleted && ev.Tool != nil && ev.Tool.CallID == "call-skill" {
			return ev.Tool.Summary
		}
	}
	t.Fatalf("no tool_completed event for call-skill: %#v", events)
	return ""
}

func TestSkillInvokeToolRunsInFullAccess(t *testing.T) {
	stub := &skillInvokeRecordingStub{}
	events := runSkillInvokeChat(t, stub, executionModeFullAccess)
	summary := toolCompletedSummary(t, events)
	if !strings.Contains(summary, "技能执行结果：工卡字段已抽取") {
		t.Fatalf("tool summary = %q, want skill output", summary)
	}
	if stub.seenMode != string(executionModeFullAccess) || stub.seenInput != "帮我解析手册章节" {
		t.Fatalf("invoke args = (%q,%q)", stub.seenMode, stub.seenInput)
	}
	if !stub.executed || !stub.seenApproved {
		t.Fatalf("full-access must auto-approve: executed=%v approved=%v", stub.executed, stub.seenApproved)
	}
}

func TestSkillInvokeToolKeepsRiskGateOutsideFullAccess(t *testing.T) {
	stub := &skillInvokeRecordingStub{requires: true}
	events := runSkillInvokeChat(t, stub, executionModeApproval)
	summary := toolCompletedSummary(t, events)
	if !strings.Contains(summary, "requires user approval") {
		t.Fatalf("tool summary = %q, want approval-gate message", summary)
	}
	if stub.executed {
		t.Fatal("requires-approval skill must not execute in approval mode")
	}
	// The stream itself must complete normally (the turn is answerable,
	// not parked in an approval flow).
	sawCompleted := false
	for _, ev := range events {
		if ev.Type == bridge.EventCompleted {
			sawCompleted = true
		}
	}
	if !sawCompleted {
		t.Fatalf("stream must complete after the gated skill result: %#v", events)
	}
}

func TestSkillInvokeToolDefinitionsWithAndWithoutService(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	if defs := e.skillToolDefinitions(); defs != nil {
		t.Fatalf("no skill service must yield no definitions: %#v", defs)
	}
	e.skills = &skillInvokeRecordingStub{}
	defs := e.skillToolDefinitions()
	if len(defs) != 4 || defs[0].Name != "skill.invoke" || defs[1].Name != "skill.view" || defs[2].Name != "skill.create" || defs[3].Name != "skill.manage" {
		t.Fatalf("definitions = %#v, want skill.invoke, skill.view, skill.create, skill.manage", defs)
	}
}

func TestInvokeSkillToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokeSkillTool(context.Background(), executionModeApproval, session, []byte(`{"skillId":"missing","input":"整理手册"}`))
	if err == nil {
		t.Fatal("unready skill.invoke must fail")
	}
	op := store.last()
	if op.ToolName != "skill.invoke" || op.State == modelfit.OpSucceeded {
		t.Fatalf("skill.invoke must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokeExpertCreateToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokeExpertCreateTool(context.Background(), session, []byte(`{"name":"测试专家"}`))
	if err == nil {
		t.Fatal("unready expert.create must fail")
	}
	op := store.last()
	if op.ToolName != "expert.create" || op.State == modelfit.OpSucceeded {
		t.Fatalf("expert.create must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokeSkillCreateToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokeSkillCreateTool(context.Background(), session, []byte(`{"name":"draft","permissions":["read_only"],"manifestJson":"{}"}`))
	if err == nil {
		t.Fatal("unready skill.create must fail")
	}
	op := store.last()
	if op.ToolName != "skill.create" || op.State == modelfit.OpSucceeded {
		t.Fatalf("skill.create must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokeSkillManageToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokeSkillManageTool(context.Background(), session, []byte(`{"action":"patch"}`))
	if err == nil {
		t.Fatal("unready skill.manage must fail")
	}
	op := store.last()
	if op.ToolName != "skill.manage" || op.State == modelfit.OpSucceeded {
		t.Fatalf("skill.manage must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokeSkillTrialToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokeSkillTrialTool(context.Background(), executionModeApproval, session, []byte(`{"skillId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","input":"试用草稿"}`))
	if err == nil {
		t.Fatal("unready skill.try must fail")
	}
	op := store.last()
	if op.ToolName != "skill.try" || op.State == modelfit.OpSucceeded {
		t.Fatalf("skill.try must leave a receipt without inventing success: %+v", op)
	}
}

func TestInvokePluginCreateToolRecordsReceipt(t *testing.T) {
	e := NewEngine(nil, "test")
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	_, err := e.invokePluginCreateTool(context.Background(), session, []byte(`{"pluginId":"x","name":"x","kind":"mcp"}`))
	if err == nil {
		t.Fatal("plugin.create mcp kind must fail")
	}
	op := store.last()
	if op.ToolName != "plugin.create" || op.State == modelfit.OpSucceeded {
		t.Fatalf("plugin.create must leave a receipt without inventing success: %+v", op)
	}
}

func TestSkillViewReadsCatalogTemplate(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.skills = &skillInvokeRecordingStub{}
	out, err := e.invokeSkillViewTool(context.Background(), []byte(`{"skillId":"computer-control"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "computer.act") {
		t.Fatalf("skill.view body missing computer.act: %s", out.Output)
	}
	if strings.Contains(out.Output, "cc.window_list") {
		t.Fatal("skill.view must not tell the model to call cc.* tool names")
	}
}

func TestSkillReferencesFromManifest(t *testing.T) {
	got := skillReferencesFromManifest(`{"prompt":"body","references":["docs/api.md","  "]}`)
	if len(got) != 1 || got[0] != "docs/api.md" {
		t.Fatalf("refs = %#v", got)
	}
}
