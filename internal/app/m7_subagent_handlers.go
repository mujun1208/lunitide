package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-6 handlers (T-7.6.x): subagent.spawn / subagent.join /
// subagent.tree. The legacy plan.run.spawn/join/tree aliases stay on the
// adapter layer only - they never enter the schema registry.
//
// Error mapping follows the M7 wire contract: purpose guards answer
// M7-SAG-001 (422), read-cap whitelist violations answer M7-SAG-002 (403),
// concurrency/budget/deadline guards answer M7-SAG-003 (429), join TOCTOU
// drift answers M7-SAG-004 (409) and deadline termination answers
// M7-SAG-005 (504).

func handleSubagentSpawn(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootRunID    string   `json:"rootRunId"`
		StageRunID   string   `json:"stageRunId"`
		Purpose      string   `json:"purpose"`
		ReadCaps     []string `json:"readCaps"`
		PersonaRef   string   `json:"personaDigest"`
		BudgetTokens int64    `json:"budgetTokens"`
		DeadlineMS   int64    `json:"deadlineMs"`
		RequestID    string   `json:"requestId"`
		Actor        string   `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RootRunID) < 1 || len(p.RootRunID) > 128 ||
		len(p.StageRunID) > 128 || len(p.Purpose) < 1 || len(p.Purpose) > m7flow.SubagentMaxPurpose ||
		len(p.ReadCaps) < 1 || len(p.RequestID) < 1 || len(p.RequestID) > 128 || len(p.Actor) > 128 ||
		(p.PersonaRef != "" && len(p.PersonaRef) != 64) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "subagent.spawn 参数无效", false)
	}
	if e.m7subagent == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "子代理服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	run, err := e.m7subagent.Spawn(ctx, m7app.SpawnInput{
		RootRunID:      p.RootRunID,
		StageRunID:     p.StageRunID,
		Purpose:        p.Purpose,
		ReadCaps:       p.ReadCaps,
		PersonaDigest:  p.PersonaRef,
		BudgetTokens:   p.BudgetTokens,
		DeadlineMS:     p.DeadlineMS,
		IdempotencyKey: p.RequestID,
		Actor:          p.Actor,
	})
	if err != nil {
		return m7SubagentFailure(r, err, "subagent.spawn")
	}
	return bridge.Success(r.ID, struct {
		SubagentID       string `json:"subagentId"`
		Status           string `json:"status"`
		CapabilityDigest string `json:"capabilityDigest"`
	}{run.ID, run.Status, run.CapabilityDigest})
}

func handleSubagentJoin(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubagentID              string `json:"subagentId"`
		WaitMS                  int64  `json:"waitMs"`
		ExpectedCapabilityDigest string `json:"expectedCapabilityDigest"`
		ExpectedPolicyVersion   string `json:"expectedPolicyVersion"`
		ExpectedPersonaDigest   string `json:"expectedPersonaDigest"`
		MaxSummaryBytes         int64  `json:"maxSummaryBytes"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SubagentID) ||
		p.WaitMS < 1 || p.WaitMS > 30000 ||
		(p.ExpectedCapabilityDigest != "" && len(p.ExpectedCapabilityDigest) != 64) ||
		(p.ExpectedPersonaDigest != "" && len(p.ExpectedPersonaDigest) != 64) ||
		p.MaxSummaryBytes < 0 || p.MaxSummaryBytes > m7flow.SubagentMaxSummary {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "subagent.join 参数无效", false)
	}
	if e.m7subagent == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "子代理服务暂时不可用", true)
	}
	res, err := e.m7subagent.Join(ctx, m7app.JoinInput{
		SubagentRunID:            p.SubagentID,
		ExpectedCapabilityDigest: p.ExpectedCapabilityDigest,
		ExpectedPolicyVersion:    p.ExpectedPolicyVersion,
		ExpectedPersonaDigest:    p.ExpectedPersonaDigest,
		MaxSummaryBytes:          int(p.MaxSummaryBytes),
	})
	if err != nil {
		return m7SubagentFailure(r, err, "subagent.join")
	}
	digests := res.Digests
	if digests == nil {
		digests = []string{}
	}
	return bridge.Success(r.ID, struct {
		Status      string   `json:"status"`
		Summary     string   `json:"summary,omitempty"`
		Digests     []string `json:"digests"`
		Truncated   bool     `json:"truncated"`
		SpentTokens int64    `json:"spentTokens"`
	}{res.State, res.Summary, digests, res.Truncated, res.SpentTokens})
}

func handleSubagentTree(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootRunID string `json:"rootRunId"`
		Cursor    string `json:"cursor"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RootRunID) < 1 || len(p.RootRunID) > 128 ||
		len(p.Cursor) > 128 || p.Limit < 0 || p.Limit > 100 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "subagent.tree 参数无效", false)
	}
	if e.m7subagent == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "子代理服务暂时不可用", true)
	}
	runs, next, err := e.m7subagent.Tree(ctx, p.RootRunID, p.Cursor, p.Limit)
	if err != nil {
		return m7SubagentFailure(r, err, "subagent.tree")
	}
	items := make([]m7SubagentTreeItem, 0, len(runs))
	for _, run := range runs {
		obsCount := 0
		if obs, err := e.m7subagent.ListObservations(ctx, run.ID); err == nil {
			obsCount = len(obs)
		}
		items = append(items, m7SubagentTreeItem{
			ID: run.ID, Purpose: run.Purpose, Status: run.Status,
			SpentTokens: run.SpentTokens, ObservationCount: obsCount,
		})
	}
	return bridge.Success(r.ID, struct {
		Subagents  []m7SubagentTreeItem `json:"subagents"`
		NextCursor string               `json:"nextCursor,omitempty"`
	}{items, next})
}

// m7SubagentTreeItem is one row of the subagent.tree projection.
type m7SubagentTreeItem struct {
	ID               string `json:"id"`
	Purpose          string `json:"purpose"`
	Status           string `json:"status"`
	SpentTokens      int64  `json:"spentTokens"`
	ObservationCount int    `json:"observationCount"`
}

// m7SubagentFailure maps m7app slice-6 errors onto the M7 wire family.
func m7SubagentFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrSubagentPurpose):
		return bridge.Failure(r.ID, r.TraceID, "M7-SAG-001", "purpose 缺失或超过 2000 字符", false)
	case errors.Is(err, m7app.ErrSubagentCaps):
		return bridge.Failure(r.ID, r.TraceID, "M7-SAG-002", "readCaps 含白名单外或写类能力，已拒绝并审计", false)
	case errors.Is(err, m7app.ErrSubagentQuota):
		return bridge.Failure(r.ID, r.TraceID, "M7-SAG-003", "并发、预算或期限配额超限", false)
	case errors.Is(err, m7app.ErrSubagentJoinStale):
		return bridge.Failure(r.ID, r.TraceID, "M7-SAG-004", "join 目标不存在、已终态或摘要漂移（TOCTOU）", false)
	case errors.Is(err, m7app.ErrSubagentDeadline):
		return bridge.Failure(r.ID, r.TraceID, "M7-SAG-005", "子代理超过 deadline 已终止并标记 partial", false)
	case errors.Is(err, m7app.ErrSubagentNotFound):
		return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "子代理不存在", false)
	case errors.Is(err, m7app.ErrSubagentTransition):
		return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", "子代理状态迁移非法", false)
	case errors.Is(err, m7app.ErrServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "子代理服务暂时不可用", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", method+" 执行失败", false)
}
