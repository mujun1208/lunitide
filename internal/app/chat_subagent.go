// Chat-layer subagent delegation (P1-1): exposes subagent.spawn /
// subagent.join as model tools inside the chat gateway loop. Spawn runs one
// independent read-only sub-session through the M7 SubagentService (quota,
// idempotency, audit), the sub-session answers with a single report
// (observation), and the main loop receives that summary exactly once.
// Delegation tiers: disabled (tools hidden), explicit (tools available),
// proactive (tools available + system prompt encourages delegation).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// delegationMode is the P1-1 delegation tier.
type delegationMode string

const (
	delegationDisabled  delegationMode = "disabled"
	delegationExplicit  delegationMode = "explicit"
	delegationProactive delegationMode = "proactive"
)

func delegationModeValid(m delegationMode) bool {
	return m == delegationDisabled || m == delegationExplicit || m == delegationProactive
}

// subagent budget / deadline defaults. The budget inherits from the parent
// request budget (MaxTokens 4096) with a 2x research allowance, clamped into
// the frozen M7 guard window; the deadline is fixed at 5 minutes.
const (
	subagentDefaultBudgetTokens = 8192
	subagentDeadlineMS          = 5 * 60 * 1000
	subagentMaxSteps            = 4
	subagentMaxSummaryChars     = 2000
	// maxParallelSubagentSpawns bounds how many subagent.spawn calls of a
	// single model turn may overlap (M7 quota allows 4 live per root, so
	// the local bound keeps one slot of headroom for unrelated runs).
	maxParallelSubagentSpawns = 3
)

// subagentReadOnlyCaps is the frozen read-only capability subset granted to
// chat-spawned subagents (all entries must satisfy m7flow.SagCapAllowed).
var subagentReadOnlyCaps = []string{"fs.read", "fs.tree", "web.fetch", "web.search"}

const subagentSystemPrompt = "You are a read-only research subagent. Investigate the assigned purpose using only the provided read-only tools (workspace listing/reading, allowlisted commands, web fetch/search), then answer with a single concise report (max 2000 characters) containing findings and conclusions. You cannot write files, run mutating commands, or spawn further agents."

// SetDelegationMode configures the delegation tier. An invalid value is
// refused (fail-closed); the zero value defaults to the explicit tier so
// wiring SubagentService alone enables model-visible delegation.
func (e *Engine) SetDelegationMode(m delegationMode) error {
	if !delegationModeValid(m) {
		return errors.New("invalid delegation mode")
	}
	e.delegation = m
	return nil
}

// subagentToolDefinitions returns the model tool definitions for the
// delegation tier. disabled hides the tools entirely (fail-closed); the
// unset zero value behaves as explicit.
func (e *Engine) subagentToolDefinitions(mode executionMode) []gateway.ToolDefinition {
	if mode == executionModePlan || e.delegation == delegationDisabled || e.m7subagent == nil {
		return nil
	}
	return []gateway.ToolDefinition{
		{
			Name:        "subagent.spawn",
			Description: "Spawn one read-only research subagent with an independent budget. It investigates the purpose (workspace reads, allowlisted commands, web fetch/search) and returns a single summary report. Use for self-contained research subtasks such as codebase survey or documentation lookup. Multiple subagent.spawn calls in the same turn run in parallel (up to 3), so batch independent research tasks together.",
			Schema:      []byte(`{"type":"object","properties":{"purpose":{"type":"string","minLength":1,"maxLength":2000,"description":"Self-contained research task for the subagent"},"budgetTokens":{"type":"integer","minimum":1000,"maximum":50000,"description":"Optional token budget inherited default 8192"}},"required":["purpose"],"additionalProperties":false}`),
		},
		{
			Name:        "subagent.join",
			Description: "Re-read the summary report of one previously spawned subagent (single report, read-only).",
			Schema:      []byte(`{"type":"object","properties":{"subagentId":{"type":"string","minLength":1,"maxLength":128}},"required":["subagentId"],"additionalProperties":false}`),
		},
	}
}

// delegationHint is appended to the execution-mode instruction on the
// proactive tier only.
const delegationProactiveHint = " Delegation: for complex, self-contained research subtasks (multi-file codebase survey, broad documentation or web research), prefer spawning read-only subagents via subagent.spawn and synthesize their reports instead of doing every read yourself. Independent subtasks can be spawned in the same turn and run in parallel."

// subagentToolNames is the dispatch set intercepted before toolruntime.
var subagentToolNames = map[string]bool{"subagent.spawn": true, "subagent.join": true}

// invokeSubagentTool dispatches one subagent.* model tool call. It runs
// inside the provider lease callback, so the adapter, credential and model
// of the parent request are reused for the sub-session.
func (e *Engine) invokeSubagentTool(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID, tool string, rawArgs json.RawMessage) (string, error) {
	switch tool {
	case "subagent.spawn":
		var p struct {
			Purpose      string `json:"purpose"`
			BudgetTokens int64  `json:"budgetTokens"`
		}
		if err := json.Unmarshal(rawArgs, &p); err != nil {
			return "", errors.New("subagent.spawn arguments must be a JSON object")
		}
		if len(p.Purpose) < 1 || len(p.Purpose) > m7flow.SubagentMaxPurpose {
			return "", errors.New("subagent.spawn purpose must be 1-2000 characters")
		}
		budget := p.BudgetTokens
		if budget < 1 {
			budget = subagentDefaultBudgetTokens // inherited default
		}
		if budget < 1000 || budget > m7flow.SubagentMaxBudgetTokens {
			return "", fmt.Errorf("subagent.spawn budgetTokens must be 1000-%d", m7flow.SubagentMaxBudgetTokens)
		}
		return e.runSubagentSession(ctx, a, credential, model, sessionID, p.Purpose, budget)
	case "subagent.join":
		var p struct {
			SubagentID string `json:"subagentId"`
		}
		if err := json.Unmarshal(rawArgs, &p); err != nil || p.SubagentID == "" {
			return "", errors.New("subagent.join requires subagentId")
		}
		res, err := e.m7subagent.Join(ctx, m7app.JoinInput{SubagentRunID: p.SubagentID})
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(map[string]any{
			"subagentId": res.SubagentRunID, "status": res.State,
			"summary": res.Summary, "spentTokens": res.SpentTokens,
		})
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	return "", errors.New("unknown subagent tool " + tool)
}

// runSubagentSession spawns the governed run, executes the independent
// read-only sub-session against the same provider, and completes the run
// with the single report. Failures still complete the run (with a failure
// report) so the concurrency quota is not leaked.
func (e *Engine) runSubagentSession(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID, purpose string, budget int64) (string, error) {
	run, err := e.m7subagent.Spawn(ctx, m7app.SpawnInput{
		RootRunID:      sessionID,
		Purpose:        purpose,
		ReadCaps:       subagentReadOnlyCaps,
		BudgetTokens:   budget,
		DeadlineMS:     subagentDeadlineMS,
		IdempotencyKey: "chat-" + ulid.Make().String(),
		Actor:          "model",
	})
	if err != nil {
		return "", err
	}
	report, spent, execErr := e.executeSubagentLoop(ctx, a, credential, model, sessionID, purpose, budget)
	if len(report) > subagentMaxSummaryChars {
		report = report[:subagentMaxSummaryChars]
	}
	if execErr != nil {
		report = "subagent execution failed: " + execErr.Error()
		if len(report) > subagentMaxSummaryChars {
			report = report[:subagentMaxSummaryChars]
		}
	}
	if _, completeErr := e.m7subagent.Complete(ctx, run.ID, spent, []m7app.ObservationInput{{EvidenceID: ulid.Make().String(), Summary: report}}); completeErr != nil {
		// Double failure (execErr + completeErr) must not swallow the
		// completion error silently: the run would stay open and leak
		// the concurrency quota. Surface whichever error we have.
		if execErr == nil {
			return "", completeErr
		}
		log.Printf("subagent complete failed after execution error (quota may leak until deadline): %v", completeErr)
	}
	out, err := json.Marshal(map[string]any{
		"subagentId": run.ID, "status": "completed", "summary": report, "spentTokens": spent,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// executeSubagentLoop runs the independent read-only sub-session: at most
// subagentMaxSteps model turns, tools restricted to the read-only subset,
// returning the final report text and total tokens spent.
func (e *Engine) executeSubagentLoop(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID, purpose string, budget int64) (string, int64, error) {
	maxTokens := int(budget)
	if maxTokens > 16000 {
		maxTokens = 16000
	}
	if maxTokens < 512 {
		maxTokens = 512
	}
	req := gateway.Request{
		Model:       model,
		MaxTokens:   maxTokens,
		MaxAttempts: 1,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: subagentSystemPrompt},
			{Role: gateway.RoleUser, Content: purpose},
		},
		Tools: readOnlyEngineToolDefinitions(),
	}
	var spent int64
	var report string
	for step := 0; step < subagentMaxSteps; step++ {
		resp, err := a.Complete(ctx, credential, req)
		if err != nil {
			return report, spent, err
		}
		spent += int64(resp.Usage.TotalTokens)
		if len(resp.Message.ToolCalls) == 0 {
			return strings.TrimSpace(resp.Message.Content), spent, nil
		}
		req.Messages = append(req.Messages, resp.Message)
		for _, call := range resp.Message.ToolCalls {
			summary := ""
			if !subagentAllowedTools[call.Name] {
				// Defense in depth: even if the model hallucinates a
				// tool outside the declared read-only list, the runtime
				// never executes it (FullAccess would bypass approval).
				summary = "refused: subagent is read-only"
			} else {
				r, toolErr := e.tools.Execute(ctx, toolruntime.FullAccess, sessionID, call.Name, call.Arguments, false)
				if toolErr != nil {
					summary = toolErr.Error()
				} else {
					summary = r.Output
				}
			}
			if len(summary) > 4096 {
				summary = summary[:4096]
			}
			req.Messages = append(req.Messages, gateway.Message{Role: gateway.RoleTool, ToolCallID: call.ID, Content: summary})
		}
	}
	return report, spent, errors.New("subagent exceeded max steps without a final report")
}

// readOnlyEngineToolDefinitions is the read-only subset of the engine tools
// granted to sub-sessions: no workspace.write and no anchored edits,
// everything else already read-only by construction (command allowlist is
// read-only git/go).
func readOnlyEngineToolDefinitions() []gateway.ToolDefinition {
	all := engineToolDefinitions()
	out := make([]gateway.ToolDefinition, 0, len(all))
	for _, d := range all {
		if d.Name == "workspace.write" || d.Name == "workspace.edit" || d.Name == "browser.act" {
			continue
		}
		out = append(out, d)
	}
	return out
}

// subagentAllowedTools is the exact name set of
// readOnlyEngineToolDefinitions, computed once. The subagent loop uses it
// as an allowlist so hallucinated tool calls outside the declared list are
// refused before reaching the runtime (which runs in FullAccess).
var subagentAllowedTools = func() map[string]bool {
	set := make(map[string]bool)
	for _, d := range readOnlyEngineToolDefinitions() {
		set[d.Name] = true
	}
	return set
}()

// subagentFutureResult carries one background spawn outcome to the main
// tool loop.
type subagentFutureResult struct {
	summary string
	err     error
}

// startSubagentFutures pre-starts up to maxParallelSubagentSpawns
// subagent.spawn calls from one model turn on background goroutines so
// independent research subagents overlap instead of queueing. Results
// flow through buffered channels consumed by the main loop in original
// call order, which keeps the event stream and tool-message order
// deterministic; join calls and spawns beyond the bound fall back to
// inline execution.
func startSubagentFutures(ctx context.Context, e *Engine, a gateway.Adapter, credential []byte, model, sessionID string, calls []gateway.ToolCall) map[string]chan subagentFutureResult {
	futures := make(map[string]chan subagentFutureResult)
	started := 0
	for _, call := range calls {
		if call.Name != "subagent.spawn" || started >= maxParallelSubagentSpawns {
			continue
		}
		started++
		ch := make(chan subagentFutureResult, 1)
		futures[call.ID] = ch
		go func(call gateway.ToolCall) {
			defer func() {
				if r := recover(); r != nil {
					ch <- subagentFutureResult{err: fmt.Errorf("subagent panicked: %v", r)}
				}
			}()
			summary, err := e.invokeSubagentTool(ctx, a, credential, model, sessionID, call.Name, call.Arguments)
			ch <- subagentFutureResult{summary: summary, err: err}
		}(call)
	}
	return futures
}
