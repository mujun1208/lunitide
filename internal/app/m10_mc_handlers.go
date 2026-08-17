package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/mcapp"
)

// M10 wave-3 MCP-market handlers: mc.market.list/detail,
// mc.config.validate, mc.confirm.token, mc.connector.install/uninstall/
// update/usage and mc.tombstone.check. Error mapping follows the M10
// wire contract (M10-MC-001~007); lifecycle mutations all burn one
// single-use confirmation token inside the same write transaction.

type mcValidationCheckDTO struct {
	Rule   string `json:"rule"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

func mcCheckDTOs(res mcapp.ValidationResult) []mcValidationCheckDTO {
	out := make([]mcValidationCheckDTO, 0, len(res.Checks))
	for _, c := range res.Checks {
		out = append(out, mcValidationCheckDTO{Rule: c.Rule, Passed: c.Passed, Reason: c.Reason})
	}
	return out
}

func handleMcMarketList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Query         string `json:"query"`
		TransportHint string `json:"transportHint"`
		Cursor        string `json:"cursor"`
		Limit         int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.market.list 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	items, fresh, next, err := e.mcmarket.MarketList(ctx, p.Query, p.TransportHint, p.Cursor, p.Limit)
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Items      []mcapp.MarketItemDTO `json:"items"`
		Fresh      bool                  `json:"fresh"`
		NextCursor string                `json:"nextCursor,omitempty"`
	}{Items: items, Fresh: fresh, NextCursor: next})
}

func handleMcMarketDetail(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ItemID string `json:"itemId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ItemID) < 1 || len(p.ItemID) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.market.detail 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	item, cfg, res, err := e.mcmarket.MarketDetail(ctx, p.ItemID)
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Item   mcapp.MarketItemDTO        `json:"item"`
		Config mcapp.ConfigInput          `json:"config"`
		Checks []mcValidationCheckDTO     `json:"checks"`
	}{
		Item: mcapp.MarketItemDTO{
			ID: item.ID, Name: item.Name, Publisher: item.Publisher, Description: item.Description,
			TransportHint: item.TransportHint, CatalogDigest: item.CatalogDigest, FetchedAt: item.FetchedAt,
		},
		Config: cfg,
		Checks: mcCheckDTOs(res),
	})
}

func handleMcConfigValidate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p mcapp.ConfigInput
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.config.validate 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	res, err := e.mcmarket.ValidateConfig(ctx, p)
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Valid  bool                    `json:"valid"`
		Checks []mcValidationCheckDTO  `json:"checks"`
	}{Valid: res.Valid, Checks: mcCheckDTOs(res)})
}

func handleMcConfirmToken(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Method string `json:"method"`
		Target string `json:"target"`
		Digest string `json:"digest"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Target) < 1 || len(p.Target) > 256 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.confirm.token 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	token, expiresAt, err := e.mcmarket.IssueConfirmToken(ctx, p.Method, p.Target, p.Digest)
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		ConfirmToken string `json:"confirmToken"`
		ExpiresAt    string `json:"expiresAt"`
	}{ConfirmToken: token, ExpiresAt: expiresAt})
}

func handleMcConnectorInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Origin        string            `json:"origin"`
		Transport     string            `json:"transport"`
		Command       string            `json:"command"`
		Args          []string          `json:"args"`
		URL           string            `json:"url"`
		EnvSecretRefs map[string]string `json:"envSecretRefs"`
		MarketItemID  string            `json:"marketItemId"`
		ConfirmToken  string            `json:"confirmToken"`
		RequestID     string            `json:"requestId"`
		Actor         string            `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ConfirmToken) != 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.connector.install 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	out, res, err := e.mcmarket.Install(ctx, mcapp.InstallInput{
		Origin: p.Origin, Transport: p.Transport, Command: p.Command, Args: p.Args,
		URL: p.URL, EnvSecretRefs: p.EnvSecretRefs, MarketItemID: p.MarketItemID,
		ConfirmToken: p.ConfirmToken, Actor: p.Actor,
	})
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		EndpointID       string                 `json:"endpointId"`
		State            string                 `json:"state"`
		CapabilityDigest string                 `json:"capabilityDigest,omitempty"`
		Checks           []mcValidationCheckDTO `json:"checks"`
	}{
		EndpointID: out.EndpointID, State: out.State,
		CapabilityDigest: out.CapabilityDigest, Checks: mcCheckDTOs(res),
	})
}

func handleMcConnectorUninstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID   string `json:"endpointId"`
		ConfirmToken string `json:"confirmToken"`
		Actor        string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.EndpointID) < 1 || len(p.EndpointID) > 128 || len(p.ConfirmToken) != 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.connector.uninstall 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	state, err := e.mcmarket.Uninstall(ctx, p.EndpointID, p.ConfirmToken, p.Actor)
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		EndpointID string `json:"endpointId"`
		State      string `json:"state"`
	}{EndpointID: p.EndpointID, State: state})
}

func handleMcConnectorUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID   string   `json:"endpointId"`
		URL          string   `json:"url"`
		Args         []string `json:"args"`
		ConfirmToken string   `json:"confirmToken"`
		Actor        string   `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.EndpointID) < 1 || len(p.EndpointID) > 128 || len(p.ConfirmToken) != 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.connector.update 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	out, res, err := e.mcmarket.Update(ctx, mcapp.UpdateInput{
		EndpointID: p.EndpointID, URL: p.URL, Args: p.Args,
		ConfirmToken: p.ConfirmToken, Actor: p.Actor,
	})
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		EndpointID       string                 `json:"endpointId"`
		State            string                 `json:"state"`
		CapabilityDigest string                 `json:"capabilityDigest,omitempty"`
		Checks           []mcValidationCheckDTO `json:"checks"`
	}{
		EndpointID: out.EndpointID, State: out.State,
		CapabilityDigest: out.CapabilityDigest, Checks: mcCheckDTOs(res),
	})
}

func handleMcConnectorUsage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID string `json:"endpointId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.connector.usage 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	stats, err := e.mcmarket.Usage(ctx, p.EndpointID)
	if err != nil {
		return mcFailure(r, err)
	}
	if stats == nil {
		stats = []mcapp.EndpointUsage{}
	}
	return bridge.Success(r.ID, struct {
		Stats []mcapp.EndpointUsage `json:"stats"`
	}{Stats: stats})
}

func handleMcTombstoneCheck(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mc.tombstone.check 参数无效", false)
	}
	if e.mcmarket == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场服务暂时不可用", true)
	}
	report, err := e.mcmarket.TombstoneCheck(ctx)
	if err != nil {
		return mcFailure(r, err)
	}
	return bridge.Success(r.ID, report)
}

// mcFailure maps mcapp errors onto M10-MC-001~007.
func mcFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, mcapp.ErrMcSchema):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-001", "连接器配置未通过校验链（MC-VR-01~08）", false)
	case errors.Is(err, mcapp.ErrMcConfirm):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-002", "确认令牌缺失、过期、已使用或不匹配", false)
	case errors.Is(err, mcapp.ErrMcSource):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-003", "市场来源签名或摘要校验失败", false)
	case errors.Is(err, mcapp.ErrMcNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-004", "市场条目或端点不存在", false)
	case errors.Is(err, mcapp.ErrMcQuota):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-005", "端点配额已满或指纹重复", false)
	case errors.Is(err, mcapp.ErrMcRateLimited):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-006", "市场操作频率超限（每分钟 11 次）", false)
	case errors.Is(err, mcapp.ErrMcRegistry):
		return bridge.Failure(r.ID, r.TraceID, "M10-MC-007", "市场目录不可达且本地缓存为空", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 市场存储暂时不可用", true)
}
