package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func mcpPinDigest(pin mcp6.CapabilityPin) string {
	raw, _ := json.Marshal(pin)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func handleMcpSecurityReview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID      string `json:"endpointId"`
		ExpectedVersion int64  `json:"expectedVersion"`
		Action          string `json:"action"`
		ObservedDigest  string `json:"observedDigest"`
		Confirmed       bool   `json:"confirmed"`
	}
	if decodePayload(r.Payload, &p) != nil || e.m7mcp == nil || e.mcp6Registry == nil || !e.mcp6Registry.SecurityEnabled() || p.ExpectedVersion < 0 || (p.Action != "inspect" && p.Action != "accept") || (p.Action == "accept" && (!p.Confirmed || len(p.ObservedDigest) != 64)) {
		return r.Fail("MCP_REVIEW_INVALID", "MCP 复核参数无效", false)
	}
	ep, err := e.m7mcp.Endpoint(ctx, p.EndpointID)
	if err != nil {
		return m7McpFailure(r, err, "security review")
	}
	input, err := settingsMcpInput(ep)
	if err != nil {
		return m7McpFailure(r, err, "security review")
	}
	reply := func(observed *mcp6.Endpoint, version int64, accepted bool) bridge.Response {
		names := make([]string, 0, len(observed.Pin.ToolSchemaDigests))
		for name := range observed.Pin.ToolSchemaDigests {
			names = append(names, name)
		}
		sort.Strings(names)
		return r.Ok(map[string]any{"observedDigest": mcpPinDigest(observed.Pin), "identityDigest": observed.Pin.ServerIdentityDigest, "tools": names, "lockedArgs": append([]string{}, observed.Args...), "securityVersion": version, "accepted": accepted})
	}
	if ep.Security.Version != p.ExpectedVersion {
		if p.Action == "accept" && ep.State == "ready" && ep.Security.Version == p.ExpectedVersion+1 && mcpPinDigest(input.Pin) == p.ObservedDigest {
			return reply(&mcp6.Endpoint{Pin: input.Pin, Args: input.Args}, ep.Security.Version, true)
		}
		return r.Fail("MCP_REVIEW_CONFLICT", "MCP 配置已变化，请重新复核", false)
	}
	if ep.State == "revoked" {
		return r.Fail("MCP_REVIEW_CONFLICT", "此 MCP 已删除", false)
	}
	ctx, release, err := e.AcquireCapability(ctx, mcpPluginIDs(input.Command, input.Args)...)
	if err != nil {
		return m7McpFailure(r, err, "security review")
	}
	defer release()
	input.Pin = mcp6.BootstrapPin("explicit-review")
	input.LaunchDigest = ""
	observed, err := e.mcp6Registry.CheckEndpoint(ctx, input)
	if err != nil {
		return m7McpFailure(r, err, "security review")
	}
	if p.Action == "inspect" {
		return reply(observed, ep.Security.Version, false)
	}
	if mcpPinDigest(observed.Pin) != p.ObservedDigest {
		return r.Fail("MCP_REVIEW_CONFLICT", "服务器在确认后再次变化，请重新查看", false)
	}
	security, err := e.m7mcp.ReplacePin(ctx, ep, observed.Pin, observed.Args, observed.LaunchDigest)
	if err != nil {
		return m7McpFailure(r, err, "security review")
	}
	return reply(observed, security.Version, true)
}
