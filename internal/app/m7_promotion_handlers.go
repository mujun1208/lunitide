package app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-4 handlers (T-7.4.x): release.promote / release.rollback /
// release.getPromotion. The migration and deployment adapter ports stay
// internal to the Promotion aggregate - they are never registered as bridge
// methods (M7-MIG-001).
//
// Error mapping follows the M7 wire contract: a changed canonical intent
// freezes execution via M7-PRM-002, window/edge/SoD policy rejections answer
// M7-PRM-003, migration failures answer M7-MIG-002 (rollback follows),
// dispatch/health failures answer M7-DEP-001 (auto rollback per policy) and
// failed rollbacks freeze the environment via M7-RBK-001.

func handleReleasePromote(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PackageID     string         `json:"packageId"`
		TargetEnv     string         `json:"targetEnv"`
		PolicyContext map[string]any `json:"policyContext"`
		RequestID     string         `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PackageID) ||
		m7flow.EnvRank(p.TargetEnv) < 1 ||
		len(p.PolicyContext) < 1 || len(p.PolicyContext) > 64 ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "release.promote 参数无效", false)
	}
	if e.m7promotion == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "晋级服务暂时不可用", true)
	}
	// M7-DR-001: a broken audit ledger freezes production promotions.
	if p.TargetEnv == "prod" {
		if frozen := m7AuditGuard(e, ctx, r); frozen != nil {
			return *frozen
		}
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	prm, err := e.m7promotion.Promote(ctx, m7app.PromoteInput{
		PackageID:     p.PackageID,
		TargetEnv:     p.TargetEnv,
		PolicyContext: p.PolicyContext,
		RequestID:     p.RequestID,
	})
	if err != nil {
		return m7PromotionFailure(r, err, "release.promote")
	}
	return r.Ok(struct {
		PromotionID string `json:"promotionId"`
		State       string `json:"state"`
	}{prm.ID, m7flow.PromoteWireState(prm.State)})
}

func handleReleaseRollback(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PromotionID string `json:"promotionId"`
		Reason      string `json:"reason"`
		RequestID   string `json:"requestId"`
		OperatorID  string `json:"operatorId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PromotionID) ||
		len(p.Reason) < 1 || len(p.Reason) > 2000 ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 || len(p.OperatorID) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "release.rollback 参数无效", false)
	}
	if e.m7promotion == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "回退服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	prm, attempts, err := e.m7promotion.Rollback(ctx, m7app.RollbackInput{
		PromotionID: p.PromotionID,
		Reason:      p.Reason,
		RequestID:   p.RequestID,
		OperatorID:  p.OperatorID,
	})
	if err != nil {
		return m7PromotionFailure(r, err, "release.rollback")
	}
	// rollbackRef identifies the newest append-only attempt (RBK-002).
	ref := prm.ID
	if len(attempts) > 0 {
		ref = attempts[len(attempts)-1].ID
	}
	return r.Ok(struct {
		RollbackRef string `json:"rollbackRef"`
		State       string `json:"state"`
	}{ref, m7flow.RollbackWireState(prm.State)})
}

func handleReleaseGetPromotion(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PromotionID string `json:"promotionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PromotionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "release.getPromotion 参数无效", false)
	}
	if e.m7promotion == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "晋级服务暂时不可用", true)
	}
	view, err := e.m7promotion.GetPromotion(ctx, p.PromotionID)
	if err != nil {
		return m7PromotionFailure(r, err, "release.getPromotion")
	}
	timeline := view.Timeline
	if timeline == nil {
		timeline = []m7app.TimelineStep{}
	}
	migrations := make([]m7MigrationExecDTO, 0, len(view.Migrations))
	for _, m := range view.Migrations {
		migrations = append(migrations, m7MigrationExecDTO{
			ID: m.ID, PromotionID: m.PromotionID, PlanDigest: m.PlanDigest,
			State: m.State, RollbackRef: m.RollbackRef,
			CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	deployments := make([]m7DeploymentDTO, 0, len(view.Deployments))
	for _, d := range view.Deployments {
		deployments = append(deployments, m7DeploymentDTO{
			ID: d.ID, PromotionID: d.PromotionID, TargetEnv: d.TargetEnv,
			State: d.State, Receipt: d.ReceiptJSON,
			StartedAt:   m7RFC(d.StartedAt),
			CompletedAt: m7RFC(d.CompletedAt),
		})
	}
	attempts := make([]m7RollbackAttemptDTO, 0, len(view.RollbackAttempts))
	for _, a := range view.RollbackAttempts {
		attempts = append(attempts, m7RollbackAttemptDTO{
			ID: a.ID, PromotionID: a.PromotionID, Dimension: a.Dimension,
			State: a.State, PlanDigest: a.PlanDigest, OperatorID: a.OperatorID,
			Result: a.ResultJSON, CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339Nano),
			CompletedAt: m7RFC(a.CompletedAt),
		})
	}
	return r.Ok(struct {
		Promotion        m7PromotionDTO         `json:"promotion"`
		Timeline         []m7app.TimelineStep   `json:"timeline"`
		Migrations       []m7MigrationExecDTO   `json:"migrations"`
		Deployments      []m7DeploymentDTO      `json:"deployments"`
		RollbackAttempts []m7RollbackAttemptDTO `json:"rollbackAttempts"`
	}{
		Promotion: m7PromotionDTO{
			ID: view.Promotion.ID, PackageID: view.Promotion.PackageID,
			FromEnv: view.Promotion.FromEnv, ToEnv: view.Promotion.ToEnv,
			CanonicalIntentDigest: view.Promotion.CanonicalIntentDigest,
			PolicyVersion:         view.Promotion.PolicyVersion,
			State:                 view.Promotion.State,
			RequestedBy:           view.Promotion.RequestedBy,
			ApprovalExpiry:        m7RFC(view.Promotion.ApprovalExpiry),
			CreatedAt:             view.Promotion.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt:             view.Promotion.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
		Timeline: timeline, Migrations: migrations,
		Deployments: deployments, RollbackAttempts: attempts,
	})
}

// m7RFC renders an optional timestamp ("" for nil).
func m7RFC(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// m7PromotionDTO is the wire form of one promotion saga row.
type m7PromotionDTO struct {
	ID                    string `json:"id"`
	PackageID             string `json:"packageId"`
	FromEnv               string `json:"fromEnv"`
	ToEnv                 string `json:"toEnv"`
	CanonicalIntentDigest string `json:"canonicalIntentDigest"`
	PolicyVersion         string `json:"policyVersion"`
	State                 string `json:"state"`
	RequestedBy           string `json:"requestedBy"`
	ApprovalExpiry        string `json:"approvalExpiry,omitempty"`
	CreatedAt             string `json:"createdAt"`
	UpdatedAt             string `json:"updatedAt"`
}

// m7MigrationExecDTO is the wire form of one migration execution.
type m7MigrationExecDTO struct {
	ID          string `json:"id"`
	PromotionID string `json:"promotionId"`
	PlanDigest  string `json:"planDigest"`
	State       string `json:"state"`
	RollbackRef string `json:"rollbackRef,omitempty"`
	CreatedAt   string `json:"createdAt"`
}

// m7DeploymentDTO is the wire form of one deployment.
type m7DeploymentDTO struct {
	ID          string `json:"id"`
	PromotionID string `json:"promotionId"`
	TargetEnv   string `json:"targetEnv"`
	State       string `json:"state"`
	Receipt     string `json:"receipt,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// m7RollbackAttemptDTO is the wire form of one append-only rollback attempt.
type m7RollbackAttemptDTO struct {
	ID          string `json:"id"`
	PromotionID string `json:"promotionId"`
	Dimension   string `json:"dimension"`
	State       string `json:"state"`
	PlanDigest  string `json:"planDigest"`
	OperatorID  string `json:"operatorId"`
	Result      string `json:"result"`
	CreatedAt   string `json:"createdAt"`
	CompletedAt string `json:"completedAt,omitempty"`
}

// m7PromotionFailure maps m7app slice-4 errors onto the M7 wire family.
func m7PromotionFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrPromotionNotFound), errors.Is(err, m7app.ErrPackageNotFound),
		errors.Is(err, m7app.ErrRevisionNotFound):
		return r.Fail("NOT_FOUND", "晋级相关对象不存在", false)
	case errors.Is(err, m7app.ErrPackageNotSealed):
		return r.Fail("M7-PKG-003", "发行包未封版，禁止晋级", false)
	case errors.Is(err, m7app.ErrDigestMismatch):
		return r.Fail("M7-PKG-002", "发行包摘要校验失败，已被隔离", false)
	case errors.Is(err, m7app.ErrIntentChanged):
		return r.Fail("M7-PRM-002", "晋级意图已变化，冻结执行", false)
	case errors.Is(err, m7app.ErrConcurrentPromotion):
		return r.Fail("M7-PRM-001", "同环境已存在进行中的晋级，原晋级继续生效", false)
	case errors.Is(err, m7app.ErrPolicyRejected):
		return r.Fail("M7-PRM-003", "环境或时窗策略拒绝", false)
	case errors.Is(err, m7app.ErrApprovalInvalid):
		return r.Fail("M7-PRM-003", "晋级审批无效（SoD 校验失败）", false)
	case errors.Is(err, m7app.ErrApprovalExpired):
		return r.Fail("M7-PRM-003", "晋级审批已过期，请重新审批", false)
	case errors.Is(err, m7app.ErrMigrationFailed):
		return r.Fail("M7-MIG-002", "数据库迁移失败，已进入回退", false)
	case errors.Is(err, m7app.ErrDeploymentFailed):
		return r.Fail("M7-DEP-001", "部署或健康验证失败，按策略回退", false)
	case errors.Is(err, m7app.ErrRollbackFailed):
		return r.Fail("M7-RBK-001", "回退失败，环境已冻结并告警", false)
	case errors.Is(err, m7app.ErrOutcomeUnknown):
		return r.Fail("OUTCOME_UNKNOWN", "外部步骤结果未确认，等待对账后重放", true)
	case errors.Is(err, m7app.ErrRollbackNotAllowed), errors.Is(err, m7app.ErrIllegalPromotionTransition):
		return r.Fail("M7-RBK-002", "当前状态不允许该回退或状态迁移", false)
	case errors.Is(err, m7app.ErrServiceUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "晋级服务暂时不可用", true)
	}
	return r.Fail("INTERNAL_ERROR", method+" 执行失败", false)
}
