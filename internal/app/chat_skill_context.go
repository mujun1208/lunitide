package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

var errSkillContextBudget = errors.New("SKILL_CONTEXT_BUDGET_EXCEEDED：当前模型剩余上下文不足以完整加载该技能；未截断技能或按不完整约束执行，请开启新对话或选更大上下文模型")

// Invoke loads a complete, governance-verified agreement into model context.
// The UI summary remains small; unlike ordinary tool logs this is executable
// task guidance whose final constraints must not be clipped for presentation.
func skillInvocationFitsContext(p provider.Provider, req llmadapter.Request, output string) error {
	if len(output) > skillInvocationMaxBytes+1024 {
		return errSkillContextBudget
	}
	window := int64(128000)
	if m := modelByID(p, req.Model); m.ContextWindow > 0 {
		window = m.ContextWindow
	}
	reserved := int64(req.MaxTokens)
	if reserved <= 0 {
		reserved = chatMaxTokens
	}
	used := token.CountTokensForModel(req.Model, output) + 1024
	for _, m := range req.Messages {
		used += token.CountTokensForModel(req.Model, m.Content) + 8
		for _, call := range m.ToolCalls {
			used += token.CountTokensForModel(req.Model, call.Name) + token.CountTokensForModel(req.Model, string(call.Arguments))
		}
	}
	for _, t := range req.Tools {
		raw, _ := json.Marshal(t)
		used += token.CountTokensForModel(req.Model, string(raw))
	}
	// Image costs vary by provider. Reserve the same bounded upper estimate per
	// attached image instead of claiming their token cost is zero.
	used += int64(len(req.Images)) * 4096
	if used+reserved > window*15/16 {
		return errSkillContextBudget
	}
	return nil
}

func skillInvocationDisplay(output string) string {
	if len(output) <= 3800 {
		return output
	}
	return truncateUTF8Bytes(output, 3400) + "\n（完整正文与尾部约束已保留在本轮上下文；界面仅展示摘要。）"
}

func skillInvocationReplay(output, callID string, names ...string) string {
	source, _, _ := strings.Cut(output, "\n")
	name := "skill.invoke"
	if len(names) > 0 && names[0] == "skill.try" {
		name = "skill.try"
	}
	return truncateUTF8Bytes(source, 800) + "\n本次是重复调用回执，不是技能全文。完整约定保留在此前 callId=" + truncateUTF8Bytes(callID, 128) + " 的 " + name + " 工具消息中；继续使用那份完整约定，不重复注入或执行。"
}
