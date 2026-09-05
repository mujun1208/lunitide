package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
)

// planFakeAdapter answers the plan/execute/verify Complete calls in order.
type planFakeAdapter struct {
	calls int
}

func (a *planFakeAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	a.calls++
	switch {
	case strings.Contains(req.Messages[0].Content, "planning coordinator"):
		return gateway.Response{Message: gateway.Message{Content: `{"steps":[{"action":"read","detail":"inspect the file"},{"action":"report","detail":"summarize findings"}]}`}}, nil
	case strings.Contains(req.Messages[0].Content, "execution agent"):
		return gateway.Response{Message: gateway.Message{Content: "step outcome: done"}}, nil
	case strings.Contains(req.Messages[0].Content, "verifier"):
		return gateway.Response{Message: gateway.Message{Content: `{"verified":true,"gaps":""}`}}, nil
	}
	return gateway.Response{Message: gateway.Message{Content: "unexpected"}}, nil
}

func (a *planFakeAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, context.Canceled
}

func (a *planFakeAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
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

func (a *planJudgeAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	a.calls++
	sys := ""
	if len(req.Messages) > 0 {
		sys = req.Messages[0].Content
	}
	switch {
	case strings.Contains(sys, "planning coordinator"):
		return gateway.Response{Message: gateway.Message{Content: `{"steps":[{"action":"open","detail":"open app"}]}`}}, nil
	case strings.Contains(sys, "execution agent"):
		return gateway.Response{Message: gateway.Message{Content: "opened\n{\"l0\":{\"kind\":\"foreground\",\"passed\":false,\"uncertain\":true}}"}}, nil
	case strings.Contains(sys, "verifier"):
		a.verifyModel = req.Model
		user := ""
		if n := len(req.Messages); n > 0 {
			user = req.Messages[n-1].Content
		}
		if !strings.Contains(user, "L0:") {
			return gateway.Response{Message: gateway.Message{Content: `{"verified":false,"gaps":"missing l0"}`}}, nil
		}
		return gateway.Response{Message: gateway.Message{Content: `{"verified":false,"gaps":"l0 incomplete"}`}}, nil
	}
	return gateway.Response{Message: gateway.Message{Content: "unexpected"}}, nil
}

func (a *planJudgeAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, context.Canceled
}

func (a *planJudgeAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
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
	simple := []gateway.Message{{Role: gateway.RoleSystem, Content: "sys"}, {Role: gateway.RoleUser, Content: "hi"}}
	if got := complexityTierHint(simple); got != "" {
		t.Fatalf("simple conversation hinted: %q", got)
	}
	// Moderate: deep turn history plus tool traffic lands in the 8-16 band.
	many := []gateway.Message{{Role: gateway.RoleSystem, Content: "sys"}}
	for i := 0; i < 10; i++ {
		many = append(many, gateway.Message{Role: gateway.RoleUser, Content: "question"}, gateway.Message{Role: gateway.RoleTool, Content: "tool output"}, gateway.Message{Role: gateway.RoleAssistant, Content: "answer"})
	}
	hint := complexityTierHint(many)
	if !strings.Contains(hint, "moderate") || !strings.Contains(hint, "plan.run") {
		t.Fatalf("moderate hint = %q", hint)
	}
}
