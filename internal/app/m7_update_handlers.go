package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-5 handlers (T-7.5.x): appUpdate.check / appUpdate.install on the
// app-update split-track. The namespace never accepts a project release ID;
// publish stays an internal management operation per the wire contract.
//
// Error mapping: digest/signature failures answer M7-UPD-001 (install
// forbidden); window, downgrade and nonce-replay rejections share the
// 422 family with a concrete reason; a broken audit ledger freezes
// production promotions via M7-DR-001 (guard in the promote handler).

func handleAppUpdateCheck(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Channel        string `json:"channel"`
		CurrentVersion string `json:"currentVersion"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		(p.Channel != "stable" && p.Channel != "beta") ||
		len(p.CurrentVersion) < 1 || len(p.CurrentVersion) > 32 ||
		strings.ContainsAny(p.CurrentVersion, " ,;/\\") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "appUpdate.check 参数无效", false)
	}
	if e.m7update == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "更新服务暂时不可用", true)
	}
	res, err := e.m7update.Check(ctx, p.Channel, p.CurrentVersion)
	if err != nil {
		return m7UpdateFailure(r, err, "appUpdate.check")
	}
	return r.Ok(struct {
		UpdateID  string `json:"updateId"`
		Version   string `json:"version"`
		Digest    string `json:"digest"`
		Mandatory bool   `json:"mandatory"`
	}{res.UpdateID, res.Version, res.Digest, res.Mandatory})
}

func handleAppUpdateInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UpdateID       string `json:"updateId"`
		ExpectedDigest string `json:"expectedDigest"`
		DeviceID       string `json:"deviceId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UpdateID) ||
		len(p.ExpectedDigest) != 64 || strings.ContainsAny(p.ExpectedDigest, "ghijklmnopqrstuvwxyz") ||
		len(p.DeviceID) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "appUpdate.install 参数无效", false)
	}
	if e.m7update == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "更新服务暂时不可用", true)
	}
	// Install mutates device state - idempotency key is mandatory.
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	state, err := e.m7update.Install(ctx, m7app.InstallInput{
		UpdateID:       p.UpdateID,
		ExpectedDigest: p.ExpectedDigest,
		DeviceID:       p.DeviceID,
	})
	if err != nil {
		return m7UpdateFailure(r, err, "appUpdate.install")
	}
	return r.Ok(struct {
		State string `json:"state"`
	}{state})
}

// auditChainVerifier re-derives a tamper-evident audit hash chain and reports
// audit.ErrChainBroken on any edit/deletion/reorder/insertion. Both the m7
// ledger (m7app.UpdateService) and the general audit_events chain (W3,
// *sqlite.Store) satisfy it; the interface keeps app-layer tests stubbable.
type auditChainVerifier interface {
	VerifyAuditChain(ctx context.Context) error
}

// m7AuditGuard freezes production promotions while any audit chain is broken
// (M7-DR-001). Called by release.promote before the saga starts. It verifies
// both the m7 release ledger and — since W3 — the general audit_events chain,
// so tampering with either integrity log halts promotion.
func m7AuditGuard(e *Engine, ctx context.Context, r bridge.Request) *bridge.Response {
	verifiers := []auditChainVerifier{}
	if e.m7update != nil {
		verifiers = append(verifiers, e.m7update)
	}
	if e.auditVerifier != nil {
		verifiers = append(verifiers, e.auditVerifier)
	}
	for _, v := range verifiers {
		if err := v.VerifyAuditChain(ctx); err != nil {
			if errors.Is(err, audit.ErrChainBroken) {
				resp := r.Fail("M7-DR-001", "审计链断裂，生产晋级已冻结", false)
				return &resp
			}
			resp := r.Fail("STORAGE_UNAVAILABLE", "审计账本暂时不可读", true)
			return &resp
		}
	}
	return nil
}

// m7UpdateFailure maps m7app slice-5 errors onto the M7 wire family.
func m7UpdateFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrUpdateNotFound):
		return r.Fail("NOT_FOUND", "更新包不存在", false)
	case errors.Is(err, m7app.ErrUpdateSignature):
		return r.Fail("M7-UPD-001", "更新签名或摘要校验失败，禁止安装", false)
	case errors.Is(err, m7app.ErrUpdateWindowClosed):
		return r.Fail("M7-UPD-001", "更新不在可信时间窗内，禁止安装", false)
	case errors.Is(err, m7app.ErrUpdateDowngrade):
		return r.Fail("M7-UPD-001", "目标版本低于当前版本或最低版本，禁止降级", false)
	case errors.Is(err, m7app.ErrNonceReplayed):
		return r.Fail("M7-UPD-001", "更新 nonce 已消费，拒绝重放", false)
	case errors.Is(err, m7app.ErrUpdateNotPublished):
		return r.Fail("M7-UPD-001", "更新包未发布，禁止安装", false)
	case errors.Is(err, m7app.ErrUpdateInstallFailed):
		return r.Fail("M7-DEP-001", "更新安装失败，已自动回退", false)
	case errors.Is(err, m7app.ErrUpdateRollbackFailed):
		return r.Fail("M7-RBK-001", "更新回退失败，安装已冻结并告警", false)
	case errors.Is(err, m7app.ErrUpdateChannelInvalid),
		errors.Is(err, m7app.ErrIllegalInstallationTransition):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "更新轨道参数或状态非法", false)
	case errors.Is(err, m7app.ErrServiceUnavailable):
		return r.Fail("STORAGE_UNAVAILABLE", "更新服务暂时不可用", true)
	}
	return r.Fail("INTERNAL_ERROR", method+" 执行失败", false)
}
