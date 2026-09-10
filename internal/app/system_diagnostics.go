package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
)

func readinessDiagnostic(ready CapabilityReadiness) (string, string, string) {
	switch ready.Availability {
	case "ready":
		return "configured", ready.Detail, ""
	case "unavailable":
		return "degraded", ready.Detail, ready.Code
	default:
		return "not_configured", ready.Detail, ready.Code
	}
}

type diagnosticComponent struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Detail     string `json:"detail"`
	Code       string `json:"code,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

// Diagnostics checks local persisted truth without triggering model calls,
// installing components, opening capture devices, or executing desktop tools.
func handleSystemDiagnostics(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if !emptyObject(r.Payload) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "诊断参数无效", false)
	}
	if !e.diagnosticsRunning.CompareAndSwap(false, true) {
		return r.Fail("DIAGNOSTICS_BUSY", "系统检查正在进行，请稍后重试", true)
	}
	defer e.diagnosticsRunning.Store(false)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items := []diagnosticComponent{}
	add := func(id, name string, wired bool, probe func(context.Context) (string, string, string)) {
		item := diagnosticComponent{ID: id, Name: name, State: "not_configured", Detail: "当前实例未配置此服务"}
		if wired {
			item.State, item.Detail = "configured", "运行入口已配置；外部服务与设备需在对应模块测试"
			if probe != nil {
				started := time.Now()
				checkCtx, stop := context.WithTimeout(ctx, 1500*time.Millisecond)
				item.State, item.Detail, item.Code = probe(checkCtx)
				stop()
				item.DurationMS = time.Since(started).Milliseconds()
			}
		}
		if item.State == "degraded" {
			log.Printf("diagnostics trace_id=%s component=%s code=%s duration_ms=%d", r.TraceID, item.ID, item.Code, item.DurationMS)
		}
		items = append(items, item)
	}
	readResult := func(err error, detail string) (string, string, string) {
		if err != nil {
			return "degraded", "本地状态检查失败，请结合本次追踪编号定位", "LOCAL_CHECK_FAILED"
		}
		return "healthy", detail, ""
	}
	add("storage", "数据库", e.storageReadiness != nil, func(ctx context.Context) (string, string, string) {
		return readResult(e.storageReadiness.CheckReadiness(ctx), "连接可用，事务入口可访问")
	})
	add("audit", "权限与操作审计", e.auditVerifier != nil, func(ctx context.Context) (string, string, string) {
		err := e.auditVerifier.VerifyAuditChain(ctx)
		if errors.Is(err, audit.ErrChainBroken) {
			return "degraded", "审计链校验失败，发布受阻；请保留数据进行恢复核对", "AUDIT_CHAIN_BROKEN"
		}
		if err != nil {
			return "degraded", "审计校验未完成，请重试或查看诊断日志", "AUDIT_CHECK_INCOMPLETE"
		}
		return "healthy", "已验证当前一般操作审计链；历史未封链记录不在此校验范围", ""
	})
	add("providers", "模型与供应商", e.providers != nil, func(ctx context.Context) (string, string, string) {
		rows, err := e.providers.List(ctx, provider.Filter{})
		if err != nil {
			return readResult(err, "")
		}
		for _, p := range rows {
			if p.Status == provider.StatusEnabled && p.CredentialState == provider.CredentialConfigured && len(p.Models) > 0 {
				return "configured", "已配置可选模型；连通性以供应商连接测试为准", ""
			}
		}
		return "not_configured", "请先配置并启用供应商、凭据和模型", ""
	})
	add("text_chat", "打字对话", true, func(ctx context.Context) (string, string, string) {
		return readinessDiagnostic(e.capabilityReadiness(ctx, "chat"))
	})
	add("gui", "GUI / 视觉", true, func(ctx context.Context) (string, string, string) {
		return readinessDiagnostic(e.capabilityReadiness(ctx, "gui"))
	})
	add("voice_local", "语音：本地识别", e.voice != nil && e.voice.backend != nil, func(ctx context.Context) (string, string, string) {
		if e.voice.ready(ctx) {
			return "configured", "本地语音资源已就绪，录音设备尚未测试", ""
		}
		return "not_configured", "本地语音资源未就绪，请在语音设置中检查", ""
	})
	add("voice_cloud", "语音：云识别与对话", e.voice != nil && e.leases != nil, nil)
	add("voice_realtime", "语音：实时音频", e.talkDialer != nil && e.leases != nil, nil)
	add("meetings", "会议纪要", e.meetings != nil, func(ctx context.Context) (string, string, string) {
		_, err := e.meetings.List(ctx)
		return readResult(err, "会议记录读取正常；录音设备与转写服务需单独测试")
	})
	add("people", "同事聊天", e.people != nil, func(ctx context.Context) (string, string, string) {
		_, err := e.people.ListThreads(ctx)
		return readResult(err, "会话存储可读；跨设备送达以每条消息的收件回执为准")
	})
	add("projects", "项目与计划执行", e.projects != nil && e.planning != nil && e.agentRuns != nil, nil)
	add("assets", "资产与附件", e.attachmentService != nil, nil)
	add("skills", "技能中心", e.skills != nil && e.m6skills != nil, nil)
	add("plugins", "插件中心", e.m8plugin != nil, func(ctx context.Context) (string, string, string) {
		_, err := e.m8plugin.List(ctx, "", "")
		return readResult(err, "安装及停用状态可读取；各能力在执行前仍单独检查授权")
	})
	add("mcp", "MCP", e.m7mcp != nil && e.mcp6Registry != nil, nil)
	add("experts", "专家中心", e.m8expert != nil, nil)
	add("knowledge", "知识查新", e.m8kb != nil, nil)
	add("memory", "记忆", e.m8memory != nil, nil)
	add("browser", "浏览器控制", e.brmulti != nil, nil)
	add("computer", "电脑控制", e.ccctrl != nil, func(ctx context.Context) (string, string, string) {
		ready := e.capabilityReadiness(ctx, "desktop")
		switch ready.Availability {
		case "ready":
			return "configured", "电脑控制已启用；未执行鼠标、键盘或窗口操作", ""
		case "unavailable":
			if ready.Code == "SCOPE_DENIED" {
				return "disabled", ready.Detail, ready.Code
			}
			return "degraded", ready.Detail, ready.Code
		case "missing_dependency", "needs_config":
			if ready.Code == "CAPABILITY_NOT_READY" {
				return "disabled", "电脑控制已关闭", ready.Code
			}
			return "not_configured", ready.Detail, ready.Code
		default:
			return "disabled", ready.Detail, ready.Code
		}
	})
	add("tool_policy", "命令与钩子设置", e.tools != nil, func(ctx context.Context) (string, string, string) {
		for _, kind := range []string{"commands", "hooks"} {
			raw, err := e.tools.PolicySnapshot(kind)
			if err != nil {
				return readResult(err, "")
			}
			var status struct {
				State string `json:"state"`
			}
			if json.Unmarshal(raw, &status) != nil {
				return "degraded", "策略状态无法解析", "POLICY_INVALID"
			}
			if status.State != "applied" {
				return "degraded", "已保存的策略尚未应用，请检查设置", "POLICY_PENDING"
			}
		}
		return "healthy", "持久配置与当前运行规则的版本一致", ""
	})
	add("ocr", "OCR 路由", e.ocr != nil, func(ctx context.Context) (string, string, string) {
		ready := e.capabilityReadiness(ctx, "ocr")
		switch ready.Availability {
		case "ready":
			return "configured", ready.Detail, ""
		case "unavailable":
			return "degraded", ready.Detail, ready.Code
		default:
			return "not_configured", ready.Detail, ready.Code
		}
	})
	add("files", "文件批处理", e.fileOps != nil || e.tools != nil, func(ctx context.Context) (string, string, string) {
		ready := e.capabilityReadiness(ctx, "files")
		if ready.Availability == "ready" {
			return "configured", ready.Detail, ""
		}
		return "not_configured", ready.Detail, ready.Code
	})
	add("automation", "自动化", e.automation != nil, func(ctx context.Context) (string, string, string) {
		if _, err := e.automation.Store().ListJobs(); err != nil {
			return readResult(err, "")
		}
		snapshot := e.automation.Snapshot()
		if snapshot.LastError != "" {
			return "degraded", snapshot.LastError, "SCHEDULER_STORAGE_ERROR"
		}
		if !snapshot.Running {
			return "disabled", "调度器未运行", ""
		}
		if !snapshot.LastHeartbeat.IsZero() && time.Since(snapshot.LastHeartbeat) > time.Minute {
			return "degraded", "调度心跳已超过一分钟", "SCHEDULER_STALE"
		}
		return "healthy", "任务配置可读，调度器心跳正常", ""
	})
	add("connectors", "消息连接器", e.imChannels != nil && e.mcmarket != nil, nil)
	add("mro", "MRO工作台", e.mro != nil, func(ctx context.Context) (string, string, string) {
		_, err := e.mro.ListAircraft(ctx)
		return readResult(err, "工作台存储可读；业务数据有效性由来源和版本决定")
	})
	state := "healthy"
	for _, item := range items {
		if item.State == "degraded" {
			state = "degraded"
			break
		}
	}
	return r.Ok(map[string]any{"state": state, "checkedAt": time.Now().UTC().Format(time.RFC3339Nano), "traceId": r.TraceID, "components": items})
}
