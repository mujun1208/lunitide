package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m7app"
)

func handleMcpCredentialResolve(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID string `json:"endpointId"`
	}
	if decodePayload(r.Payload, &p) != nil || p.EndpointID == "" || e.m7mcp == nil {
		return r.Fail("MCP_CREDENTIAL_INVALID", "MCP 凭据目标无效", false)
	}
	ep, err := e.m7mcp.Endpoint(ctx, p.EndpointID)
	if err != nil {
		return m7McpFailure(r, err, "credential resolve")
	}
	in, err := settingsMcpInput(ep)
	if err != nil {
		return m7McpFailure(r, err, "credential resolve")
	}
	return r.Ok(map[string]any{"endpointId": ep.EndpointID, "runtimeId": in.ID, "transport": in.Transport, "url": in.URL, "command": in.Command, "args": in.Args, "authRef": in.AuthRef, "envRefs": in.EnvSecretRefs, "securityVersion": ep.Security.Version, "state": ep.State})
}

func handleMcpCredentialBind(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID      string `json:"endpointId"`
		ExpectedVersion int64  `json:"expectedVersion"`
		Ref             string `json:"ref"`
		Env             string `json:"env"`
	}
	if decodePayload(r.Payload, &p) != nil || p.ExpectedVersion < 0 || e.m7mcp == nil {
		return r.Fail("MCP_CREDENTIAL_INVALID", "MCP 凭据参数无效", false)
	}
	if p.Ref != "" && !strings.HasPrefix(p.Ref, "secretref:mcp/"+chatMcpEndpointID(p.EndpointID)+"/") {
		return r.Fail("MCP_CREDENTIAL_INVALID", "凭据引用不属于此 MCP", false)
	}
	refs := map[string]string{}
	if p.Env != "" {
		refs[p.Env] = "secretref:validation"
	}
	if err := m7app.ValidateMcpSecretRefs(p.Ref, refs); err != nil {
		return m7McpFailure(r, err, "credential bind")
	}
	out, err := e.m7mcp.BindCredential(ctx, p.EndpointID, p.ExpectedVersion, p.Ref, p.Env)
	if err != nil {
		return r.Fail("MCP_CREDENTIAL_CONFLICT", "MCP 配置已变化，请刷新后重试", false)
	}
	return r.Ok(map[string]any{"securityVersion": out.Version, "configured": p.Ref != ""})
}
