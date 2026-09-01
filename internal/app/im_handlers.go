package app

import (
	"context"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/lunitide/lunitide/internal/scheduler"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func handleImChannelsGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "im.channels.get 参数无效", false)
	}
	if e.imChannels == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "消息通道暂时不可用", true)
	}
	items, err := e.imChannels.List(ctx)
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "无法读取消息通道", true)
	}
	return r.Ok(map[string]any{"channels": items})
}

func handleImChannelsSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Kind             string  `json:"kind"`
		Enabled          *bool   `json:"enabled"`
		WebhookURL       *string `json:"webhookUrl"`
		InboundEnabled   *bool   `json:"inboundEnabled"`
		InboundAllowlist *string `json:"inboundAllowlist"`
		InboundAutoRun   *bool   `json:"inboundAutoRun"`
		InboundAppID     *string `json:"inboundAppId"`
		InboundAppSecret *string `json:"inboundAppSecret"`
		TestSend         *bool   `json:"testSend"`
		ProbeDesktop     *bool   `json:"probeDesktop"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "im.channels.set 参数无效", false)
	}
	if e.imChannels == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "消息通道暂时不可用", true)
	}
	kind, err := imapp.ParseKind(p.Kind)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "不支持的消息通道", false)
	}
	if p.TestSend != nil && *p.TestSend {
		url := ""
		if p.WebhookURL != nil {
			url = strings.TrimSpace(*p.WebhookURL)
		}
		if url == "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "试发需要 Webhook 地址", false)
		}
		n, err := scheduler.NewWebhookNotifier(url)
		if err != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "Webhook 地址无效：仅支持飞书/企微/钉钉的 https 机器人地址", false)
		}
		if err := n.Notify(imapp.Label(kind), "月汐已连上"); err != nil {
			return r.Fail("WEBHOOK_UNREACHABLE", "试发失败，地址没保存："+err.Error(), true)
		}
	}
	if p.ProbeDesktop != nil && *p.ProbeDesktop && p.Enabled != nil && *p.Enabled && (kind == imapp.KindWeChat || kind == imapp.KindQQ) {
		if err := toolruntime.ProbeDesktopApp(imapp.DesktopApp(kind)); err != nil {
			return r.Fail("DESKTOP_APP_MISSING", "本机没有检测到"+imapp.Label(kind)+"，无法启用", false)
		}
	}
	items, err := e.imChannels.Set(ctx, kind, imapp.ChannelPatch{
		Enabled: p.Enabled, WebhookURL: p.WebhookURL,
		InboundEnabled: p.InboundEnabled, InboundAllowlist: p.InboundAllowlist,
		InboundAutoRun: p.InboundAutoRun, InboundAppID: p.InboundAppID, InboundAppSecret: p.InboundAppSecret,
	})
	if err != nil {
		if errors.Is(err, scheduler.ErrWebhookInvalid) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "Webhook 地址无效：仅支持飞书/企微/钉钉的 https 机器人地址", false)
		}
		if errors.Is(err, imapp.ErrWebhookRequired) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "飞书/企微/钉钉启用前请粘贴 Webhook，不能只用本机打字", false)
		}
		if errors.Is(err, imapp.ErrInboundAllowlist) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "开启入站需要 App ID，第一条消息会写入白名单", false)
		}
		if errors.Is(err, imapp.ErrInboundKind) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "仅飞书和企业微信支持入站", false)
		}
		return r.Fail("BRIDGE_SCHEMA_INVALID", "消息通道设置无效", false)
	}
	return r.Ok(map[string]any{"channels": items})
}
