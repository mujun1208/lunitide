// Chat-layer subagent delegation (P1-1): exposes subagent.spawn /
// subagent.join as model tools inside the chat gateway loop.
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
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const (
	subagentDefaultBudgetTokens = 8192
	subagentDeadlineMS          = 5 * 60 * 1000
	subagentMaxSteps            = 4
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

func (e *Engine) subagentToolDefinitions(mode executionMode, policy subagentChatPolicy) []gateway.ToolDefinition {
	if mode == executionModePlan || effectiveDelegationMode(policy) == delegationDisabled || e.m7subagent == nil {
		return nil
	}
	return []gateway.ToolDefinition{
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

func (e *Engine) invokeSubagentTool(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID, tool string, rawArgs json.RawMessage, policy subagentChatPolicy) (string, error) {
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
		subA, subCred, subModel := e.subagentAdapter(ctx, a, credential, model, ov)
		return e.runSubagentSession(ctx, subA, subCred, subModel, sessionID, p.Purpose, budget, profile)
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

func (e *Engine) subagentAdapter(ctx context.Context, parent gateway.Adapter, parentCred []byte, parentModel string, ov subagentProfileOverride) (gateway.Adapter, []byte, string) {
	if ov.ModelID == "" {
		return parent, parentCred, parentModel
	}
	if ov.ProviderID == "" || e.providers == nil {
		return parent, parentCred, ov.ModelID
	}
	item, err := e.providers.Get(ctx, ov.ProviderID)
	if err != nil {
		return parent, parentCred, ov.ModelID
	}
	var outAdapter gateway.Adapter
	var outCred []byte
	leaseErr := e.withProviderLease(ctx, item, secretlease.OperationChat, func(op context.Context, cred []byte) error {
		a, err := e.adapter(op, item)
		if err != nil {
			return err
		}
		outAdapter = a
		outCred = cred
		return nil
	})
	if leaseErr != nil || outAdapter == nil {
		return parent, parentCred, ov.ModelID
	}
	return outAdapter, outCred, ov.ModelID
}

func subagentStoredPurpose(profile subagentProfileDef, purpose string) string {
	label := strings.TrimSpace(profile.DisplayName)
	if label == "" {
		label = profile.ID
	}
	tagged := fmt.Sprintf("[%s] %s", label, purpose)
	if len(tagged) > m7flow.SubagentMaxPurpose {
		tagged = tagged[:m7flow.SubagentMaxPurpose]
	}
	return tagged
}

func (e *Engine) runSubagentSession(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID, purpose string, budget int64, profile subagentProfileDef) (string, error) {
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
	report, spent, execErr := e.executeSubagentLoop(ctx, a, credential, model, sessionID, purpose, budget, profile)
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
		if execErr == nil {
			return "", completeErr
		}
		log.Printf("subagent complete failed after execution error (quota may leak until deadline): %v", completeErr)
	}
	out, err := json.Marshal(map[string]any{
		"subagentId": run.ID, "status": "completed", "profile": profile.ID,
		"summary": report, "spentTokens": spent,
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (e *Engine) executeSubagentLoop(ctx context.Context, a gateway.Adapter, credential []byte, model, sessionID, purpose string, budget int64, profile subagentProfileDef) (string, int64, error) {
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
	maxSteps := profile.MaxSteps
	if maxSteps < 1 {
		maxSteps = subagentMaxSteps
	}
	tools := readOnlyEngineToolDefinitionsForProfile(profile)
	allowed := toolNameSet(tools)
	req := gateway.Request{
		Model: model, MaxTokens: maxTokens, MaxAttempts: 1,
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: prompt},
			{Role: gateway.RoleUser, Content: purpose},
		},
		Tools: tools,
	}
	var spent int64
	for step := 0; step < maxSteps; step++ {
		resp, err := a.Complete(ctx, credential, req)
		if err != nil {
			return "", spent, err
		}
		spent += int64(resp.Usage.TotalTokens)
		if len(resp.Message.ToolCalls) == 0 {
			return strings.TrimSpace(resp.Message.Content), spent, nil
		}
		req.Messages = append(req.Messages, resp.Message)
		for _, call := range resp.Message.ToolCalls {
			summary := ""
			if !allowed[call.Name] {
				summary = "refused: tool not allowed for profile " + profile.ID
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
	return "", spent, errors.New("subagent exceeded max steps without a final report")
}

func toolNameSet(tools []gateway.ToolDefinition) map[string]bool {
	set := make(map[string]bool, len(tools))
	for _, d := range tools {
		set[d.Name] = true
	}
	return set
}

func readOnlyEngineToolDefinitionsForProfile(profile subagentProfileDef) []gateway.ToolDefinition {
	all := readOnlyEngineToolDefinitions()
	switch profile.ID {
	case "research":
		return filterToolDefs(all, map[string]bool{"web.search": true, "web.fetch": true})
	case "browser":
		defs := filterToolDefs(all, map[string]bool{"web.search": true, "web.fetch": true})
		for _, d := range engineToolDefinitions() {
			if d.Name == "browser.act" {
				defs = append(defs, d)
				break
			}
		}
		return defs
	case "shell":
		return filterToolDefs(all, map[string]bool{"command.run": true, "workspace.list": true, "workspace.read": true, "workspace.search": true})
	case "explore", "review", "test":
		return filterToolDefs(all, workspaceReadTools())
	}
	return all
}

func workspaceReadTools() map[string]bool {
	return map[string]bool{
		"workspace.list": true, "workspace.read": true, "workspace.search": true, "command.run": true,
	}
}

func filterToolDefs(all []gateway.ToolDefinition, allow map[string]bool) []gateway.ToolDefinition {
	out := make([]gateway.ToolDefinition, 0, len(allow))
	for _, d := range all {
		if allow[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

func readOnlyEngineToolDefinitions() []gateway.ToolDefinition {
	all := engineToolDefinitions()
	out := make([]gateway.ToolDefinition, 0, len(all))
	for _, d := range all {
		if d.Name == "workspace.write" || d.Name == "workspace.edit" || d.Name == "html.gen" || d.Name == "desktop.open" || d.Name == "media.play" || d.Name == "browser.act" {
			continue
		}
		out = append(out, d)
	}
	return out
}

var subagentAllowedTools = toolNameSet(readOnlyEngineToolDefinitions())

type subagentFutureResult struct {
	summary string
	err     error
}

func startSubagentFutures(ctx context.Context, e *Engine, a gateway.Adapter, credential []byte, model, sessionID string, calls []gateway.ToolCall, policy subagentChatPolicy) map[string]chan subagentFutureResult {
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
			summary, err := e.invokeSubagentTool(ctx, a, credential, model, sessionID, call.Name, call.Arguments, policy)
			ch <- subagentFutureResult{summary: summary, err: err}
		}(call)
	}
	return futures
}
