package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/lunitide/lunitide/internal/scheduler"
)

func handleImChannelsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "im.channels.get 参数无效", false)
	}
	if e.imChannels == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息通道暂时不可用", true)
	}
	items, err := e.imChannels.List(ctx)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "无法读取消息通道", true)
	}
	return bridge.Success(r.ID, map[string]any{"channels": items})
}

func handleImChannelsSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Kind       string  `json:"kind"`
		Enabled    *bool   `json:"enabled"`
		WebhookURL *string `json:"webhookUrl"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "im.channels.set 参数无效", false)
	}
	if e.imChannels == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "消息通道暂时不可用", true)
	}
	kind, err := imapp.ParseKind(p.Kind)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "不支持的消息通道", false)
	}
	items, err := e.imChannels.Set(ctx, kind, p.Enabled, p.WebhookURL)
	if err != nil {
		if errors.Is(err, scheduler.ErrWebhookInvalid) {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "Webhook 地址无效：仅支持飞书/企微/钉钉的 https 机器人地址", false)
		}
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", err.Error(), false)
	}
	return bridge.Success(r.ID, map[string]any{"channels": items})
}
