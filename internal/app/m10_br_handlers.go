package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/brapp"
	"github.com/lunitide/lunitide/internal/bridge"
)

// M10 wave-3 browser multi-mode handlers: br.settings.get/update,
// br.mode.detect, br.session.connect/list/disconnect, br.navigate,
// br.data.usage/clear and br.permission.list/request/decide/policy.
// Error mapping follows the M10 wire contract (M10-BR-001~006); the
// CDP state machine and the six audit actions live in brapp.

func handleBrSettingsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.settings.get 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	settings, err := e.brmulti.GetSettings(ctx)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(settings)
}

func handleBrSettingsUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Mode                *string   `json:"mode"`
		ChromePath          *string   `json:"chromePath"`
		EdgePath            *string   `json:"edgePath"`
		ExtensionPort       *int      `json:"extensionPort"`
		Allowlist           *[]string `json:"allowlist"`
		DataRetentionDays   *int      `json:"dataRetentionDays"`
		BlockPrivateNetwork *bool     `json:"blockPrivateNetworks"`
		Actor               string    `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.settings.update 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	settings, err := e.brmulti.UpdateSettings(ctx, brapp.SettingsPatch{
		Mode: p.Mode, ChromePath: p.ChromePath, EdgePath: p.EdgePath,
		ExtensionPort: p.ExtensionPort, Allowlist: p.Allowlist,
		DataRetentionDays: p.DataRetentionDays, BlockPrivateNetwork: p.BlockPrivateNetwork,
		Actor: p.Actor,
	})
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(settings)
}

func handleBrModeDetect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.mode.detect 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	report, err := e.brmulti.DetectModes(ctx)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(report)
}

func handleBrSessionConnect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Mode      string `json:"mode"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.session.connect 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	sess, err := e.brmulti.Connect(ctx, p.SessionID, p.Mode, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(sess)
}

func handleBrSessionList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.session.list 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	sessions, err := e.brmulti.ListSessions(ctx)
	if err != nil {
		return brFailure(r, err)
	}
	if sessions == nil {
		sessions = []brapp.Session{}
	}
	return r.Ok(struct {
		Sessions []brapp.Session `json:"sessions"`
	}{Sessions: sessions})
}

func handleBrSessionDisconnect(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.SessionID) < 1 || len(p.SessionID) > 64 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.session.disconnect 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	sess, err := e.brmulti.Disconnect(ctx, p.SessionID, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(sess)
}

func handleBrNavigate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		URL       string `json:"url"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.SessionID) < 1 || len(p.SessionID) > 64 ||
		len(p.URL) < 1 || len(p.URL) > 2048 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.navigate 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	out, err := e.brmulti.Navigate(ctx, p.SessionID, p.URL, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(out)
}

func handleBrDataUsage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.data.usage 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	usage, err := e.brmulti.DataUsage(ctx, p.SessionID)
	if err != nil {
		return brFailure(r, err)
	}
	if usage == nil {
		usage = []brapp.DataUsage{}
	}
	return r.Ok(struct {
		Usage []brapp.DataUsage `json:"usage"`
	}{Usage: usage})
}

func handleBrDataClear(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SessionID string `json:"sessionId"`
		Actor     string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.data.clear 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	out, err := e.brmulti.ClearData(ctx, p.SessionID, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(out)
}

func handleBrPermissionList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		State string `json:"state"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.permission.list 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	perms, err := e.brmulti.ListPermissions(ctx, p.State)
	if err != nil {
		return brFailure(r, err)
	}
	if perms == nil {
		perms = []brapp.Permission{}
	}
	return r.Ok(struct {
		Permissions []brapp.Permission `json:"permissions"`
	}{Permissions: perms})
}

func handleBrPermissionRequest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Origin     string `json:"origin"`
		Permission string `json:"permission"`
		SessionID  string `json:"sessionId"`
		Actor      string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Origin) < 1 || len(p.Origin) > 512 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.permission.request 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	perm, err := e.brmulti.RequestPermission(ctx, p.Origin, p.Permission, p.SessionID, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(perm)
}

func handleBrPermissionDecide(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PermissionID string `json:"permissionId"`
		Decision     string `json:"decision"`
		Actor        string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.PermissionID) < 1 || len(p.PermissionID) > 64 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.permission.decide 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	perm, err := e.brmulti.DecidePermission(ctx, p.PermissionID, p.Decision, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(perm)
}

func handleBrPermissionPolicy(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Origin     string `json:"origin"`
		Permission string `json:"permission"`
		Policy     string `json:"policy"`
		Actor      string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Origin) < 1 || len(p.Origin) > 512 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "br.permission.policy 参数无效", false)
	}
	if e.brmulti == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式服务暂时不可用", true)
	}
	perm, err := e.brmulti.SetPermissionPolicy(ctx, p.Origin, p.Permission, p.Policy, p.Actor)
	if err != nil {
		return brFailure(r, err)
	}
	return r.Ok(perm)
}

// brFailure maps brapp errors onto M10-BR-001~006.
func brFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, brapp.ErrBrSchema):
		return r.Fail("M10-BR-001", "浏览器参数或配置无效", false)
	case errors.Is(err, brapp.ErrBrState):
		return r.Fail("M10-BR-002", "CDP 会话状态机迁移非法", false)
	case errors.Is(err, brapp.ErrBrNotFound):
		return r.Fail("M10-BR-003", "浏览器会话或权限不存在", false)
	case errors.Is(err, brapp.ErrBrURLPolicy):
		return r.Fail("M10-BR-004", "导航 URL 被策略拒绝（白名单/私网/端口）", false)
	case errors.Is(err, brapp.ErrBrMode):
		return r.Fail("M10-BR-005", "浏览器模式不可用", false)
	case errors.Is(err, brapp.ErrBrRateLimited):
		return r.Fail("M10-BR-006", "浏览器操作频率超限", false)
	}
	return r.Fail("STORAGE_UNAVAILABLE", "浏览器多模式存储暂时不可用", true)
}
