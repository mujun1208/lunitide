package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestSkillAuthoringDoesNotGenerateItsSubjectAsAnOfficeDocument(t *testing.T) {
	for _, goal := range []string{"创建一个每周写周报的技能", "帮我封装一个生成 Excel 表格的技能", "create a weekly report skill", "优化小说写作技能", "[引用技能 skill-creator|01ARZ3NDEKTSV4RRFFQ69G5FAV]\n每周收集进展，整理 Word 周报和 PPT", "把刚才这段对话整理成一个可复用技能。\n周报摘录"} {
		if !looksLikeSkillAuthoringTask(goal) {
			t.Fatalf("not recognized: %s", goal)
		}
		req := llmadapter.Request{Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}}}
		turn := &chatTurnCheckpoint{Goal: goal, DocxActive: true, PptActive: true}
		if includeOfficeGenWorkflow(goal) || docxKindFromRequest(req, goal) != "" || pptTaskFromRequest(req, goal) || officeGenToolForGoal(goal) != "" || shouldAutoOfficeGen(turn, errors.New("provider failed")) {
			t.Fatalf("skill authoring entered document fallback: %s", goal)
		}
	}
	for goal, want := range map[string]string{"调用周报技能帮我生成本周周报": "docx.gen", "用 PPT 技能做一份介绍公司的演示文稿": "pptx.gen", "不要创建技能，直接生成周报": "docx.gen", "生成一份 Excel 表格": "excel.gen"} {
		if got := officeGenToolForGoal(goal); got != want {
			t.Fatalf("existing generation changed: %s got=%s want=%s", goal, got, want)
		}
	}
}

type weeklySkillCreateAdapter struct {
	calls    int
	requests []llmadapter.Request
}

func (*weeklySkillCreateAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("unused")
}
func (*weeklySkillCreateAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("unused")
}
func (a *weeklySkillCreateAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.requests = append(a.requests, req)
	a.calls++
	if a.calls == 1 {
		args, _ := json.Marshal(map[string]any{"name": "weekly-report-fixture", "displayName": "每周周报", "description": "根据提供的工作进展生成周报", "permissions": []string{"read_only"}, "manifestJson": `{"prompt":"根据用户提供的本周进展汇总完成、风险和下周计划。缺失信息标注待补充。","triggers":["每周周报"]}`})
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "create-weekly", Name: "skill.create", Arguments: args}}}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "每周周报技能已保存为草稿，可在技能中心查看。"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{}, nil
}

func TestWeeklySkillCreationPersistsActualDraftWithoutOfficeFallback(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "weekly.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.skills = skillapp.New(store, store)
	adapter := &weeklySkillCreateAdapter{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["weekly-skill"] = state
	goal := "[引用技能 skill-creator|01ARZ3NDEKTSV4RRFFQ69G5FAV]\n创建一个每周写周报的技能"
	var events []bridge.Event
	e.runStream(streamCtx, "weekly-skill", state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}}}, func(ev bridge.Event) error { events = append(events, ev); return nil }, "", executionModeFullAccess)
	created, err := e.skills.List(ctx, skill.SkillStatusDraft)
	if err != nil || len(created) != 1 || created[0].Name != "weekly-report-fixture" || !strings.Contains(created[0].ManifestJSON, "下周计划") {
		t.Fatalf("real skill missing: %+v %v", created, err)
	}
	if adapter.calls != 2 {
		t.Fatalf("unexpected document workflow continuations: %d", adapter.calls)
	}
	for _, req := range adapter.requests {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "报告流水线") || strings.Contains(m.Content, "没有可完整写入的正文") {
				t.Fatalf("wrong route: %s", m.Content)
			}
		}
	}
	for _, ev := range events {
		if ev.Type == bridge.EventFailed {
			t.Fatalf("failed: %+v", ev)
		}
	}
}
