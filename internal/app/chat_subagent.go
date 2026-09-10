// Chat-layer subagent delegation (P1-1): exposes subagent.spawn /
// subagent.join as model tools inside the chat gateway loop.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	subagentDefaultBudgetTokens = 8192
	subagentDeadlineMS          = 5 * 60 * 1000
	subagentMaxSteps            = 16
	subagentMaxSummaryChars     = 2000
	maxParallelSubagentSpawns   = 3
)

const subagentSystemPrompt = "You are a read-only research subagent. Investigate the assigned purpose using only the provided read-only tools (workspace listing/reading, allowlisted commands, web fetch/search), then answer with a single concise report (max 2000 characters) containing findings and conclusions. You cannot write files, run mutating commands, or spawn further agents."

func (e *Engine) SetDelegationMode(m delegationMode) error {
	if !delegationModeValid(m) {
		return errors.New("invalid delegation mode")
	}
	e.delegation = m
	return nil
}

func effectiveDelegationMode(policy subagentChatPolicy) delegationMode {
	if policy.DelegationMode != "" {
		return policy.DelegationMode
	}
	return delegationExplicit
}

func (e *Engine) subagentToolDefinitions(mode executionMode, policy subagentChatPolicy) []llmadapter.ToolDefinition {
	if mode == executionModePlan || effectiveDelegationMode(policy) == delegationDisabled || e.m7subagent == nil {
		return nil
	}
	return []llmadapter.ToolDefinition{
		{
			Name:        "subagent.spawn",
			Description: "Spawn one read-only subagent with an independent budget and profile (explore, research, general-purpose, review, browser, shell, writer, test). Multiple spawns in one turn run in parallel (up to 3).",
			Schema:      []byte(`{"type":"object","properties":{"purpose":{"type":"string","minLength":1,"maxLength":2000},"profile":{"type":"string","maxLength":64},"budgetTokens":{"type":"integer","minimum":1000,"maximum":50000}},"required":["purpose"],"additionalProperties":false}`),
		},
		{
			Name:        "subagent.join",
			Description: "Re-read the summary report of one previously spawned subagent.",
			Schema:      []byte(`{"type":"object","properties":{"subagentId":{"type":"string","minLength":1,"maxLength":128}},"required":["subagentId"],"additionalProperties":false}`),
		},
	}
}

const delegationProactiveHint = " Delegation: for complex, self-contained research subtasks (multi-file codebase survey, broad documentation or web research), prefer spawning read-only subagents via subagent.spawn with the best profile and synthesize their reports instead of doing every read yourself. Independent subtasks can be spawned in the same turn and run in parallel."

var subagentToolNames = map[string]bool{"subagent.spawn": true, "subagent.join": true}

func (e *Engine) invokeSubagentTool(ctx context.Context, a llmadapter.Adapter, credential []byte, model, sessionID, tool string, rawArgs json.RawMessage, policy subagentChatPolicy) (string, error) {
	switch tool {
	case "subagent.spawn":
		var p struct {
			Purpose      string `json:"purpose"`
			Profile      string `json:"profile"`
			BudgetTokens int64  `json:"budgetTokens"`
		}
		if err := json.Unmarshal(rawArgs, &p); err != nil {
			return "", errors.New("subagent.spawn arguments must be a JSON object")
		}
		if len(p.Purpose) < 1 || len(p.Purpose) > m7flow.SubagentMaxPurpose {
			return "", errors.New("subagent.spawn purpose must be 1-2000 characters")
		}
		profile, ov, ok := resolveSubagentProfile(policy, p.Profile)
		if !ok {
			return "", fmt.Errorf("subagent profile %q is disabled", strings.TrimSpace(p.Profile))
		}
		budget := p.BudgetTokens
		if budget < 1 {
			budget = profile.BudgetTokens
		}
		if budget < 1000 || budget > m7flow.SubagentMaxBudgetTokens {
			return "", fmt.Errorf("subagent.spawn budgetTokens must be 1000-%d", m7flow.SubagentMaxBudgetTokens)
		}
		if policy.ExpertWork {
			profile = applyExpertSpawnCaps(profile, policy.ExpertWriteTools)
		}
		return e.withSubagentAdapter(ctx, a, credential, model, ov, func(op context.Context, subA llmadapter.Adapter, subCred []byte, subModel string) (string, error) {
			return e.runSubagentSession(op, subA, subCred, subModel, sessionID, p.Purpose, budget, profile, subagentToolMode(policy.ParentMode))
		})
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

// Lease callbacks own credential lifetime. A provider override must finish
// inside the callback; returning its slice would use an already-zeroed key.
func (e *Engine) withSubagentAdapter(ctx context.Context, parent llmadapter.Adapter, parentCred []byte, parentModel string, ov subagentProfileOverride, run func(context.Context, llmadapter.Adapter, []byte, string) (string, error)) (string, error) {
	if ov.ModelID == "" {
		return run(ctx, parent, parentCred, parentModel)
	}
	if ov.ProviderID == "" {
		return run(ctx, parent, parentCred, ov.ModelID)
	}
	if e.providers == nil {
		return "", errors.New("subagent provider settings unavailable")
	}
	item, err := e.providers.Get(ctx, ov.ProviderID)
	if err != nil {
		return "", fmt.Errorf("subagent provider unavailable: %w", err)
	}
	var result string
	leaseErr := e.withProviderLease(ctx, item, secretlease.OperationChat, func(op context.Context, cred []byte) error {
		a, err := e.adapter(op, item)
		if err != nil {
			return err
		}
		result, err = run(op, a, cred, ov.ModelID)
		return err
	})
	return result, leaseErr
}

func subagentStoredPurpose(profile subagentProfileDef, purpose string) string {
	label := strings.TrimSpace(profile.DisplayName)
	if label == "" {
		label = profile.ID
	}
	tagged := fmt.Sprintf("[%s] %s", label, purpose)
	if len(tagged) > m7flow.SubagentMaxPurpose {
		tagged = truncateUTF8Bytes(tagged, m7flow.SubagentMaxPurpose)
	}
	return tagged
}

func (e *Engine) runSubagentSession(ctx context.Context, a llmadapter.Adapter, credential []byte, model, sessionID, purpose string, budget int64, profile subagentProfileDef, parentMode executionMode) (string, error) {
	storedPurpose := subagentStoredPurpose(profile, purpose)
	run, err := e.m7subagent.Spawn(ctx, m7app.SpawnInput{
		RootRunID:      sessionID,
		Purpose:        storedPurpose,
		ReadCaps:       profile.ReadCaps,
		PersonaDigest:  subagentPersonaDigest(profile),
		BudgetTokens:   budget,
		DeadlineMS:     subagentDeadlineMS,
		IdempotencyKey: "chat-" + ulid.Make().String(),
		Actor:          "model:" + profile.ID,
	})
	if err != nil {
		return "", err
	}
	executionCtx, cancel := context.WithTimeout(ctx, time.Duration(subagentDeadlineMS)*time.Millisecond)
	defer cancel()
	executionCtx = withSubagentProgress(executionCtx, subagentProgress{ID: run.ID, Profile: profile.DisplayName, Purpose: purpose, Status: "running"})
	emitSubagentProgress(executionCtx, "starting", "", "任务已开始")
	report, spent, execErr := e.executeSubagentSafely(executionCtx, a, credential, model, sessionID, purpose, budget, profile, parentMode)
	status := m7flow.SagCompleted
	if execErr != nil {
		status = m7flow.SagFailed
		if errors.Is(execErr, context.Canceled) {
			status = m7flow.SagCancelled
		}
		report = "子任务执行失败：" + execErr.Error()
	}
	if strings.TrimSpace(report) == "" {
		status = m7flow.SagFailed
		report = "子任务没有返回有效结果"
	}
	report = truncateUTF8Bytes(report, subagentMaxSummaryChars)
	// A cancelled provider call must still release this run's durable quota.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	_, completeErr := e.m7subagent.Finish(finishCtx, run.ID, status, spent, []m7app.ObservationInput{{EvidenceID: ulid.Make().String(), Summary: report}})
	finishCancel()
	if completeErr != nil {
		emitSubagentProgress(executionCtx, "failed", "", "任务已结束，但结果保存失败")
		return "", completeErr
	}
	emitSubagentProgress(executionCtx, status, "", report)
	out, err := json.Marshal(map[string]any{
		"subagentId": run.ID, "status": status, "profile": profile.ID,
		"summary": report, "spentTokens": spent,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (e *Engine) executeSubagentLoop(ctx context.Context, a llmadapter.Adapter, credential []byte, model, sessionID, purpose string, budget int64, profile subagentProfileDef, parentMode executionMode) (string, int64, error) {
	ctx = withCallPurpose(ctx, "subagent")
	maxTokens := int(budget)
	if maxTokens > 16000 {
		maxTokens = 16000
	}
	if maxTokens < 512 {
		maxTokens = 512
	}
	prompt := strings.TrimSpace(profile.SystemPrompt)
	if prompt == "" {
		prompt = subagentSystemPrompt
	}
	if len(profile.WriteTools) > 0 {
		prompt += " This task explicitly grants these additional tools: " + strings.Join(profile.WriteTools, ", ") + ". Use them only for the assigned purpose; all parent approval rules still apply."
	}
	maxSteps := profile.MaxSteps
	if maxSteps < 1 {
		maxSteps = subagentMaxSteps
	}
	tools := subagentEngineToolDefinitions(profile)
	allowed := toolNameSet(tools)
	req := llmadapter.Request{
		Model: model, MaxTokens: maxTokens, MaxAttempts: 1,
		Messages: []llmadapter.Message{
			{Role: llmadapter.RoleSystem, Content: prompt},
			{Role: llmadapter.RoleUser, Content: purpose},
		},
		Tools: tools,
	}
	var spent int64
	for step := 0; step < maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return "", spent, err
		}
		emitSubagentProgress(ctx, "thinking", "", "正在分析任务")
		resp, err := e.completeMaybeRotate(ctx, a, credential, req)
		if err != nil {
			return "", spent, err
		}
		spent += int64(resp.Usage.TotalTokens)
		if len(resp.Message.ToolCalls) == 0 {
			return strings.TrimSpace(resp.Message.Content), spent, nil
		}
		req.Messages = append(req.Messages, resp.Message)
		req.Messages = append(req.Messages, e.runSubagentToolCalls(ctx, sessionID, profile, allowed, resp.Message.ToolCalls, parentMode)...)
	}
	// Running out of steps used to discard the entire delegation: several
	// rounds of reads and searches were paid for and then thrown away, and
	// the parent got an error instead of findings. Asking once more with the
	// tools removed leaves the model no way to answer except in prose, so
	// the work already done comes back as a report.
	req.Tools = nil
	req.Messages = append(req.Messages, llmadapter.Message{
		Role:    llmadapter.RoleUser,
		Content: "Step budget reached. Report what you found so far in prose, and say plainly what is still unverified. No tool calls.",
	})
	resp, err := e.completeMaybeRotate(ctx, a, credential, req)
	if err != nil {
		return "", spent, fmt.Errorf("subagent exceeded max steps and could not summarize: %w", err)
	}
	spent += int64(resp.Usage.TotalTokens)
	report := strings.TrimSpace(resp.Message.Content)
	if report == "" {
		return "", spent, errors.New("subagent exceeded max steps without a final report")
	}
	return report, spent, nil
}

// runSubagentToolCalls executes one step's tool calls and returns their tool
// messages in the original call order, which the tool protocol requires.
//
// A delegated investigation is mostly independent reads — three files, two
// greps — and running them one after another was the slowest part of every
// subagent turn while the main chat loop already overlapped the same calls.
// Eligibility reuses chat_parallel.go's verified read-only contract rather
// than inventing a second one; anything outside it still runs inline.
func (e *Engine) runSubagentToolCalls(ctx context.Context, sessionID string, profile subagentProfileDef, allowed map[string]bool, calls []llmadapter.ToolCall, parentMode executionMode) []llmadapter.Message {
	summaries := make([]string, len(calls))
	// Distinct indices, so the background writes below never overlap the
	// inline ones.
	spawned := make([]bool, len(calls))
	var wg sync.WaitGroup
	started := 0
	profileAllowed := toolNameSet(subagentEngineToolDefinitions(profile))
	for i, call := range calls {
		if !allowed[call.Name] || !profileAllowed[call.Name] || !subagentCallAllowed(profile, call) {
			summaries[i] = "refused: tool not allowed for profile " + profile.ID
			spawned[i] = true
			continue
		}
		if len(calls) < 2 || started >= maxParallelToolCalls || !parallelToolEligible(call.Name) {
			continue
		}
		started++
		spawned[i] = true
		wg.Add(1)
		go func(i int, call llmadapter.ToolCall) {
			defer wg.Done()
			summaries[i] = e.runSubagentTool(ctx, sessionID, call, parentMode)
		}(i, call)
	}
	for i, call := range calls {
		if !spawned[i] {
			summaries[i] = e.runSubagentTool(ctx, sessionID, call, parentMode)
		}
	}
	wg.Wait()
	out := make([]llmadapter.Message, len(calls))
	for i, call := range calls {
		summary := summaries[i]
		summary = truncateUTF8Bytes(summary, 4096)
		out[i] = llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: call.ID, Content: summary}
	}
	return out
}

// runSubagentTool executes one delegated call. approved stays false: nobody is
// watching a subagent, so an approval-mode parent has to see its writes
// refused rather than have them auto-approved on its behalf.
func (e *Engine) runSubagentTool(ctx context.Context, sessionID string, call llmadapter.ToolCall, parentMode executionMode) string {
	if reason, denied := ungatedEngineToolDenied(subagentToolMode(parentMode), false, call.Name, call.Arguments); denied {
		return reason
	}
	stage := "tool"
	if call.Name == "web.search" || call.Name == "web.fetch" {
		stage = "searching"
	}
	emitSubagentProgress(ctx, stage, call.Name, "正在执行")
	var r toolruntime.Result
	var err error
	if call.Name == "browser.act" {
		r, err = e.invokeBrowserAct(ctx, subagentToolMode(parentMode), sessionID, call.Arguments)
	} else {
		r, err = e.recordExistingToolCall(ctx, sessionID, call.Name, call.Arguments, func() (toolruntime.Result, error) {
			return e.tools.Execute(ctx, toolruntime.Mode(subagentToolMode(parentMode)), sessionID, call.Name, call.Arguments, false)
		})
	}
	if err != nil {
		emitSubagentProgress(ctx, "tool", call.Name, "工具执行失败，正在整理结果")
		return err.Error()
	}
	emitSubagentProgress(ctx, "tool", call.Name, "工具已完成")
	return r.Output
}

func toolNameSet(tools []llmadapter.ToolDefinition) map[string]bool {
	set := make(map[string]bool, len(tools))
	for _, d := range tools {
		set[d.Name] = true
	}
	return set
}

func subagentEngineToolDefinitions(profile subagentProfileDef) []llmadapter.ToolDefinition {
	tools := readOnlyEngineToolDefinitionsForProfile(profile)
	if len(profile.WriteTools) == 0 {
		return tools
	}
	have := toolNameSet(tools)
	allow := map[string]bool{}
	for _, name := range profile.WriteTools {
		allow[name] = true
	}
	for _, d := range engineToolDefinitions() {
		if have[d.Name] || !allow[d.Name] || expertWriteToolDenied(d.Name) {
			continue
		}
		tools = append(tools, d)
		have[d.Name] = true
	}
	return tools
}

func readOnlyEngineToolDefinitionsForProfile(profile subagentProfileDef) []llmadapter.ToolDefinition {
	allow := subagentReadTools(profile.ReadCaps)
	return filterToolDefs(engineToolDefinitions(), allow)
}

func filterToolDefs(all []llmadapter.ToolDefinition, allow map[string]bool) []llmadapter.ToolDefinition {
	out := make([]llmadapter.ToolDefinition, 0, len(allow))
	for _, d := range all {
		if allow[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

func readOnlyEngineToolDefinitions() []llmadapter.ToolDefinition {
	allow := subagentReadTools(fullSubagentReadCaps())
	delete(allow, "browser.act")
	return filterToolDefs(engineToolDefinitions(), allow)
}

type subagentFutureResult struct {
	summary string
	err     error
}

func startSubagentFutures(ctx context.Context, e *Engine, a llmadapter.Adapter, credential []byte, model, sessionID string, calls []llmadapter.ToolCall, policy subagentChatPolicy, observers ...func(callID string, progress subagentProgress)) map[string]chan subagentFutureResult {
	futures := make(map[string]chan subagentFutureResult)
	started := 0
	for _, call := range calls {
		if call.Name != "subagent.spawn" || call.ID == "" || futures[call.ID] != nil || started >= maxParallelSubagentSpawns {
			continue
		}
		started++
		ch := make(chan subagentFutureResult, 1)
		futures[call.ID] = ch
		go func(call llmadapter.ToolCall) {
			defer close(ch)
			childCtx := ctx
			if len(observers) > 0 && observers[0] != nil {
				childCtx = withSubagentObserver(ctx, func(update subagentProgress) { observers[0](call.ID, update) })
			}
			defer func() {
				if r := recover(); r != nil {
					ch <- subagentFutureResult{err: fmt.Errorf("subagent panicked: %v", r)}
				}
			}()
			summary, err := e.invokeSubagentTool(childCtx, a, credential, model, sessionID, call.Name, call.Arguments, policy)
			ch <- subagentFutureResult{summary: summary, err: err}
		}(call)
	}
	return futures
}

func (e *Engine) executeSubagentSafely(ctx context.Context, a llmadapter.Adapter, credential []byte, model, sessionID, purpose string, budget int64, profile subagentProfileDef, parentMode executionMode) (report string, spent int64, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("subagent execution panic: %v", recovered)
		}
	}()
	return e.executeSubagentLoop(ctx, a, credential, model, sessionID, purpose, budget, profile, parentMode)
}
