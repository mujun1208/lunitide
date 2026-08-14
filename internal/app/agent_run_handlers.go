package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

const agentRunMutationActor = "desktop-host"

type agentRunBudgetDTO struct {
	MaxModelTurns       int64 `json:"maxModelTurns"`
	MaxToolCalls        int64 `json:"maxToolCalls"`
	MaxTokens           int64 `json:"maxTokens"`
	MaxCostMicros       int64 `json:"maxCostMicros"`
	MaxWallClockSeconds int64 `json:"maxWallClockSeconds"`
	MaxOutputBytes      int64 `json:"maxOutputBytes"`
	MaxRetries          int64 `json:"maxRetries"`
	MaxNoProgress       int64 `json:"maxNoProgress"`
	HardCeiling         bool  `json:"hardCeiling"`
}

type agentRunUsageDTO struct {
	ModelTurns       int64 `json:"modelTurns"`
	ToolCalls        int64 `json:"toolCalls"`
	Tokens           int64 `json:"tokens"`
	CostMicros       int64 `json:"costMicros"`
	WallClockSeconds int64 `json:"wallClockSeconds"`
	OutputBytes      int64 `json:"outputBytes"`
	Retries          int64 `json:"retries"`
	NoProgress       int64 `json:"noProgress"`
}

type agentRunDTO struct {
	ID        string             `json:"id"`
	SessionID string             `json:"sessionId"`
	Status    agentrun.RunStatus `json:"status"`
	Budget    agentRunBudgetDTO  `json:"budget"`
	Used      agentRunUsageDTO   `json:"used"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
	Version   int64              `json:"version"`
}

func newAgentRunDTO(r agentrun.AgentRun) agentRunDTO {
	return agentRunDTO{
		ID:        r.ID,
		SessionID: r.SessionID,
		Status:    r.Status,
		Budget: agentRunBudgetDTO{
			MaxModelTurns:       r.Budget.MaxModelTurns,
			MaxToolCalls:        r.Budget.MaxToolCalls,
			MaxTokens:           r.Budget.MaxTokens,
			MaxCostMicros:       r.Budget.MaxCostMicros,
			MaxWallClockSeconds: r.Budget.MaxWallClockSeconds,
			MaxOutputBytes:      r.Budget.MaxOutputBytes,
			MaxRetries:          r.Budget.MaxRetries,
			MaxNoProgress:       r.Budget.MaxNoProgress,
			HardCeiling:         r.Budget.HardCeiling,
		},
		Used: agentRunUsageDTO{
			ModelTurns:       r.Used.ModelTurns,
			ToolCalls:        r.Used.ToolCalls,
			Tokens:           r.Used.Tokens,
			CostMicros:       r.Used.CostMicros,
			WallClockSeconds: r.Used.WallClockSeconds,
			OutputBytes:      r.Used.OutputBytes,
			Retries:          r.Used.Retries,
			NoProgress:       r.Used.NoProgress,
		},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		Version:   r.Version,
	}
}

func agentRunFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agentrunapp.ErrIdempotencyKeyRequired):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, agentrunapp.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, agentrun.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "AGENT_RUN_NOT_FOUND", "Agent 运行不存在", false)
	case errors.Is(err, agentrun.ErrVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "RUN_VERSION_CONFLICT", "Agent 运行版本冲突", false)
	case errors.Is(err, agentrun.ErrTerminal):
		return bridge.Failure(r.ID, r.TraceID, "AGENT_RUN_TERMINAL", "Agent 运行已终结", false)
	case errors.Is(err, agentrun.ErrInvalidTransition):
		return bridge.Failure(r.ID, r.TraceID, "AGENT_RUN_TRANSITION_INVALID", "Agent 运行状态迁移非法", false)
	case errors.Is(err, agentrun.ErrInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "Agent 运行参数无效", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
}

func handleCapabilityList(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if !emptyObject(r.Payload) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "capability.list 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	return bridge.Success(r.ID, struct {
		Manifest agentrunapp.CapabilityManifest `json:"manifest"`
	}{e.agentRuns.Capability()})
}

func handleAgentRunStart(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string            `json:"sessionId"`
		Budget    agentRunBudgetDTO `json:"budget"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "agent.run.start 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	budget := agentrun.Budget{
		MaxModelTurns:       p.Budget.MaxModelTurns,
		MaxToolCalls:        p.Budget.MaxToolCalls,
		MaxTokens:           p.Budget.MaxTokens,
		MaxCostMicros:       p.Budget.MaxCostMicros,
		MaxWallClockSeconds: p.Budget.MaxWallClockSeconds,
		MaxOutputBytes:      p.Budget.MaxOutputBytes,
		MaxRetries:          p.Budget.MaxRetries,
		MaxNoProgress:       p.Budget.MaxNoProgress,
		HardCeiling:         p.Budget.HardCeiling,
	}
	run, err := e.agentRuns.Start(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.SessionID, budget)
	if err != nil {
		return agentRunFailure(r, err)
	}
	return bridge.Success(r.ID, newAgentRunDTO(run))
}

func handleAgentRunGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID string `json:"runId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "agent.run.get 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	run, err := e.agentRuns.Get(ctx, p.RunID)
	if err != nil {
		return agentRunFailure(r, err)
	}
	return bridge.Success(r.ID, newAgentRunDTO(run))
}

type agentRunMutationPayload struct {
	RunID           string             `json:"runId"`
	ExpectedVersion int64              `json:"expectedVersion"`
	Budget          *agentRunBudgetDTO `json:"budget,omitempty"`
}

func handleAgentRunResume(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return agentRunTransition(e, ctx, r, "agent.run.resume", func(s *agentrunapp.Service, key string, p agentRunMutationPayload) (agentrun.AgentRun, error) {
		if p.Budget == nil {
			return agentrun.AgentRun{}, agentrun.ErrInvalid
		}
		b := agentrun.Budget{
			MaxModelTurns:       p.Budget.MaxModelTurns,
			MaxToolCalls:        p.Budget.MaxToolCalls,
			MaxTokens:           p.Budget.MaxTokens,
			MaxCostMicros:       p.Budget.MaxCostMicros,
			MaxWallClockSeconds: p.Budget.MaxWallClockSeconds,
			MaxOutputBytes:      p.Budget.MaxOutputBytes,
			MaxRetries:          p.Budget.MaxRetries,
			MaxNoProgress:       p.Budget.MaxNoProgress,
			HardCeiling:         p.Budget.HardCeiling,
		}
		return s.ResumeWithBudget(ctx, key, agentRunMutationActor, p, p.RunID, p.ExpectedVersion, b)
	})
}

func handleAgentRunCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return agentRunTransition(e, ctx, r, "agent.run.cancel", func(s *agentrunapp.Service, key string, p agentRunMutationPayload) (agentrun.AgentRun, error) {
		return s.Cancel(ctx, key, agentRunMutationActor, p, p.RunID, p.ExpectedVersion)
	})
}

func agentRunTransition(e *Engine, ctx context.Context, r bridge.Request, method string, fn func(*agentrunapp.Service, string, agentRunMutationPayload) (agentrun.AgentRun, error)) bridge.Response {
	var p agentRunMutationPayload
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", method+" 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	run, err := fn(e.agentRuns, r.IdempotencyKey, p)
	if err != nil {
		return agentRunFailure(r, err)
	}
	return bridge.Success(r.ID, newAgentRunDTO(run))
}

func handleAgentRunReconcile(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p agentRunMutationPayload
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || p.ExpectedVersion < 1 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "agent.run.reconcile 参数无效", false)
	}
	if e.agentRuns == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.Reconcile(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.RunID, p.ExpectedVersion)
	if err != nil {
		return agentRunFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Run               agentRunDTO `json:"run"`
		ReconciledEffects int         `json:"reconciledEffects"`
	}{newAgentRunDTO(result.Run), result.ReconciledEffects})
}
