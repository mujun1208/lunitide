package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// M8 slice-1 handlers (T-8.1.x): memory.confirmCandidate / recall.query.
//
// Error mapping follows the M8 wire contract (04 错误矩阵): candidate
// guards answer M8-001/002/003/004/005/006/007/008, recall guards answer
// M8-009/010 and the global fail-closed policy guard answers M8-027.

// m8ConfirmFactRef mirrors the x-result fact block.
type m8ConfirmFactRef struct {
	FactID  string `json:"factId"`
	Version int64  `json:"version"`
}

func handleMemoryConfirmCandidate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		CandidateID string             `json:"candidateId"`
		Token       string             `json:"confirmationToken"`
		Action      string             `json:"action"`
		EditedDoc   *m8core.PayloadDoc `json:"editedPayload"`
		RequestID   string             `json:"requestId"`
		Actor       string             `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.CandidateID) ||
		len(p.Token) != m8core.DigestHexLen || !m8core.ValidHexDigest(p.Token) ||
		(p.Action != "confirm" && p.Action != "reject") ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 || len(p.Actor) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.confirmCandidate 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	var edited *m8core.PayloadDoc
	if p.EditedDoc != nil {
		edited = p.EditedDoc
	}
	res, err := e.m8memory.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: p.CandidateID,
		Token:       p.Token,
		Action:      p.Action,
		EditedDoc:   edited,
		RequestID:   p.RequestID,
		Actor:       p.Actor,
	})
	if err != nil {
		return m8MemoryFailure(r, err)
	}
	out := struct {
		CandidateID string            `json:"candidateId"`
		State       string            `json:"state"`
		Fact        *m8ConfirmFactRef `json:"fact,omitempty"`
	}{CandidateID: res.CandidateID, State: res.State}
	if res.Fact != nil {
		out.Fact = &m8ConfirmFactRef{FactID: res.Fact.FactID, Version: res.Fact.Version}
	}
	// M10 handler-side settlement: the settled candidate closes its nomination
	// (if any) as decided. Plain candidates never had a nomination row and the
	// call is silently ignored — the 0061 service itself stays zero-touch.
	if e.m10nomination != nil {
		_ = e.m10nomination.MarkDecided(ctx, res.CandidateID)
	}
	return r.Ok(out)
}

func handleRecallQuery(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeID      string `json:"scopeId"`
		Query        string `json:"query"`
		TopK         int    `json:"topK"`
		IndexVersion string `json:"indexVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ScopeID) < 1 || len(p.ScopeID) > m8core.MaxScopeID ||
		len(p.Query) < 1 || len(p.Query) > 2048 || p.TopK < 0 || p.TopK > m8core.RecallMaxTopK ||
		len(p.IndexVersion) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "recall.query 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	res, err := e.m8memory.Recall(ctx, m8app.RecallInput{
		ScopeID:      p.ScopeID,
		Query:        p.Query,
		TopK:         p.TopK,
		IndexVersion: p.IndexVersion,
	})
	if err != nil {
		return m8MemoryFailure(r, err)
	}
	return r.Ok(res)
}

// m8MemoryFailure maps m8app slice-1 errors onto the M8 code family.
func m8MemoryFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrCandidateNotFound):
		return r.Fail("M8-001", "候选不存在", false)
	case errors.Is(err, m8app.ErrCandidateExpired):
		return r.Fail("M8-002", "候选已过期", false)
	case errors.Is(err, m8app.ErrExplicitConfirmationRequired):
		return r.Fail("M8-003", "需要显式确认", false)
	case errors.Is(err, m8app.ErrConfirmTokenInvalid):
		return r.Fail("M8-004", "确认令牌无效或已使用", false)
	case errors.Is(err, m8app.ErrPayloadDigestMismatch):
		return r.Fail("M8-005", "载荷被修改", false)
	case errors.Is(err, m8app.ErrInferencePromotionDenied):
		return r.Fail("M8-006", "禁止推断自动晋升", false)
	case errors.Is(err, m8app.ErrSourceLeafRequired):
		return r.Fail("M8-007", "缺少叶级来源", false)
	case errors.Is(err, m8app.ErrSourceEvidenceUnavailable):
		return r.Fail("M8-008", "证据不可验证", false)
	case errors.Is(err, m8app.ErrRecallScopeDenied):
		return r.Fail("M8-009", "召回范围拒绝", false)
	case errors.Is(err, m8app.ErrExplanationUnavailable):
		return r.Fail("M8-010", "解释不可用", true)
	case errors.Is(err, m8app.ErrPolicyUnavailable):
		return r.Fail("M8-027", "策略不可用，安全关闭", true)
	case errors.Is(err, m8app.ErrPayloadInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "记忆载荷非法", false)
	case errors.Is(err, m8app.ErrServiceUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	return r.Fail("INTERNAL_ERROR", "记忆内核执行失败", false)
}
