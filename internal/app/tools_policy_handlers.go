// tools.commandPolicy.*: the chat command whitelist settings plane. The
// document lives at <tool-workspaces>/command-policy.json; get answers the
// persisted bytes, set validates through the same fail-closed rules the
// runtime applies at startup, then hot-applies without an engine restart.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func handleToolsCommandPolicyGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	raw, err := e.tools.PolicySnapshot("commands")
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
		ExpectedRevision string `json:"expectedRevision"`
		Commands         []struct {
			Prefix    []string `json:"prefix"`
			MaxArgs   int      `json:"maxArgs,omitempty"`
			TimeoutMS int64    `json:"timeoutMs,omitempty"`
		} `json:"commands"`
		FullAccess bool `json:"fullAccess,omitempty"`
	}
	if decodePayload(r.Payload, &doc) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "tools.commandPolicy.set 参数无效", false)
	}
	status, err := e.tools.SetPolicyVersioned("commands", r.Payload, doc.ExpectedRevision)
	if err != nil {
		if errors.Is(err, toolruntime.ErrPolicyRevisionConflict) {
			return r.Fail("SETTINGS_VERSION_CONFLICT", err.Error(), false)
		}
		return r.Fail("COMMAND_POLICY_INVALID", localizeSettingsPolicyError(err), false)
	}
	return r.Ok(struct {
		Applied int `json:"applied"`
		toolruntime.PolicyStatus
	}{len(doc.Commands), status})
}

func localizeSettingsPolicyError(err error) string {
	if err == nil {
		return "策略无效，请检查后重试"
	}
	if errors.Is(err, toolruntime.ErrPolicyRevisionConflict) {
		return err.Error()
	}
	msg := strings.TrimSpace(err.Error())
	switch {
	case strings.Contains(msg, "invalid prefix item"):
		return "命令前缀无效：不能为空、不能包含 ..，也不能以 /、\\ 或盘符开头"
	case strings.Contains(msg, "prefix must have"):
		return "每条规则的命令前缀必须是 1 到 8 段"
	case strings.Contains(msg, "more than 128 commands"):
		return "命令白名单最多 128 条"
	case strings.Contains(msg, "block decision requires a message"):
		return "拦截规则必须填写说明"
	case strings.Contains(msg, "message is only valid for block"):
		return "只有拦截规则才能填写说明"
	case strings.Contains(msg, "needs at least one event and one tool"):
		return "规则必须至少指定一个事件和一个工具"
	case strings.Contains(msg, "unknown event"):
		return "规则事件无效"
	case strings.Contains(msg, "unknown tool"):
		return "规则工具不在可挂钩清单"
	case strings.Contains(msg, "invalid decision"):
		return "规则决定无效"
	case strings.Contains(msg, "message exceeds"):
		return "拦截说明超过字数上限"
	case strings.Contains(msg, "duplicate id"):
		return "规则 ID 重复"
	case strings.Contains(msg, "invalid id"):
		return "规则 ID 无效（1–64 位字母、数字、. _ -）"
	case strings.Contains(msg, "more than") && strings.Contains(msg, "hooks"):
		return "Hooks 规则最多 64 条"
	case strings.HasPrefix(msg, "command-policy.json:"):
		return "命令白名单不是有效文档"
	case strings.HasPrefix(msg, "hooks-policy.json:"):
		return "Hooks 策略不是有效文档"
	case msg == "policy exceeds 64 KiB":
		return "策略文件超过 64 KiB"
	case msg == "unknown policy":
		return "未知策略类型"
	case msg == "invalid policy document":
		return "策略文档无效"
	case msg == "stored policy is not valid JSON", msg == "stored policy is not an object":
		return "已保存的策略不是有效 JSON"
	}
	for _, r := range msg {
		if unicode.Is(unicode.Han, r) {
			return msg
		}
	}
	return "策略无效，请检查后重试"
}
