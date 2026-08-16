package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

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
	if !res.Verified {
		t.Fatal("verifier reported unverified")
	}
	// plan(1) + 2 steps(2) + verify(1) = 4 adapter calls
	if adapter.calls != 4 {
		t.Fatalf("adapter calls = %d, want 4", adapter.calls)
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
