package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

// planFakeAdapter answers the plan/execute/verify Complete calls in order.
type planFakeAdapter struct {
	calls int
}

func (a *planFakeAdapter) Complete(_ context.Context, _ []byte, req llmadapter.Request) (llmadapter.Response, error) {
	a.calls++
	switch {
	case strings.Contains(req.Messages[0].Content, "planning coordinator"):
		return llmadapter.Response{Message: llmadapter.Message{Content: `{"steps":[{"action":"read","detail":"inspect the file"},{"action":"report","detail":"summarize findings"}]}`}}, nil
	case strings.Contains(req.Messages[0].Content, "execution agent"):
		return llmadapter.Response{Message: llmadapter.Message{Content: "step outcome: done"}}, nil
	case strings.Contains(req.Messages[0].Content, "verifier"):
		return llmadapter.Response{Message: llmadapter.Message{Content: `{"verified":true,"gaps":""}`}}, nil
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: "unexpected"}}, nil
}

func (a *planFakeAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, context.Canceled
}

func (a *planFakeAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

type planPurposeAdapter struct{ planFakeAdapter }

func (a *planPurposeAdapter) Complete(ctx context.Context, secret []byte, req llmadapter.Request) (llmadapter.Response, error) {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "execution agent") {
		return llmadapter.Response{Message: llmadapter.Message{Content: `{"l0":{"kind":"check","passed":false,"uncertain":true,"detail":"incomplete"}}`}}, nil
	}
	return a.planFakeAdapter.Complete(ctx, secret, req)
}

type planStepToolAdapter struct {
	wrote bool
}

func (a *planStepToolAdapter) Complete(_ context.Context, _ []byte, req llmadapter.Request) (llmadapter.Response, error) {
	if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == llmadapter.RoleTool {
		return llmadapter.Response{Message: llmadapter.Message{Content: "step outcome: done"}}, nil
	}
	if a.wrote {
		return llmadapter.Response{Message: llmadapter.Message{Content: "step outcome: done"}}, nil
	}
	a.wrote = true
	return llmadapter.Response{Message: llmadapter.Message{ToolCalls: []llmadapter.ToolCall{{
		ID: "w1", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"plan-secret.txt","content":"no"}`),
	}}}}, nil
}

func (a *planStepToolAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, context.Canceled
}

func (a *planStepToolAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

func TestPlanStepRecordsReceiptWithoutAutoApprove(t *testing.T) {
	e := newSubagentChatEngine(t)
	store := &memToolOps{}
	e.SetToolOperationStore(store)
	out := e.executePlanStep(context.Background(), &planStepToolAdapter{}, nil, "model-x", subTestSession, executionModeApproval, "inspect", planStep{Action: "write", Detail: "do not write"}, 1, 1, RouteUnspecified)
	if !strings.Contains(out, "approval required") && !strings.Contains(out, "step outcome") {
		// The tool error is fed back to the model; the receipt is the contract.
		t.Log(out)
	}
	var writeOp modelfit.ToolOperation
	for _, op := range mustListToolOps(t, store, subTestSession) {
		if op.ToolName == "workspace.write" {
			writeOp = op
		}
	}
	if writeOp.ID == "" {
		t.Fatal("plan step write must leave a receipt")
	}
	if writeOp.State == modelfit.OpSucceeded {
		t.Fatalf("plan step must keep approved=false: %+v", writeOp)
	}
	if _, err := e.tools.Execute(context.Background(), toolruntime.FullAccess, subTestSession, "workspace.read", json.RawMessage(`{"path":"plan-secret.txt"}`), false); err == nil {
		t.Fatal("gated plan step must not create the file")
	}
}

func TestPlanRunRecordsPlanPurpose(t *testing.T) {
	e := newSubagentChatEngine(t)
	store := &memCalls{}
	e.SetCallAttemptStore(store)
	inner := &planPurposeAdapter{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return inner, nil
	})
	p := provider.Provider{ID: ulid.Make().String(), Protocol: provider.ProtocolOpenAICompatible}
	a, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.invokePlanRunTool(context.Background(), a, nil, "model-x", subTestSession, executionModeFullAccess, json.RawMessage(`{"objective":"audit the workspace files"}`)); err != nil {
		t.Fatal(err)
	}
	plan, judge := 0, 0
	for _, rec := range store.recs {
		switch rec.Purpose {
		case "plan":
			plan++
		case "judge":
			judge++
		}
	}
	if plan < 2 {
		t.Fatalf("planner and executor must record purpose plan: %+v", purposesOf(store))
	}
	if judge < 1 {
		t.Fatalf("verifier must stay purpose judge: %+v", purposesOf(store))
	}
}

func purposesOf(store *memCalls) []string {
	out := make([]string, 0, len(store.recs))
	for _, rec := range store.recs {
		out = append(out, rec.Purpose)
	}
	return out
}

func TestPlanRunCyclePlanExecuteVerify(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &planFakeAdapter{}
	out, err := e.invokePlanRunTool(context.Background(), adapter, nil, "model-x", subTestSession, executionModeFullAccess, json.RawMessage(`{"objective":"audit the workspace files"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Steps []struct {
			Action string `json:"action"`
		} `json:"steps"`
		Log      string `json:"log"`
		Verified bool   `json:"verified"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 2 || res.Steps[0].Action != "read" {
		t.Fatalf("steps = %+v", res.Steps)
	}
	if !strings.Contains(res.Log, "step 1 [read]") || !strings.Contains(res.Log, "step outcome: done") {
		t.Fatalf("log = %q", res.Log)
	}
	if res.Verified {
		t.Fatal("D-C2: analysis steps without l0 must not verify")
	}
	// plan(1) + 2 steps(2); verifier skipped when no l0
	if adapter.calls != 3 {
		t.Fatalf("adapter calls = %d, want 3 (no verifier)", adapter.calls)
	}
}

func TestExtractL0(t *testing.T) {
	obs, ok := extractL0("opened foo\n{\"l0\":{\"kind\":\"foreground\",\"passed\":true}}")
	if !ok || !obs.Passed || obs.Kind != "foreground" {
		t.Fatalf("got %+v ok=%v", obs, ok)
	}
	if _, ok := extractL0("step outcome: done"); ok {
		t.Fatal("plain text must not parse as l0")
	}
}

func TestDecidePlanVerify(t *testing.T) {
	skip, verified, gaps := decidePlanVerify(nil)
	if !skip || verified || gaps != "no l0 observation" {
		t.Fatalf("D-C2 empty: skip=%v verified=%v gaps=%q", skip, verified, gaps)
	}
	skip, verified, gaps = decidePlanVerify([]l0Observation{
		{Kind: "foreground", Passed: true},
		{Kind: "file", Passed: true},
	})
	if !skip || !verified || gaps != "" {
		t.Fatalf("D-C3 all passed: skip=%v verified=%v gaps=%q", skip, verified, gaps)
	}
	skip, verified, _ = decidePlanVerify([]l0Observation{
		{Kind: "foreground", Passed: true},
		{Kind: "pixels", Passed: false},
	})
	if skip || verified {
		t.Fatalf("failed l0 must call judge: skip=%v verified=%v", skip, verified)
	}
}

func TestAttachPlanStepL0SurfacesToolObservation(t *testing.T) {
	got := attachPlanStepL0("already opened.", []string{
		"opened soda\n{\"l0\":{\"kind\":\"foreground\",\"passed\":true,\"detail\":\"soda\"}}",
	})
	if !strings.Contains(got, "already opened.") {
		t.Fatalf("coda lost: %q", got)
	}
	obs, ok := extractL0(got)
	if !ok || !obs.Passed || obs.Kind != "foreground" {
		t.Fatalf("D-C3 tool l0 missing: %+v ok=%v out=%q", obs, ok, got)
	}
}

type flashJudgeProvider struct{ providerRepositoryStub }

func (flashJudgeProvider) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return []provider.Provider{{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW",
		Models: []provider.Model{
			{ModelID: "gpt-4o", DisplayName: "GPT-4o"},
			{ModelID: "gpt-4o-mini", DisplayName: "GPT-4o mini"},
		},
	}}, nil
}

type planJudgeAdapter struct {
	verifyModel string
	calls       int
}

func (a *planJudgeAdapter) Complete(_ context.Context, _ []byte, req llmadapter.Request) (llmadapter.Response, error) {
	a.calls++
	sys := ""
	if len(req.Messages) > 0 {
		sys = req.Messages[0].Content
	}
	switch {
	case strings.Contains(sys, "planning coordinator"):
		return llmadapter.Response{Message: llmadapter.Message{Content: `{"steps":[{"action":"open","detail":"open app"}]}`}}, nil
	case strings.Contains(sys, "execution agent"):
		return llmadapter.Response{Message: llmadapter.Message{Content: "opened\n{\"l0\":{\"kind\":\"foreground\",\"passed\":false,\"uncertain\":true}}"}}, nil
	case strings.Contains(sys, "verifier"):
		a.verifyModel = req.Model
		user := ""
		if n := len(req.Messages); n > 0 {
			user = req.Messages[n-1].Content
		}
		if !strings.Contains(user, "L0:") {
			return llmadapter.Response{Message: llmadapter.Message{Content: `{"verified":false,"gaps":"missing l0"}`}}, nil
		}
		return llmadapter.Response{Message: llmadapter.Message{Content: `{"verified":false,"gaps":"l0 incomplete"}`}}, nil
	}
	return llmadapter.Response{Message: llmadapter.Message{Content: "unexpected"}}, nil
}

func (a *planJudgeAdapter) Stream(context.Context, []byte, llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return llmadapter.Response{}, context.Canceled
}

func (a *planJudgeAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}

func TestPlanVerifyUsesFlashWhenVendorHasPlusAndFlash(t *testing.T) {
	e := newSubagentChatEngine(t)
	e.providers = flashJudgeProvider{}
	adapter := &planJudgeAdapter{}
	out, err := e.invokePlanRunTool(context.Background(), adapter, nil, "gpt-4o", subTestSession, executionModeFullAccess, json.RawMessage(`{"objective":"open the player"}`))
	if err != nil {
		t.Fatal(err)
	}
	if adapter.verifyModel == "" || adapter.verifyModel == "gpt-4o" {
		t.Fatalf("D-C1 verify model=%q want flash, out=%s", adapter.verifyModel, out)
	}
	if adapter.verifyModel != "gpt-4o-mini" {
		t.Fatalf("D-C1 verify model=%q", adapter.verifyModel)
	}
}

func TestPlanStepToolsInheritRoute(t *testing.T) {
	e := NewEngine(nil, "test")
	got := planStepTools(e, RouteR1)
	for _, d := range got {
		if d.Name == "computer.act" || d.Name == "desktop.open" {
			t.Fatalf("R1 plan step leaked %s", d.Name)
		}
	}
}

func TestPlanRunGuardsAndFallback(t *testing.T) {
	e := newSubagentChatEngine(t)
	adapter := &planFakeAdapter{}
	if _, err := e.invokePlanRunTool(context.Background(), adapter, nil, "m", subTestSession, executionModeApproval, json.RawMessage(`{"objective":""}`)); err == nil {
		t.Fatal("empty objective accepted")
	}
	if adapter.calls != 0 {
		t.Fatal("adapter called for invalid objective")
	}
	// Malformed plan JSON degrades to a single analysis step, not a crash.
	degenerate := &planFakeAdapter{}
	e2 := newSubagentChatEngine(t)
	// reuse invoke via malformed first answer
	out, err := e2.invokePlanRunToolWithPlanner(context.Background(), degenerate, nil, "m", subTestSession, executionModeApproval, "objective", "not json at all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "analysis") {
		t.Fatalf("fallback plan missing: %s", out)
	}
}

func TestPlanToolDefinitionsTiers(t *testing.T) {
	if got := planToolDefinitions(executionModePlan); len(got) != 0 {
		t.Fatalf("plan mode exposed plan.run: %+v", got)
	}
	if got := planToolDefinitions(executionModeApproval); len(got) != 1 || got[0].Name != "plan.run" {
		t.Fatalf("approval tier defs = %+v", got)
	}
}

func TestComplexityTierHintWiring(t *testing.T) {
	// Simple conversation: no hint.
	simple := []llmadapter.Message{{Role: llmadapter.RoleSystem, Content: "sys"}, {Role: llmadapter.RoleUser, Content: "hi"}}
	if got := complexityTierHint(simple); got != "" {
		t.Fatalf("simple conversation hinted: %q", got)
	}
	// Moderate: deep turn history plus tool traffic lands in the 8-16 band.
	many := []llmadapter.Message{{Role: llmadapter.RoleSystem, Content: "sys"}}
	for i := 0; i < 10; i++ {
		many = append(many, llmadapter.Message{Role: llmadapter.RoleUser, Content: "question"}, llmadapter.Message{Role: llmadapter.RoleTool, Content: "tool output"}, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "answer"})
	}
	hint := complexityTierHint(many)
	if !strings.Contains(hint, "moderate") || !strings.Contains(hint, "plan.run") {
		t.Fatalf("moderate hint = %q", hint)
	}
}

func TestComplexityTierHintDoesNotExceedFinalBudget(t *testing.T) {
	many := []llmadapter.Message{{Role: llmadapter.RoleSystem, Content: "sys"}}
	for i := 0; i < 10; i++ {
		many = append(many, llmadapter.Message{Role: llmadapter.RoleUser, Content: "question"}, llmadapter.Message{Role: llmadapter.RoleTool, Content: "tool output"}, llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "answer"})
	}
	if complexityTierHint(many) == "" {
		t.Fatal("fixture must produce a hint")
	}
	used := countVisibleRequestTokens("glm-4", many, nil)
	info := contextapp.ProviderInfo{Model: "glm-4", ContextWindow: used + 1, SafetyCeiling: used + 1}
	if err := errIfRequestOverBudget(used, info); err != nil {
		t.Fatalf("base messages must fit: %v", err)
	}
	got := applyComplexityTierHint(many, nil, info)
	if err := errIfRequestOverBudget(countVisibleRequestTokens("glm-4", got, nil), info); err != nil {
		t.Fatalf("hint must not push the request over budget: %v", err)
	}
	if got[0].Content != many[0].Content {
		t.Fatal("over-budget hint must be skipped rather than sent")
	}
}
