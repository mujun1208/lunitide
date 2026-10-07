package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/remotegateway"
)

// 手机伴侣远程网关的 bridge 入口（PRD §4.3）。全部方法归 settings scope，
// 远程设备默认无权调用（remote.* 本身就是管理面）。

func handleRemoteAccess(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if e.remoteGateway == nil {
		return request.Fail("REMOTE_UNAVAILABLE", "远程访问组件未就绪", false)
	}
	switch request.Method {
	case "remote.access.status":
		if !emptyObject(request.Payload) {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.access.status 参数无效", false)
		}
		status, err := e.remoteGateway.Status(ctx)
		if err != nil {
			return request.Fail("STORAGE_UNAVAILABLE", "远程访问状态暂时不可用", true)
		}
		return request.Ok(status)
	case "remote.access.enable":
		if !emptyObject(request.Payload) {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.access.enable 参数无效", false)
		}
		if err := e.remoteGateway.Enable(ctx); err != nil {
			return request.Fail("REMOTE_START_FAILED", "远程访问开启失败：" + err.Error(), false)
		}
		return request.Ok(map[string]any{"enabled": true})
	default: // remote.access.disable
		if !emptyObject(request.Payload) {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.access.disable 参数无效", false)
		}
		if err := e.remoteGateway.Disable(ctx); err != nil {
			return request.Fail("REMOTE_STOP_FAILED", "远程访问关闭失败：" + err.Error(), false)
		}
		return request.Ok(map[string]any{"enabled": false})
	}
}

func handleRemotePairCode(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if e.remoteGateway == nil {
		return request.Fail("REMOTE_UNAVAILABLE", "远程访问组件未就绪", false)
	}
	var payload struct {
		Lang string `json:"lang"`
	}
	if len(request.Payload) > 0 {
		var fields map[string]json.RawMessage
		if json.Unmarshal(request.Payload, &fields) != nil || len(fields) > 1 {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.pair.code 参数无效", false)
		}
		if len(fields) == 1 {
			if _, hasLang := fields["lang"]; !hasLang {
				return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.pair.code 参数无效", false)
			}
			if json.Unmarshal(request.Payload, &payload) != nil || (payload.Lang != "" && payload.Lang != "zh-CN" && payload.Lang != "en") {
				return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.pair.code 参数无效", false)
			}
		}
	}
	info, err := e.remoteGateway.IssuePairCode(ctx, payload.Lang)
	if err != nil {
		if errors.Is(err, remotegateway.ErrDisabled) {
			return request.Fail("REMOTE_DISABLED", "请先开启远程访问", false)
		}
		return request.Fail("REMOTE_PAIR_FAILED", "配对码生成失败，请重试", true)
	}
	return request.Ok(info)
}

func handleRemoteDevices(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if e.remoteGateway == nil {
		return request.Fail("REMOTE_UNAVAILABLE", "远程访问组件未就绪", false)
	}
	switch request.Method {
	case "remote.devices.list":
		if !emptyObject(request.Payload) {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.devices.list 参数无效", false)
		}
		devices, err := e.remoteGateway.Devices(ctx)
		if err != nil {
			return request.Fail("STORAGE_UNAVAILABLE", "设备列表暂时不可用", true)
		}
		return request.Ok(map[string]any{"devices": devices})
	default: // remote.devices.revoke
		var payload struct {
			DeviceID string `json:"deviceId"`
		}
		if err := decodePayload(request.Payload, &payload); err != nil || payload.DeviceID == "" {
			return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.devices.revoke 参数无效", false)
		}
		if err := e.remoteGateway.Revoke(ctx, payload.DeviceID); err != nil {
			return request.Fail("REMOTE_REVOKE_FAILED", "吊销失败：" + err.Error(), false)
		}
		return request.Ok(map[string]any{"revoked": true})
	}
}

func handleRemoteSessions(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if e.remoteGateway == nil {
		return request.Fail("REMOTE_UNAVAILABLE", "远程访问组件未就绪", false)
	}
	if !emptyObject(request.Payload) {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "remote.sessions.list 参数无效", false)
	}
	summary, err := e.remoteGateway.Sessions(ctx)
	if err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "远程会话与审计暂时不可用", true)
	}
	return request.Ok(summary)
}

func handlePowerKeepAwake(e *Engine, ctx context.Context, request bridge.Request) bridge.Response {
	if e.remoteGateway == nil {
		return request.Fail("REMOTE_UNAVAILABLE", "远程访问组件未就绪", false)
	}
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodePayload(request.Payload, &payload); err != nil {
		return request.Fail("BRIDGE_SCHEMA_INVALID", "power.keepAwake.set 参数无效", false)
	}
	if err := e.remoteGateway.SetKeepAwake(ctx, payload.Enabled); err != nil {
		return request.Fail("STORAGE_UNAVAILABLE", "防休眠设置失败，请重试", true)
	}
	return request.Ok(map[string]any{"enabled": payload.Enabled})
}
