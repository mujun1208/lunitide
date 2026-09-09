package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

func customSkillFixture(t *testing.T, body string) (*Engine, skill.Skill) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "custom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.skills = skillapp.New(store, store)
	manifest, _ := json.Marshal(map[string]any{"prompt": body, "triggers": []string{"按技能复核"}})
	args, _ := json.Marshal(map[string]any{"name": "long-custom-fixture", "displayName": "自定义技能", "permissions": []string{"read_only"}, "manifestJson": string(manifest)})
	if _, err = e.invokeSkillCreateTool(ctx, args); err != nil {
		t.Fatal(err)
	}
	rows, err := e.skills.List(ctx, skill.SkillStatusDraft)
	if err != nil || len(rows) != 1 {
		t.Fatalf("create %+v %v", rows, err)
	}
	if rows[0].EntryPoint != "SKILL.md" {
		t.Fatalf("default entry point changed: %s", rows[0].EntryPoint)
	}
	if err = e.skills.Publish(ctx, rows[0].ID); err != nil {
		t.Fatal(err)
	}
	sk, err := e.skills.Get(ctx, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return e, *sk
}

func readSkillPage(t *testing.T, e *Engine, args map[string]any) skillReadPage {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, err := e.invokeSkillViewTool(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Output) > skillReadPageBytes || clipToolSummary(out.Output) != out.Output {
		t.Fatal("model summary clipped skill page")
	}
	var p skillReadPage
	if err = json.Unmarshal([]byte(out.Output), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLongSkillViewPagesPreserveAllConstraintsAndRejectSourceChanges(t *testing.T) {
	body := strings.Repeat("约束<&>\"\\逐条核对。", 1000) + "尾部强制约束：先核实来源再输出结论。"
	e, sk := customSkillFixture(t, body)
	var combined strings.Builder
	offset := 0
	digest := ""
	pages := 0
	for {
		p := readSkillPage(t, e, map[string]any{"skillId": sk.ID, "offset": offset, "expectedDigest": digest})
		pages++
		if p.SkillID != sk.ID || p.Version != sk.Version || p.Revision != sk.Rev || p.Offset != offset {
			t.Fatalf("wrong source metadata: %+v", p)
		}
		if digest != "" && digest != p.Digest {
			t.Fatal("source changed between pages")
		}
		digest = p.Digest
		combined.WriteString(p.Text)
		if !p.HasMore {
			break
		}
		if p.NextOffset <= offset || pages > 150 {
			t.Fatal("pagination stalled")
		}
		offset = p.NextOffset
	}
	if pages < 2 || combined.String() != body+"\n\n无附件，只有 SKILL.md 正文。" {
		t.Fatal("skill tail or source bytes missing")
	}
	bad := func(args map[string]any, contains string) {
		t.Helper()
		raw, _ := json.Marshal(args)
		_, err := e.invokeSkillViewTool(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), contains) {
			t.Fatalf("expected %s, got %v", contains, err)
		}
	}
	bad(map[string]any{"skillId": sk.ID, "offset": 1}, "expectedDigest")
	other := sk
	other.Name = "second-source"
	other.ID = ""
	other.DisplayName = "自定义技能二"
	other, err := e.skills.Create(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	bad(map[string]any{"skillId": other.ID, "offset": 1, "expectedDigest": digest}, "SKILL_SOURCE_CHANGED")
	newManifest := `{"prompt":"修改后的约束"}`
	if _, err = e.skills.UpdateFields(context.Background(), sk.ID, nil, nil, nil, &newManifest, nil, nil, sk.Rev); err != nil {
		t.Fatal(err)
	}
	bad(map[string]any{"skillId": sk.ID, "offset": 1, "expectedDigest": digest}, "SKILL_SOURCE_CHANGED")
	if p := readSkillPage(t, e, map[string]any{"skillId": sk.ID}); p.Digest == digest || !strings.Contains(p.Text, "修改后的约束") {
		t.Fatal("refresh did not load new revision")
	}
}

func TestSkillReferenceBeyondOldLimitIsCompleteAndPinnedToItsFile(t *testing.T) {
	e, sk := customSkillFixture(t, "先读参考材料")
	root := t.TempDir()
	prev := testHomeAgentSkillsRoot
	testHomeAgentSkillsRoot = &root
	t.Cleanup(func() { testHomeAgentSkillsRoot = prev })
	dir := filepath.Join(root, ".agents", "skills", sk.Name, "docs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("参考材料不得省略。", 1600) + "必须执行的最后一步。\n"
	file := filepath.Join(dir, "rules.md")
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	offset := 0
	digest := ""
	for {
		p := readSkillPage(t, e, map[string]any{"skillId": sk.ID, "path": "docs/rules.md", "offset": offset, "expectedDigest": digest})
		digest = p.Digest
		got.WriteString(p.Text)
		if !p.HasMore {
			break
		}
		offset = p.NextOffset
	}
	if got.String() != body {
		t.Fatal("reference was truncated or whitespace changed")
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"docs/other.md", ""} {
		raw, _ := json.Marshal(map[string]any{"skillId": sk.ID, "path": path, "offset": 1, "expectedDigest": digest})
		if _, err := e.invokeSkillViewTool(context.Background(), raw); err == nil || !strings.Contains(err.Error(), "SKILL_SOURCE_CHANGED") {
			t.Fatalf("different source accepted: %s %v", path, err)
		}
	}
	if err := os.WriteFile(file, []byte(body+"新规则"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"skillId": sk.ID, "path": "docs/rules.md", "offset": 1, "expectedDigest": digest})
	if _, err := e.invokeSkillViewTool(context.Background(), raw); err == nil || !strings.Contains(err.Error(), "SKILL_SOURCE_CHANGED") {
		t.Fatalf("changed file accepted: %v", err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", skillDocumentMaxBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"skillId": sk.ID, "path": "docs/rules.md"})
	if _, err := e.invokeSkillViewTool(context.Background(), raw); err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatalf("oversized reference not explicit: %v", err)
	}
}

func TestCustomSkillReferencesDoNotResolveAnotherSkillsDisplayName(t *testing.T) {
	e, sk := customSkillFixture(t, "保留自己的规则")
	root := t.TempDir()
	prev := testHomeAgentSkillsRoot
	testHomeAgentSkillsRoot = &root
	t.Cleanup(func() { testHomeAgentSkillsRoot = prev })
	// A display label can collide with another skill folder. Only this installed
	// skill's stable ID/name may resolve its reference files.
	dir := filepath.Join(root, ".agents", "skills", sk.DisplayName, "docs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foreign.md"), []byte("OTHER SKILL PRIVATE RULES"), 0600); err != nil {
		t.Fatal(err)
	}
	p := readSkillPage(t, e, map[string]any{"skillId": sk.ID, "path": "docs/foreign.md"})
	if strings.Contains(p.Text, "OTHER SKILL") || !strings.Contains(p.Text, "没有附件") {
		t.Fatalf("read different skill folder: %+v", p)
	}
}

func TestCustomSkillCannotInvokeWithoutStoredPromptOrAsExecutablePath(t *testing.T) {
	for _, entry := range []string{"SKILL.md", "scripts/run.ps1"} {
		e, sk := customSkillFixture(t, "valid initial prompt")
		empty := `{}`
		if _, err := e.skills.UpdateFields(context.Background(), sk.ID, nil, nil, &entry, &empty, nil, nil, sk.Rev); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]string{"skillId": sk.ID, "input": "执行任务"})
		if _, err := e.invokeSkillTool(context.Background(), executionModeFullAccess, "", args); err == nil {
			t.Fatalf("unsupported skill executed: %s", entry)
		}
	}
}

type completeSkillAdapter struct {
	skillID  string
	requests []llmadapter.Request
	repeat   bool
}

func (*completeSkillAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("unused")
}
func (*completeSkillAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("unused")
}
func (a *completeSkillAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.requests = append(a.requests, req)
	if len(a.requests) == 1 || (a.repeat && len(a.requests) == 2) {
		args, _ := json.Marshal(map[string]string{"skillId": a.skillID, "input": "请按技能完成复核"})
		id := "invoke-full-skill"
		if len(a.requests) == 2 {
			id = "invoke-again"
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: id, Name: "skill.invoke", Arguments: args}}}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "已按完整技能进行复核。"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{}, nil
}

func TestPublishedCustomSkillInvocationPreservesFullModelContextOrStopsAtBudget(t *testing.T) {
	body := strings.Repeat("逐项执行约束。", 2100) + "唯一尾部约束：绝不能跳过最终复核。"
	for _, window := range []int64{128000, 4096} {
		e, sk := customSkillFixture(t, body)
		adapter := &completeSkillAdapter{skillID: sk.ID}
		e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
		ctx, cancel := context.WithCancel(context.Background())
		state := &streamState{cancel: cancel, state: streamRunning}
		e.streams["complete-skill"] = state
		var events []bridge.Event
		e.runStream(ctx, "complete-skill", state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref", Models: []provider.Model{{ModelID: "model", ContextWindow: window}}}, llmadapter.Request{Model: "model", MaxTokens: 1024, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "请按技能完成复核"}}}, func(ev bridge.Event) error { events = append(events, ev); return nil }, "", executionModeFullAccess)
		cancel()
		if window == 4096 {
			if len(adapter.requests) != 1 {
				t.Fatalf("ran model with incomplete skill: %d", len(adapter.requests))
			}
			found := false
			for _, ev := range events {
				if ev.Type == bridge.EventFailed && ev.Error != nil && ev.Error.Code == "SKILL_CONTEXT_BUDGET_EXCEEDED" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing honest context-budget failure")
			}
			continue
		}
		if len(adapter.requests) != 2 {
			t.Fatalf("invoke failed: calls=%d events=%+v", len(adapter.requests), events)
		}
		full := ""
		for _, m := range adapter.requests[1].Messages {
			if m.Role == llmadapter.RoleTool && m.ToolCallID == "invoke-full-skill" {
				full = m.Content
			}
		}
		if !strings.Contains(full, body) || !strings.Contains(full, "manifestDigest=") || !strings.Contains(full, "skillId="+sk.ID) {
			t.Fatal("model did not receive complete version-pinned skill")
		}
		for _, ev := range events {
			if ev.Type == bridge.EventToolCompleted && ev.Tool != nil {
				if len(ev.Tool.Summary) > 4096 || !strings.Contains(ev.Tool.Summary, "完整正文") {
					t.Fatal("UI summary contract broken")
				}
			}
		}
	}
}

func TestDuplicateSkillInvocationRetainsOriginalFullAgreement(t *testing.T) {
	body := strings.Repeat("重复调用也不能丢约束。", 600) + "不可省略的最后约束。"
	e, sk := customSkillFixture(t, body)
	adapter := &completeSkillAdapter{skillID: sk.ID, repeat: true}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["repeat-skill"] = state
	e.runStream(ctx, "repeat-skill", state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model", MaxTokens: 1024, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "请按技能完成复核"}}}, func(bridge.Event) error { return nil }, "", executionModeFullAccess)
	if len(adapter.requests) != 3 {
		t.Fatalf("unexpected continuation count: %d", len(adapter.requests))
	}
	fullCount := 0
	replayFound := false
	for _, m := range adapter.requests[2].Messages {
		if m.Role != llmadapter.RoleTool {
			continue
		}
		if strings.Contains(m.Content, body) {
			fullCount++
		}
		if m.ToolCallID == "invoke-again" {
			replayFound = strings.Contains(m.Content, "不是技能全文") && strings.Contains(m.Content, "invoke-full-skill") && !strings.Contains(m.Content, body)
		}
	}
	if fullCount != 1 || !replayFound {
		t.Fatalf("complete original lost or duplicated, fullCount=%d replay=%t", fullCount, replayFound)
	}
}

func TestIncompleteSkillCannotBeBypassedByOfficeGenerationFallback(t *testing.T) {
	e, _ := officeEngineFixture(t)
	turn := &chatTurnCheckpoint{Goal: "根据技能生成 PPT", PptActive: true, PptStage: pptStageCopy, LastTools: []string{"command.run"}}
	err := errors.Join(errors.New("wrapped failure"), errSkillContextBudget)
	if shouldAutoOfficeGen(turn, err) {
		t.Fatal("incomplete skill requested automatic output")
	}
	events := 0
	done, notice := e.tryFinishOfficeGen(context.Background(), executionModeFullAccess, "", turn, "# 汇报\n## 内容\n这里是已经收到但尚未经过技能复核的正文。", err, func(bridge.Event) error { events++; return nil })
	if done || notice != "" || events != 0 || turn.PptStage != pptStageCopy {
		t.Fatalf("budget failure bypassed by generation: done=%t notice=%s events=%d stage=%s", done, notice, events, turn.PptStage)
	}
}
