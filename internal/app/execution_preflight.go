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
	return agentrun.ExecutionBudgetPolicy{
		MaxTotalTokens:   int64ptr(1_000_000),
		MaxOutputTokens:  int64ptr(65_536),
		MaxModelAttempts: int64ptr(64),
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
	binder, hasBinder := a.budget.(executionScopeBinder)
	if a.budget != nil && hasBinder {
		if !looksLikeULID(sessionID) {
			return agentrun.ExecutionScope{}, ctx, rec, errExecutionUnbound
		}
		bound, err := binder.EnsureExecutionBinding(ctx, rec.OwnerScope, sessionID, sessionID, "task_root", sessionID, "", defaultChatExecutionPolicy())
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
		eff.ThinkingType = "disabled"
		prepared.Effective = eff
	}
	req.Effective = &eff
	req.Target = prepared.Target
	return prepared, req, nil
}

func conservativeInputTokens(body []byte, profile modelfit.ModelProfile) int64 {
	n := int64((len(body) + 3) / 4)
	if n < 1 {
		n = 1
	}
	if profile.ContextWindow > 0 && n > profile.ContextWindow {
		return profile.ContextWindow
	}
	return n
}
