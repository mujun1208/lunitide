package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
)

// M10 wave-4 computer-control handlers: cc.getConfig/updateConfig,
// cc.getAuditLog and cc.emergencyStop. Error mapping follows the M10 wire
// contract (M10-CC-001~012); the three-layer interception pipeline and the
// cc.* agent tools live in ccapp behind the tool runtime.

func handleCcGetConfig(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "cc.getConfig 参数无效", false)
	}
	if e.ccctrl == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "电脑控制服务暂时不可用", true)
	}
	settings, err := e.ccctrl.GetConfig(ctx)
	if err != nil {
		return ccFailure(r, err)
	}
	return bridge.Success(r.ID, settings)
}

func handleCcUpdateConfig(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Enabled              *bool     `json:"enabled"`
		SecurityLevel        *string   `json:"securityLevel"`
		AllowCritical        *bool     `json:"allowCritical"`
		ProcessBlocklist     *[]string `json:"processBlocklist"`
		MaxActionsPerMinute  *int      `json:"maxActionsPerMinute"`
		ConfirmTimeoutSecond *int      `json:"confirmTimeoutSeconds"`
		Actor                string    `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "cc.updateConfig 参数无效", false)
	}
	if e.ccctrl == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "电脑控制服务暂时不可用", true)
	}
	settings, err := e.ccctrl.UpdateConfig(ctx, ccapp.SettingsPatch{
		Enabled: p.Enabled, SecurityLevel: p.SecurityLevel, AllowCritical: p.AllowCritical,
		ProcessBlocklist: p.ProcessBlocklist, MaxActionsPerMinute: p.MaxActionsPerMinute,
		ConfirmTimeoutSecond: p.ConfirmTimeoutSecond, Actor: p.Actor,
	})
	if err != nil {
		return ccFailure(r, err)
	}
	return bridge.Success(r.ID, settings)
}

func handleCcGetAuditLog(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Limit     int    `json:"limit"`
		Status    string `json:"status"`
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "cc.getAuditLog 参数无效", false)
	}
	if e.ccctrl == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "电脑控制服务暂时不可用", true)
	}
	entries, err := e.ccctrl.GetAuditLog(ctx, p.Limit, p.Status, p.SessionID)
	if err != nil {
		return ccFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"items": entries})
}

func handleCcEmergencyStop(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Actor  string `json:"actor"`
		Reason string `json:"reason"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "cc.emergencyStop 参数无效", false)
	}
	if e.ccctrl == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "电脑控制服务暂时不可用", true)
	}
	settings, err := e.ccctrl.EmergencyStop(ctx, p.Actor, p.Reason)
	if err != nil {
		return ccFailure(r, err)
	}
	return bridge.Success(r.ID, settings)
}

// ccFailure maps ccapp errors onto M10-CC-001~012.
func ccFailure(r bridge.Request, err error) bridge.Response {
	switch ccapp.Code(err) {
	case "M10-CC-001":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-001", "电脑控制参数或配置无效", false)
	case "M10-CC-002":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-002", "电脑控制状态迁移非法（紧急停止后需重新启用）", false)
	case "M10-CC-003":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-003", "电脑控制审计记录不存在", false)
	case "M10-CC-004":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-004", "操作被风险策略拦截", false)
	case "M10-CC-005":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-005", "紧急停止已激活", false)
	case "M10-CC-006":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-006", "电脑控制操作频率超限", false)
	case "M10-CC-007":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-007", "操作需要人工确认", false)
	case "M10-CC-008":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-008", "输入参数被过滤层拒绝", false)
	case "M10-CC-009":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-009", "前台进程在阻止名单中", false)
	case "M10-CC-010":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-010", "控制引擎不可用（非 Windows 主机）", false)
	case "M10-CC-011":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-011", "控制操作执行失败", false)
	case "M10-CC-012":
		return bridge.Failure(r.ID, r.TraceID, "M10-CC-012", "电脑控制未启用", false)
	}
	return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "电脑控制存储暂时不可用", true)
}
