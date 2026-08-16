// Chat-layer plan loop (P1-3): the plan.run model tool drives one
// LLM-authored plan → governed execution → verification cycle for
// multi-step objectives. The complexity router (complexity.decide wiring)
// labels the conversation tier on chat.start so the model knows when the
// planned path is warranted; plan.run itself stays available in every
// non-plan execution mode and inherits the parent mode's tool authority.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/complexity"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	planMaxSteps    = 6
	planMaxObjectiv = 2000
)

const planSystemPrompt = "You are a planning coordinator. Given an objective, produce a short ordered plan as strict JSON: {\"steps\":[{\"action\":\"tool or analysis\",\"detail\":\"one sentence\"}]}. Maximum 6 steps. Answer with JSON only."

const planVerifyPrompt = "You are a verifier. Compare the objective with the execution log. Answer strict JSON: {\"verified\":true|false,\"gaps\":\"missing items or empty\"}. Answer with JSON only."

// planToolDefinitions exposes plan.run except in plan mode (planning about
// planning is refused fail-closed).
func planToolDefinitions(mode executionMode) []gateway.ToolDefinition {
	if mode == executionModePlan {
		return nil
	}
	return []gateway.ToolDefinition{
		{
			Name:        "plan.run",
			Description: "Run one plan-execute-verify cycle for a multi-step objective: an LLM-authored plan is executed step by step with the session tools, then verified against the objective. Returns the plan, per-step outcomes and the verification verdict. Prefer this over ad-hoc tool chains for objectives with 3+ dependent steps.",
			Schema:      []byte(`{"type":"object","properties":{"objective":{"type":"string","minLength":1,"maxLength":2000,"description":"Self-contained objective for the cycle"}},"required":["objective"],"additionalProperties":false}`),
		},
	}
}

// invokePlanRunTool executes the plan-execute-verify cycle inside the
// provider lease. Execution inherits the parent execution mode's tool
// authority (approval gating still applies through toolruntime).
func (e *Engine) invokePlanRunTool(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID string, mode executionMode, rawArgs json.RawMessage) (string, error) {
	var p struct {
		Objective string `json:"objective"`
	}
	if err := json.Unmarshal(rawArgs, &p); err != nil || len(p.Objective) < 1 || len(p.Objective) > planMaxObjectiv {
		return "", errors.New("plan.run requires objective of 1-2000 characters")
	}
	// Phase 1: plan.
	planReq := gateway.Request{
		Model: model, MaxTokens: 1024, MaxAttempts: 1,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: planSystemPrompt},
			{Role: gateway.RoleUser, Content: p.Objective},
		},
	}
	planResp, err := a.Complete(ctx, credential, planReq)
	if err != nil {
		return "", fmt.Errorf("plan phase failed: %w", err)
	}
	return e.runPlanCycle(ctx, a, credential, model, sessionID, mode, p.Objective, planResp.Message.Content)
}

// invokePlanRunToolWithPlanner runs the cycle with a fixed planner answer
// (tests: malformed plan degradation).
func (e *Engine) invokePlanRunToolWithPlanner(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID string, mode executionMode, objective, plannerAnswer string) (string, error) {
	return e.runPlanCycle(ctx, a, credential, model, sessionID, mode, objective, plannerAnswer)
}

// runPlanCycle executes phases 2-3 against a prepared plan document.
func (e *Engine) runPlanCycle(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID string, mode executionMode, objective, planDocument string) (string, error) {
	steps := parsePlanSteps(planDocument)
	// Phase 2: execute step by step with the full session toolset.
	var log strings.Builder
	for i, step := range steps {
		outcome := e.executePlanStep(ctx, a, credential, model, sessionID, mode, objective, step, i+1, len(steps))
		log.WriteString(fmt.Sprintf("step %d [%s]: %s\n", i+1, step.Action, outcome))
	}
	// Phase 3: verify.
	verifyReq := gateway.Request{
		Model: model, MaxTokens: 512, MaxAttempts: 1,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: planVerifyPrompt},
			{Role: gateway.RoleUser, Content: "Objective: " + objective + "\n\nExecution log:\n" + log.String()},
		},
	}
	verifyResp, err := a.Complete(ctx, credential, verifyReq)
	verified, gaps := false, "verifier unavailable"
	if err == nil {
		verified, gaps = parseVerdict(verifyResp.Message.Content)
	}
	out, err := json.Marshal(map[string]any{
		"objective": objective,
		"steps":     steps,
		"log":       log.String(),
		"verified":  verified,
		"gaps":      gaps,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// planStep is one LLM-authored step.
type planStep struct {
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// parsePlanSteps extracts the step list; unparsable plans degrade to a
// single analysis step carrying the raw model output (never fail open).
func parsePlanSteps(raw string) []planStep {
	trimmed := strings.TrimSpace(raw)
	if i := strings.IndexByte(trimmed, '{'); i >= 0 {
		trimmed = trimmed[i:]
	}
	var parsed struct {
		Steps []planStep `json:"steps"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil && len(parsed.Steps) > 0 {
		if len(parsed.Steps) > planMaxSteps {
			parsed.Steps = parsed.Steps[:planMaxSteps]
		}
		return parsed.Steps
	}
	return []planStep{{Action: "analysis", Detail: truncatePlanText(raw, 500)}}
}

func parseVerdict(raw string) (bool, string) {
	trimmed := strings.TrimSpace(raw)
	if i := strings.IndexByte(trimmed, '{'); i >= 0 {
		trimmed = trimmed[i:]
	}
	var v struct {
		Verified bool   `json:"verified"`
		Gaps     string `json:"gaps"`
	}
	if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
		return v.Verified, v.Gaps
	}
	return false, truncatePlanText(raw, 300)
}

func truncatePlanText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// executePlanStep runs one planned step: the model receives the objective,
// the plan and the step under execution, and may use the session toolset
// (bounded to one tool round; approval gating still applies).
func (e *Engine) executePlanStep(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID string, mode executionMode, objective string, step planStep, index, total int) string {
	req := gateway.Request{
		Model: model, MaxTokens: 2048, MaxAttempts: 1,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: "You are an execution agent. Execute exactly the assigned step using the available tools when needed, then answer with a concise outcome report (max 500 characters). Do not perform work belonging to other steps."},
			{Role: gateway.RoleUser, Content: fmt.Sprintf("Objective: %s\nAssigned step %d/%d: %s — %s", objective, index, total, step.Action, step.Detail)},
		},
		Tools: engineToolDefinitions(),
	}
	resp, err := a.Complete(ctx, credential, req)
	if err != nil {
		return "step failed: " + err.Error()
	}
	if len(resp.Message.ToolCalls) == 0 {
		return truncatePlanText(strings.TrimSpace(resp.Message.Content), 500)
	}
	// One bounded tool round for this step.
	req.Messages = append(req.Messages, resp.Message)
	for _, call := range resp.Message.ToolCalls {
		summary := ""
		if subagentToolNames[call.Name] {
			summary = "refused: nested delegation inside plan steps is not allowed"
		} else if planToolNames[call.Name] {
			summary = "refused: nested plan.run inside plan steps is not allowed"
		} else {
			r, toolErr := e.tools.Execute(ctx, toolruntime.Mode(mode), sessionID, call.Name, call.Arguments, false)
			if toolErr != nil {
				summary = toolErr.Error()
			} else {
				summary = r.Output
			}
		}
		if len(summary) > 2048 {
			summary = summary[:2048]
		}
		req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
	}
	final, err := a.Complete(ctx, credential, req)
	if err != nil {
		return "step tool round failed: " + err.Error()
	}
	return truncatePlanText(strings.TrimSpace(final.Message.Content), 500)
}

// planToolNames is the dispatch set intercepted before toolruntime.
var planToolNames = map[string]bool{"plan.run": true}

// complexityTierHint scores the assembled conversation through the frozen
// complexity router and returns the system-message nudge for moderate and
// above ("" for simple). This is the chat-side complexity.decide wiring:
// the same message list always yields the same tier and reason codes.
func complexityTierHint(messages []gateway.Message) string {
	signals := complexity.ConversationSignals{MessageCount: len(messages)}
	for _, m := range messages {
		switch m.Role {
		case gateway.RoleUser:
			signals.TurnCount++
		case gateway.RoleTool:
			signals.ToolCallCount++
		}
		signals.EstTokens += int(token.EstimateTokens(m.Content))
	}
	decision := complexity.Route(signals)
	switch decision.Tier {
	case "moderate":
		return " Conversation tier: moderate (" + strings.Join(decision.ReasonCodes, ",") + "). For multi-step objectives prefer the plan.run tool (plan-execute-verify) over ad-hoc tool chains."
	case "complex", "high-risk":
		return " Conversation tier: " + decision.Tier + " (" + strings.Join(decision.ReasonCodes, ",") + "). Use the plan.run tool (plan-execute-verify) for multi-step objectives and keep steps small and verifiable."
	}
	return ""
}
