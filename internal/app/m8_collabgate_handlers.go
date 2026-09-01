// M8 FR-17 Bridge handlers (G1/G2): collabGate.evaluate / status /
// confirm. The three methods are the frozen Memory API surface of the
// write-collaboration gate; every failure keeps the capability disabled
// with zero side effects.
package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
)

func handleCollabGateEvaluate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectID       string `json:"subjectId"`
		WindowStart     int64  `json:"windowStart"`
		WindowEnd       int64  `json:"windowEnd"`
		CriteriaVersion string `json:"criteriaVersion"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "collabGate.evaluate 参数无效", false)
	}
	if e.m8gate == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "写协作门禁服务暂时不可用", true)
	}
	res, err := e.m8gate.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: p.SubjectID, WindowStart: p.WindowStart, WindowEnd: p.WindowEnd,
		CriteriaVersion: p.CriteriaVersion,
	})
	if err != nil {
		return m8CollabGateFailure(r, err)
	}
	return r.Ok(res)
}

func handleCollabGateStatus(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectID string `json:"subjectId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "collabGate.status 参数无效", false)
	}
	if e.m8gate == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "写协作门禁服务暂时不可用", true)
	}
	res, err := e.m8gate.Status(ctx, m8app.StatusInput{SubjectID: p.SubjectID})
	if err != nil {
		return m8CollabGateFailure(r, err)
	}
	return r.Ok(res)
}

func handleCollabGateConfirm(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		DecisionID    string `json:"decisionId"`
		DecisionToken string `json:"decisionToken"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "collabGate.confirm 参数无效", false)
	}
	if e.m8gate == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "写协作门禁服务暂时不可用", true)
	}
	res, err := e.m8gate.Confirm(ctx, m8app.GateConfirmInput{
		DecisionID: p.DecisionID, DecisionToken: p.DecisionToken,
	})
	if err != nil {
		return m8CollabGateFailure(r, err)
	}
	return r.Ok(res)
}

// m8CollabGateFailure maps the FR-17 error family onto the M8 code matrix
// (M8-028/031/032/033 plus the shared family; the M8-034 WORM guard and
// M8-029/030 outcome codes surface via the evaluate result body).
func m8CollabGateFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m8app.ErrGateDisabled):
		return r.Fail("M8-028", "写协作能力未启用，操作被拒绝", false)
	case errors.Is(err, m8app.ErrDecisionTokenInvalid):
		return r.Fail("M8-031", "决策令牌无效、已过期或已重放", false)
	case errors.Is(err, m8app.ErrGateBindingDrift):
		return r.Fail("M8-032", "能力绑定已漂移，写协作已回退为停用", false)
	case errors.Is(err, m8app.ErrEvidenceUnavailable):
		return r.Fail("M8-033", "评估证据源不可用，评估已拒绝（fail-closed）", true)
	case errors.Is(err, m8app.ErrDecisionNotFound):
		return r.Fail("BRIDGE_NOT_FOUND", "决策记录不存在", false)
	case errors.Is(err, m8app.ErrPayloadInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "载荷非法", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "写协作门禁服务暂时不可用", true)
	}
}
