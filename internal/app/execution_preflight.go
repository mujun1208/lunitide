package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

var errExecutionUnbound = errors.New("execution budget unbound")

type executionScopeBinder interface {
	EnsureExecutionBinding(ctx context.Context, ownerScope, sessionID, taskID, runKind, executionRef, parentScopeID string, policy agentrun.ExecutionBudgetPolicy) (agentrun.ExecutionScope, error)
}

type dedicatedExecutionBinder interface {
	EnsureDedicatedExecutionBinding(ctx context.Context, ownerScope, taskID, executionRef string, policy agentrun.ExecutionBudgetPolicy) (agentrun.ExecutionScope, error)
}

func AttachProductionExecutionBudget(e *Engine, uow agentrunapp.ExecutionBudgetUoW) {
	if e == nil || uow == nil {
		return
	}
	e.SetExecutionBudget(agentrunapp.NewExecutionBudget(uow))
}

func defaultChatExecutionPolicy() agentrun.ExecutionBudgetPolicy {
	// One ledger per typed user turn. The turn already stops at
	// maxToolLoopStepsHard model steps. This ceiling sits above that loop
	// when every step spends a full chatMaxTokens reply plus one thinking
	// window, with room for nested calls (council, subagent). It is a runaway
	// backstop. Voice does not use it.
	const thinkingHeadroom int64 = 65_536
	const nestedCalls int64 = 2
	calls := int64(maxToolLoopStepsHard) * nestedCalls
	ceiling := calls * (int64(chatMaxTokens) + thinkingHeadroom)
	return agentrun.ExecutionBudgetPolicy{
		MaxTotalTokens:   int64ptr(ceiling),
		MaxOutputTokens:  int64ptr(ceiling),
		MaxModelAttempts: int64ptr(calls),
		MaxActiveMillis:  int64ptr(1_800_000),
		MaxOutputBytes:   int64ptr(2 << 20),
	}
}

func int64ptr(v int64) *int64 { return &v }

func dedicatedExecutionTaskID(scope continuityScope) (string, bool) {
	key := strings.TrimSpace(scope.Task)
	if key == "" || key == "diagnostic" {
		key = strings.TrimSpace(scope.Owner)
	}
	if isDedicatedExecutionTask(key) {
		return key, true
	}
	if scope.Purpose == "meeting" {
		return "meeting", true
	}
	if scope.Purpose == "expert" {
		return "expert", true
	}
	if scope.Purpose == "diagnostic" {
		return "diagnostic", true
	}
	return "", false
}

func isDedicatedExecutionTask(taskID string) bool {
	return taskID == "meeting" || taskID == "expert" || strings.HasPrefix(taskID, "meeting:") || strings.HasPrefix(taskID, "expert:")
}

// executionBudgetExempt keeps two surfaces off the runaway ledger.
// Voice is one lifelong dialogue: a quota there becomes a permanent refusal,
// and the spoken turn already has its own step cap and a short context window.
// The flag sticks across nested calls (council, desktop verify) that replace
// Purpose. Compaction is housekeeping on the same session and would otherwise
// fill a ledger that never resets.
func executionBudgetExempt(scope continuityScope) bool {
	if scope.SkipExecutionBudget {
		return true
	}
	switch strings.TrimSpace(scope.Purpose) {
	case "companion", "compaction":
		return true
	}
	return false
}

// chatWorkUnitTaskID names the ledger. A named turn shares one allowance
// across every model call in that turn. A call with no turn is its own unit,
// so a long-lived session cannot accumulate into a permanent refusal.
func chatWorkUnitTaskID(scope continuityScope, _ string) string {
	if looksLikeULID(scope.Turn) {
		return scope.Turn
	}
	return ulid.Make().String()
}

func (a meteredAdapter) bindExecutionScope(ctx context.Context, rec sqlite.CallAttemptRecord) (agentrun.ExecutionScope, context.Context, sqlite.CallAttemptRecord, error) {
	scope := continuityScopeFrom(ctx)
	if key, ok := dedicatedExecutionTaskID(scope); ok {
		if dedicated, ok := a.budget.(dedicatedExecutionBinder); ok {
			bound, err := dedicated.EnsureDedicatedExecutionBinding(ctx, rec.OwnerScope, key, key, defaultChatExecutionPolicy())
			if err != nil {
				return agentrun.ExecutionScope{}, ctx, rec, err
			}
			rec.TaskID = clipMeterID(bound.TaskID)
			return bound, ctx, rec, nil
		}
		execScope := agentrun.ExecutionScope{TaskID: key, RunID: key, ScopeID: key}
		rec.TaskID = clipMeterID(key)
		return execScope, ctx, rec, nil
	}
	sessionID := scope.Task
	if !looksLikeULID(sessionID) {
		sessionID = scope.Owner
	}
	taskID := chatWorkUnitTaskID(scope, sessionID)
	binder, hasBinder := a.budget.(executionScopeBinder)
	if a.budget != nil && hasBinder {
		if !looksLikeULID(sessionID) {
			return agentrun.ExecutionScope{}, ctx, rec, errExecutionUnbound
		}
		bound, err := binder.EnsureExecutionBinding(ctx, rec.OwnerScope, sessionID, taskID, "task_root", taskID, "", defaultChatExecutionPolicy())
		if err != nil {
			return agentrun.ExecutionScope{}, ctx, rec, err
		}
		scope.Task = bound.TaskID
		ctx = withContinuityScope(ctx, scope)
		rec.TaskID = clipMeterID(bound.TaskID)
		return bound, ctx, rec, nil
	}
	execScope := agentrun.ExecutionScope{TaskID: rec.TaskID, RunID: rec.TaskID, ScopeID: rec.TaskID}
	if execScope.TaskID == "" {
		execScope.TaskID = rec.OwnerScope
		execScope.RunID = rec.OwnerScope
		execScope.ScopeID = rec.OwnerScope
	}
	return execScope, ctx, rec, nil
}

func defaultPreflightProfile() modelfit.ModelProfile {
	return modelfit.ModelProfile{
		ProfileID:       "app-preflight-default",
		Family:          "openai",
		Protocol:        "openai_compatible",
		ContextWindow:   128000,
		MaxOutputTokens: 32768,
		Modes: map[string]modelfit.ModeParameters{
			"quick":    {ThinkingType: "omitted", Effort: "omitted"},
			"standard": {ThinkingType: "omitted", Effort: "omitted"},
			"deep":     {ThinkingType: "enabled", Effort: "high"},
		},
	}
}

func compileProfileOrDefault(p modelfit.ModelProfile) modelfit.ModelProfile {
	if len(p.Modes) == 0 {
		return defaultPreflightProfile()
	}
	return p
}

func normalizeReasoningLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "high", "max":
		return strings.ToLower(strings.TrimSpace(raw))
	case "medium":
		// GLM-5.3 folds medium into high. Keep the wire value on a real effort.
		return "high"
	default:
		return ""
	}
}

// applyUserReasoningLevel is the typed-chat intensity slider. It runs after
// the lane's DisableReasoning default so 极高 still sends reasoning_effort=max.
func applyUserReasoningLevel(eff *modelfit.EffectiveParameters, model string, profile modelfit.ModelProfile, level string) {
	switch level {
	case "low":
		if thinkingDisableUnsupported(model, profile) {
			eff.ThinkingType = "enabled"
			eff.Effort = "low"
			return
		}
		eff.ThinkingType = "disabled"
		eff.Effort = "low"
	case "high":
		eff.ThinkingType = "enabled"
		eff.Effort = "high"
	case "max":
		eff.ThinkingType = "enabled"
		eff.Effort = "max"
	}
}

func thinkingDisableUnsupported(model string, profile modelfit.ModelProfile) bool {
	if strings.Contains(strings.ToLower(model), "glm-5.3") {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(profile.Family), "glm") {
		return false
	}
	sawEnabled := false
	for _, mode := range profile.Modes {
		switch strings.ToLower(strings.TrimSpace(mode.ThinkingType)) {
		case "disabled":
			return false
		case "enabled":
			sawEnabled = true
		}
	}
	return sawEnabled
}

func compileFinalInput(req llmadapter.Request, profile modelfit.ModelProfile, stream bool) (llmadapter.PreparedRequest, llmadapter.Request, error) {
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = "standard"
	}
	profile = compileProfileOrDefault(profile)
	target := req.Target
	if target.ModelRequested == "" {
		target.ModelRequested = req.Model
		target.Family = profile.Family
		target.Protocol = profile.Protocol
		target.EndpointPurpose = profile.EndpointPurpose
	}
	prepared, err := llmadapter.PrepareChat(req, target, modelfit.ReplayDecision{}, profile, stream)
	if err != nil {
		return llmadapter.PreparedRequest{}, req, err
	}
	eff := prepared.Effective
	if req.DisableReasoning {
		if thinkingDisableUnsupported(req.Model, profile) {
			// GLM-5.3 rejects thinking.type=disabled. Map "don't think hard"
			// onto the vendor's minimum: enabled + reasoning_effort=low.
			eff.ThinkingType = "enabled"
			eff.Effort = "low"
		} else {
			eff.ThinkingType = "disabled"
		}
		prepared.Effective = eff
	}
	if level := normalizeReasoningLevel(req.ReasoningLevel); level != "" {
		applyUserReasoningLevel(&eff, req.Model, profile, level)
		prepared.Effective = eff
	}
	req.Effective = &eff
	req.Target = prepared.Target
	return prepared, req, nil
}

func conservativeInputTokens(body []byte, _ modelfit.ModelProfile) int64 {
	n := int64((len(body) + 3) / 4)
	if n < 1 {
		n = 1
	}
	return n
}
