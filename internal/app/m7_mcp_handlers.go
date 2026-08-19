package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-8 handlers (T-7.8.x): the MCP settings plane
// (mcp.add/list/toggle/health/market.search). Invoke itself stays on
// mcp6.invoke per the wire contract - these handlers never expose tool
// execution, and no method answers secret material.
//
// Error mapping: schema violations answer M7-MCP-001 (422), source-trust
// failures answer M7-MCP-002 (403), capability drift answers M7-MCP-003
// (409, quarantined), probe failures answer M7-MCP-004 (502) and registry
// outages answer M7-MCP-005 (503, degraded cache).

func handleMcpAdd(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Origin        string            `json:"origin"`
		Transport     string            `json:"transport"`
		Command       string            `json:"command"`
		Args          []string          `json:"args"`
		URL           string            `json:"url"`
		EnvSecretRefs map[string]string `json:"envSecretRefs"`
		MarketItemID  string            `json:"marketItemId"`
		RiskConfirmed bool              `json:"riskConfirmed"`
		RequestID     string            `json:"requestId"`
		Actor         string            `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		(p.Origin != m7flow.McpOriginMarket && p.Origin != m7flow.McpOriginManual) ||
		len(p.Command) > 512 || len(p.URL) > 2048 || (p.MarketItemID != "" && !validCanonicalULID(p.MarketItemID)) ||
		len(p.RequestID) < 1 || len(p.RequestID) > 128 || len(p.Actor) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp.add 参数无效", false)
	}
	if e.m7mcp == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	res, err := e.m7mcp.Add(ctx, m7app.McpAddInput{
		Origin:         p.Origin,
		Transport:      p.Transport,
		Command:        p.Command,
		Args:           p.Args,
		URL:            p.URL,
		EnvSecretRefs:  p.EnvSecretRefs,
		MarketItemID:   p.MarketItemID,
		RiskConfirmed:  p.RiskConfirmed,
		Actor:          p.Actor,
		IdempotencyKey: p.RequestID,
	})
	if err != nil {
		return m7McpFailure(r, err, "mcp.add")
	}
	e.admitSettingsMcp(ctx, m7flow.McpEndpointConfig{
		EndpointID: res.EndpointID,
		Transport:  p.Transport,
		Command:    p.Command,
		URL:        p.URL,
		ArgsJSON:   mustJSONArgs(p.Args),
		Enabled:    true,
		State:      res.State,
	})
	return bridge.Success(r.ID, struct {
		EndpointID       string `json:"endpointId"`
		State            string `json:"state"`
		CapabilityDigest string `json:"capabilityDigest,omitempty"`
	}{res.EndpointID, res.State, res.CapabilityDigest})
}

func handleMcpList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Transport string `json:"transport"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		(p.Transport != "" && p.Transport != m7flow.McpTransportStdio && p.Transport != m7flow.McpTransportHTTPS) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp.list 参数无效", false)
	}
	if e.m7mcp == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 服务暂时不可用", true)
	}
	eps, err := e.m7mcp.List(ctx, p.Transport)
	if err != nil {
		return m7McpFailure(r, err, "mcp.list")
	}
	items := make([]m7McpEndpointDTO, 0, len(eps))
	for _, ep := range eps {
		items = append(items, m7McpEndpointDTO{
			EndpointID:   ep.EndpointID,
			Transport:    ep.Transport,
			State:        ep.State,
			Enabled:      ep.Enabled,
			Origin:       ep.Origin,
			LastHealthAt: ep.LastHealthAt,
		})
	}
	return bridge.Success(r.ID, struct {
		Endpoints []m7McpEndpointDTO `json:"endpoints"`
	}{items})
}

// m7McpEndpointDTO is one row of the mcp.list projection.
type m7McpEndpointDTO struct {
	EndpointID   string `json:"endpointId"`
	Transport    string `json:"transport"`
	State        string `json:"state"`
	Enabled      bool   `json:"enabled"`
	Origin       string `json:"origin"`
	LastHealthAt string `json:"lastHealthAt,omitempty"`
}

func handleMcpToggle(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID string `json:"endpointId"`
		Enabled    *bool  `json:"enabled"`
		Actor      string `json:"actor"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.EndpointID) < 1 || len(p.EndpointID) > 128 ||
		p.Enabled == nil || len(p.Actor) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp.toggle 参数无效", false)
	}
	if e.m7mcp == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 服务暂时不可用", true)
	}
	ep, err := e.m7mcp.Toggle(ctx, p.EndpointID, *p.Enabled, p.Actor)
	if err != nil {
		return m7McpFailure(r, err, "mcp.toggle")
	}
	if ep.Enabled {
		e.admitSettingsMcp(ctx, ep)
	} else {
		e.dropSettingsMcp(ep.EndpointID)
	}
	return bridge.Success(r.ID, struct {
		EndpointID string `json:"endpointId"`
		Enabled    bool   `json:"enabled"`
		State      string `json:"state"`
	}{ep.EndpointID, ep.Enabled, ep.State})
}

func handleMcpHealth(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID string `json:"endpointId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.EndpointID) < 1 || len(p.EndpointID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp.health 参数无效", false)
	}
	if e.m7mcp == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 服务暂时不可用", true)
	}
	res, err := e.m7mcp.Health(ctx, p.EndpointID)
	if err != nil {
		return m7McpFailure(r, err, "mcp.health")
	}
	return bridge.Success(r.ID, struct {
		State            string `json:"state"`
		LatencyMS        int64  `json:"latencyMs"`
		DriftDetected    bool   `json:"driftDetected"`
		CapabilityDigest string `json:"capabilityDigest,omitempty"`
		CheckedAt        string `json:"checkedAt"`
	}{res.State, res.LatencyMS, res.DriftDetected, res.CapabilityDigest, res.CheckedAt})
}

func handleMcpMarketSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Query  string `json:"query"`
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Query) < 1 || len(p.Query) > 128 ||
		len(p.Cursor) > 128 || p.Limit < 0 || p.Limit > 100 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp.market.search 参数无效", false)
	}
	if e.m7mcp == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 服务暂时不可用", true)
	}
	items, fresh, err := e.m7mcp.MarketSearch(ctx, p.Query, p.Cursor, p.Limit)
	if err != nil {
		return m7McpFailure(r, err, "mcp.market.search")
	}
	out := make([]m7McpMarketItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, m7McpMarketItemDTO{
			ItemID:        it.ID,
			Name:          it.Name,
			Publisher:     it.Publisher,
			Description:   it.Description,
			TransportHint: it.TransportHint,
			Digest:        it.CatalogDigest,
		})
	}
	return bridge.Success(r.ID, struct {
		Items  []m7McpMarketItemDTO `json:"items"`
		Fresh  bool                 `json:"fresh"`
		Cursor string               `json:"cursor,omitempty"`
	}{out, fresh, ""})
}

// m7McpMarketItemDTO is one row of the market search projection.
type m7McpMarketItemDTO struct {
	ItemID        string `json:"itemId"`
	Name          string `json:"name"`
	Publisher     string `json:"publisher"`
	Description   string `json:"description"`
	TransportHint string `json:"transportHint"`
	Digest        string `json:"digest"`
}

// m7McpFailure maps m7app slice-8 errors onto the M7 wire family.
func m7McpFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrMcpSchema):
		return bridge.Failure(r.ID, r.TraceID, "M7-MCP-001", "mcpServers 配置未过 schema 校验", false)
	case errors.Is(err, m7app.ErrMcpSource):
		return bridge.Failure(r.ID, r.TraceID, "M7-MCP-002", "来源未确认或签名校验失败", false)
	case errors.Is(err, m7app.ErrMcpDrift):
		return bridge.Failure(r.ID, r.TraceID, "M7-MCP-003", "能力摘要漂移，端点已隔离", false)
	case errors.Is(err, m7app.ErrMcpProbe):
		return bridge.Failure(r.ID, r.TraceID, "M7-MCP-004", "传输或会话建立失败", true)
	case errors.Is(err, m7app.ErrMcpRegistry):
		return bridge.Failure(r.ID, r.TraceID, "M7-MCP-005", "市场目录不可达，已降级只读缓存", true)
	case errors.Is(err, m7app.ErrMcpNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M7-MCP-006", "endpointId 不存在或已撤销", false)
	case errors.Is(err, m7app.ErrMcpQuota):
		return bridge.Failure(r.ID, r.TraceID, "RATE_LIMITED", "MCP 端点数量或并发超限", true)
	case errors.Is(err, m7app.ErrMcpTimeout):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-006", "MCP 操作超时", true)
	case errors.Is(err, m7app.ErrServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 服务暂时不可用", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", method+" 执行失败", false)
}
