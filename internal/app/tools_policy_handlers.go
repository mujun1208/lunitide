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
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	raw, err := e.tools.CommandPolicyJSON()
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "命令白名单读取失败", true)
	}
	return r.Ok(json.RawMessage(raw))
}

func handleToolsCommandPolicySet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "tools.commandPolicy.set 参数无效", false)
	}
	if err := e.tools.SetCommandPolicyJSON(r.Payload); err != nil {
		return r.Fail("COMMAND_POLICY_INVALID", err.Error(), false)
	}
	return r.Ok(struct {
		Applied int `json:"applied"`
	}{len(doc.Commands)})
}
