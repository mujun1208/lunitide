package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func draftTrialFixture(t *testing.T, body string) (*Engine, *storage.Store, skill.Skill) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "trial.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := skillapp.New(store, store)
	svc.SetInvocationStore(store)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = svc
	raw, _ := json.Marshal(map[string]any{"prompt": body, "triggers": []string{"试用草稿"}})
	sk, err := svc.Create(ctx, skill.Skill{Name: "trial-fixture", DisplayName: "本次试用草稿", Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: string(raw)})
	if err != nil {
		t.Fatal(err)
	}
	return e, store, sk
}

func TestDraftTrialProposalSurvivesRestartWithoutPublishingOrSessionLeak(t *testing.T) {
	e, store, sk := draftTrialFixture(t, "首段约束\n最后一段必须核实结果。")
	ctx := context.Background()
	svc := e.skills.(*skillapp.Service)
	if _, err := svc.Invoke(ctx, sk.ID, chatAttachmentSessionID, "试用", "full-access"); !errors.Is(err, skillapp.ErrSkillNotPublished) {
		t.Fatalf("normal invoke admitted draft: %v", err)
	}
	if _, err := svc.Invoke(ctx, sk.ID, chatAttachmentSessionID, "试用", "trial:full-access"); !errors.Is(err, skillapp.ErrExecutionForbidden) {
		t.Fatalf("forged trial mode admitted: %v", err)
	}
	inv, err := svc.InvokeTrial(ctx, sk.ID, chatAttachmentSessionID, "试用", "auto-edit")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetSkillInvocation(ctx, inv.ID)
	if err != nil || stored == nil || stored.Mode != "trial:auto-edit" {
		t.Fatalf("missing durable trial: %+v %v", stored, err)
	}
	restarted := skillapp.New(store, store)
	restarted.SetInvocationStore(store)
	if _, err = restarted.Execute(ctx, inv.ID, "another-session", false); !errors.Is(err, skillapp.ErrInvocationNotFound) {
		t.Fatalf("cross-session trial: %v", err)
	}
	out, err := restarted.Execute(ctx, inv.ID, chatAttachmentSessionID, false)
	if err != nil || !strings.Contains(out.Output, "最后一段必须核实结果") || !strings.Contains(out.Output, "未发布") {
		t.Fatalf("trial output %+v %v", out, err)
	}
	if _, err = svc.Execute(ctx, inv.ID, chatAttachmentSessionID, false); !errors.Is(err, skillapp.ErrInvocationConsumed) {
		t.Fatalf("old process replayed consumed proposal: %v", err)
	}
	current, _ := svc.Get(ctx, sk.ID)
	if current.Status != skill.SkillStatusDraft || current.Rev != sk.Rev {
		t.Fatalf("trial changed lifecycle: %+v", current)
	}
	matched, err := svc.Match(ctx, "trial-fixture")
	if err != nil || len(matched) != 0 {
		t.Fatalf("draft entered automatic matching: %+v %v", matched, err)
	}
}

func TestDraftTrialKeepsApprovalPlanVersionAndStatusGuards(t *testing.T) {
	for _, change := range []string{"approval", "plan", "content", "publish", "disable", "concurrent"} {
		t.Run(change, func(t *testing.T) {
			e, store, sk := draftTrialFixture(t, "只依据材料输出结果")
			ctx := context.Background()
			svc := e.skills.(*skillapp.Service)
			mode := "full-access"
			if change == "approval" || change == "plan" {
				mode = change
			}
			inv, err := svc.InvokeTrial(ctx, sk.ID, chatAttachmentSessionID, "测试", mode)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "approval":
				if _, err = svc.Execute(ctx, inv.ID, chatAttachmentSessionID, false); !errors.Is(err, skillapp.ErrApprovalRequired) {
					t.Fatal(err)
				}
				if _, err = svc.Execute(ctx, inv.ID, chatAttachmentSessionID, true); err != nil {
					t.Fatal(err)
				}
				return
			case "plan":
				if _, err = svc.Execute(ctx, inv.ID, chatAttachmentSessionID, true); !errors.Is(err, skillapp.ErrExecutionForbidden) {
					t.Fatal(err)
				}
				return
			case "content":
				manifest := `{"prompt":"修订后的全新约束"}`
				_, err = svc.UpdateFields(ctx, sk.ID, nil, nil, nil, &manifest, nil, nil, sk.Rev)
			case "publish":
				err = svc.Publish(ctx, sk.ID)
			case "disable":
				err = svc.Disable(ctx, sk.ID)
			case "concurrent":
				other := skillapp.New(store, store)
				other.SetInvocationStore(store)
				results := make(chan error, 2)
				var wg sync.WaitGroup
				for _, instance := range []*skillapp.Service{svc, other} {
					wg.Add(1)
					go func(s *skillapp.Service) {
						defer wg.Done()
						_, e := s.Execute(ctx, inv.ID, chatAttachmentSessionID, true)
						results <- e
					}(instance)
				}
				wg.Wait()
				close(results)
				success, consumed := 0, 0
				for err := range results {
					if err == nil {
						success++
					} else if errors.Is(err, skillapp.ErrInvocationConsumed) {
						consumed++
					} else {
						t.Fatal(err)
					}
				}
				if success != 1 || consumed != 1 {
					t.Fatalf("CAS winners=%d consumed=%d", success, consumed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = svc.Execute(ctx, inv.ID, chatAttachmentSessionID, true); !errors.Is(err, skillapp.ErrInvocationChanged) {
				t.Fatalf("changed draft proposal executed: %v", err)
			}
		})
	}
}

func TestSkillTrialSelectionIsExplicitAndLimitedToThisTurn(t *testing.T) {
	e, _, sk := draftTrialFixture(t, "完整约束")
	ctx := context.Background()
	instruction, err := e.prepareSkillTrials(ctx, chatAttachmentSessionID, []string{sk.ID}, false)
	if err != nil || !strings.Contains(instruction, "skill.try") || !strings.Contains(instruction, sk.ID) {
		t.Fatalf("trial instruction %s %v", instruction, err)
	}
	args, _ := json.Marshal(map[string]string{"skillId": sk.ID, "input": "测试"})
	for _, denied := range []context.Context{ctx, withSkillTrials(ctx, "other-session", []string{sk.ID}), withSkillTrials(ctx, chatAttachmentSessionID, []string{chatAttachmentProviderID})} {
		if _, err = e.invokeSkillTrialTool(denied, executionModeFullAccess, chatAttachmentSessionID, args); err == nil {
			t.Fatal("unselected draft executed")
		}
	}
	allowed := withSkillTrials(ctx, chatAttachmentSessionID, []string{sk.ID})
	if _, err = e.invokeSkillTrialTool(allowed, executionModeFullAccess, chatAttachmentSessionID, args); err != nil {
		t.Fatal(err)
	}
	if _, err = e.invokeSkillTrialTool(ctx, executionModeFullAccess, chatAttachmentSessionID, args); err == nil {
		t.Fatal("trial grant leaked to next turn")
	}
	for _, tc := range []struct {
		ids     []string
		session string
		voice   bool
	}{
		{[]string{sk.ID, sk.ID}, chatAttachmentSessionID, false},
		{[]string{"bogus"}, chatAttachmentSessionID, false},
		{[]string{chatAttachmentProviderID}, chatAttachmentSessionID, false},
		{[]string{sk.ID}, "", false},
		{[]string{sk.ID}, chatAttachmentSessionID, true},
		{make([]string, 9), chatAttachmentSessionID, false},
	} {
		if _, err = e.prepareSkillTrials(ctx, tc.session, tc.ids, tc.voice); err == nil {
			t.Fatalf("invalid trial selection accepted: %+v", tc)
		}
	}
	if err = e.skills.Publish(ctx, sk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.prepareSkillTrials(ctx, chatAttachmentSessionID, []string{sk.ID}, false); err == nil {
		t.Fatal("published skill remained a draft trial")
	}
}

type draftTrialAdapter struct {
	skillID   string
	requests  []llmadapter.Request
	repeat    bool
	toolError bool
}

func (*draftTrialAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("unused")
}
func (*draftTrialAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("unused")
}
func (a *draftTrialAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.requests = append(a.requests, req)
	if a.toolError {
		return llmadapter.Response{}, &llmadapter.Error{HTTPStatus: 400, Message: "tools unsupported"}
	}
	if len(a.requests) == 1 || a.repeat && len(a.requests) == 2 {
		args, _ := json.Marshal(map[string]string{"skillId": a.skillID, "input": "按照当前草稿试用并核查输出"})
		id := "try-first"
		if len(a.requests) == 2 {
			id = "try-repeat"
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: id, Name: "skill.try", Arguments: args}}}}, nil
	}
	return llmadapter.Response{}, emit(llmadapter.Delta{Text: "已按草稿约定给出本次试用结果；技能仍是草稿。"})
}

func TestChatStartDraftTrialLoadsCompleteBodyAndKeepsDraft(t *testing.T) {
	body := strings.Repeat("核实本轮输入，不编造材料。", 700) + "唯一尾部约束：反馈缺少的材料。"
	e, _, sk := draftTrialFixture(t, body)
	adapter := &draftTrialAdapter{skillID: sk.ID, repeat: true}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	raw, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "sessionId": chatAttachmentSessionID, "executionMode": "full-access", "trialSkillIds": []string{sk.ID}, "messages": []map[string]string{{"role": "user", "content": "[引用技能 本次试用草稿|" + sk.ID + "]\n按草稿给出试用结果"}}})
	done := make(chan bridge.Event, 1)
	res := e.HandleStreaming(context.Background(), validRequest("chat.start", string(raw)), func(ev bridge.Event) error {
		if ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed {
			select {
			case done <- ev:
			default:
			}
		}
		return nil
	})
	if !res.OK {
		t.Fatalf("chat.start rejected trial: %+v", res.Error)
	}
	select {
	case ev := <-done:
		if ev.Type == bridge.EventFailed {
			t.Fatalf("trial failed: %+v", ev.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("trial never completed")
	}
	if len(adapter.requests) != 3 {
		t.Fatalf("trial calls=%d", len(adapter.requests))
	}
	initial := adapter.requests[0]
	if !strings.Contains(initial.Messages[0].Content, "用户本轮显式选择的草稿试用") || len(initial.Tools) == 0 || initial.Tools[len(initial.Tools)-1].Name != "skill.try" {
		t.Fatal("trial tool or instructions lost in routing")
	}
	full, replay := 0, false
	for _, m := range adapter.requests[2].Messages {
		if m.Role == llmadapter.RoleTool && strings.Contains(m.Content, body) {
			full++
		}
		if m.ToolCallID == "try-repeat" {
			replay = strings.Contains(m.Content, "不是技能全文") && strings.Contains(m.Content, "skill.try") && strings.Contains(m.Content, "try-first")
		}
	}
	if full != 1 || !replay {
		t.Fatalf("full body=%d replay=%t", full, replay)
	}
	current, _ := e.skills.Get(context.Background(), sk.ID)
	if current.Status != skill.SkillStatusDraft {
		t.Fatal("trial published skill")
	}
}

func TestDraftTrialBudgetOrToolRejectionDoesNotDegradeToPartialExecution(t *testing.T) {
	for _, toolError := range []bool{false, true} {
		e, _, sk := draftTrialFixture(t, strings.Repeat("不得省略的约束", 1700))
		adapter := &draftTrialAdapter{skillID: sk.ID, toolError: toolError}
		e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
		ctx, cancel := context.WithCancel(withSkillTrials(context.Background(), chatAttachmentSessionID, []string{sk.ID}))
		state := &streamState{cancel: cancel, state: streamRunning}
		e.streams["trial-fail"] = state
		var events []bridge.Event
		e.runStream(ctx, "trial-fail", state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref", Models: []provider.Model{{ModelID: "model", ContextWindow: 4096}}}, llmadapter.Request{Model: "model", MaxTokens: 1024, Tools: []llmadapter.ToolDefinition{skillTrialToolDefinition()}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "试用草稿"}}}, func(ev bridge.Event) error { events = append(events, ev); return nil }, chatAttachmentSessionID, executionModeFullAccess)
		cancel()
		if len(adapter.requests) != 1 {
			t.Fatalf("degraded to unskilled model continuation: %d", len(adapter.requests))
		}
		failed := false
		for _, ev := range events {
			if ev.Type == bridge.EventFailed {
				failed = true
				if !toolError && ev.Error.Code != "SKILL_CONTEXT_BUDGET_EXCEEDED" {
					t.Fatalf("wrong budget result: %+v", ev.Error)
				}
			}
		}
		if !failed {
			t.Fatal("trial failure reported as completed")
		}
	}
}
