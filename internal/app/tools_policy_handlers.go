// tools.commandPolicy.*: the chat command whitelist settings plane. The
// document lives at <tool-workspaces>/command-policy.json; get answers the
// persisted bytes, set validates through the same fail-closed rules the
// runtime applies at startup, then hot-applies without an engine restart.
package app

import (
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/bridge"
)

func handleToolsCommandPolicyGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	raw, err := e.tools.CommandPolicyJSON()
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "命令白名单读取失败", true)
	}
	return bridge.Success(r.ID, json.RawMessage(raw))
}

func handleToolsCommandPolicySet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var doc struct {
		Commands []struct {
			Prefix    []string `json:"prefix"`
			MaxArgs   int      `json:"maxArgs,omitempty"`
			TimeoutMS int64    `json:"timeoutMs,omitempty"`
		} `json:"commands"`
		FullAccess bool `json:"fullAccess,omitempty"`
	}
	if decodePayload(r.Payload, &doc) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "tools.commandPolicy.set 参数无效", false)
	}
	if err := e.tools.SetCommandPolicyJSON(r.Payload); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "COMMAND_POLICY_INVALID", err.Error(), false)
	}
	return bridge.Success(r.ID, struct {
		Applied int `json:"applied"`
	}{len(doc.Commands)})
}
