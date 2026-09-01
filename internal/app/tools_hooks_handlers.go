// tools.hooksPolicy.* / tools.hooksEvents.*: the P3-B hooks settings
// plane. The document lives at <tool-workspaces>/hooks-policy.json; get
// answers the persisted bytes, set validates through the same
// fail-closed rules the runtime applies at startup, then hot-applies
// without an engine restart. hooksEvents.list surfaces the bounded
// audit trail of hook matches.
package app

import (
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func handleToolsHooksPolicyGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	raw, err := e.tools.HooksPolicyJSON()
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "Hooks 策略读取失败", true)
	}
	return r.Ok(json.RawMessage(raw))
}

func handleToolsHooksPolicySet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var doc struct {
		Hooks []struct {
			ID       string   `json:"id"`
			Events   []string `json:"events"`
			Tools    []string `json:"tools"`
			Decision string   `json:"decision"`
			Message  string   `json:"message,omitempty"`
		} `json:"hooks"`
	}
	if decodePayload(r.Payload, &doc) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "tools.hooksPolicy.set 参数无效", false)
	}
	if err := e.tools.SetHooksPolicyJSON(r.Payload); err != nil {
		return r.Fail("HOOKS_POLICY_INVALID", err.Error(), false)
	}
	return r.Ok(struct {
		Applied int `json:"applied"`
	}{len(doc.Hooks)})
}

func handleToolsHooksEventsList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var doc struct {
		Limit int `json:"limit"`
	}
	if decodePayload(r.Payload, &doc) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "tools.hooksEvents.list 参数无效", false)
	}
	if doc.Limit == 0 {
		doc.Limit = 50
	}
	events, err := e.tools.ListHookEvents(ctx, doc.Limit)
	if err != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "Hooks 审计读取失败", true)
	}
	if events == nil {
		events = []toolruntime.HookEvent{}
	}
	return r.Ok(struct {
		Events []toolruntime.HookEvent `json:"events"`
	}{events})
}
