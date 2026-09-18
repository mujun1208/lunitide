package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
)

func handleMcpUvInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	_ = ctx
	if e == nil || e.mcpUv == nil {
		return r.Fail("MCP-UV-001", "uv 安装服务未就绪", true)
	}
	if len(r.Payload) > 0 && string(r.Payload) != "{}" && string(r.Payload) != "null" {
		var empty map[string]any
		if decodePayload(r.Payload, &empty) != nil || len(empty) > 0 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "mcp.uv.install 参数无效", false)
		}
	}
	e.mcpUv.BeginInstall()
	return r.Ok(e.mcpUv.Snapshot())
}
