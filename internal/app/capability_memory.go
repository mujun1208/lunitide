package app

import (
	"context"
	"log"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/mcp6"
)

// capability-self-bootstrap P4: an approved MCP preset install is a settled
// user decision, not an inference. Recording it in semantic memory lets later
// turns of the same project recall "the user already approved this endpoint
// for this kind of task" — so the model proposes reconnecting instead of
// re-asking, and never re-installs what the user already trusted.
const mcpPresetApprovalMemoryKeyPrefix = "mcp-preset-approval:"

func (e *Engine) recordApprovedMcpPreset(ctx context.Context, sessionID, presetID, state string) {
	if e == nil || !memoryServiceAvailable(e.memories) || strings.TrimSpace(presetID) == "" {
		return
	}
	// The approval card is an explicit user action, so explicit-save policy
	// (not auto-capture) decides whether memory may record it.
	if !m8core.ResolveMemoryBehavior(e.chatMemoryV2(ctx), "project", m8core.CurrentProductFlags()).AllowExplicitSave {
		return
	}
	preset, ok := mcp6.PresetByID(strings.TrimSpace(presetID))
	if !ok {
		return
	}
	projectID := e.projectIDForSession(ctx, sessionID)
	if projectID == "" {
		return
	}
	content := "用户批准安装并连接了 MCP 预置 " + preset.Name + "（presetId=" + preset.ID + "）：" + preset.Description +
		"。同类任务可直接使用其 mcp_ 前缀工具，优先于换用其他方案。"
	if state == "needs_configuration" {
		content = "用户批准安装 MCP 预置 " + preset.Name + "（presetId=" + preset.ID + "）：" + preset.Description +
			"。待在设置页配置密钥并连接后生效；配置完成后同类任务可直接使用其 mcp_ 前缀工具。"
	}
	key := mcpPresetApprovalMemoryKeyPrefix + preset.ID
	src, srcType := sessionID, "mcp-install"
	items, err := e.memories.ListByProject(ctx, projectID, memory.LayerSemantic)
	if err != nil {
		log.Printf("capability memory: preset approval write skipped: %v", err)
		return
	}
	for _, item := range items {
		if item.Key == key {
			if err := e.memories.UpdateContent(ctx, item.ID, content); err != nil {
				log.Printf("capability memory: preset approval update skipped: %v", err)
			}
			return
		}
	}
	_, err = e.memories.Create(ctx, memory.Memory{
		ProjectID:  projectID,
		Layer:      memory.LayerSemantic,
		Scope:      memory.ScopeProject,
		Key:        key,
		Content:    content,
		SourceID:   &src,
		SourceType: &srcType,
		Confidence: 0.9,
	})
	if err != nil {
		log.Printf("capability memory: preset approval write skipped: %v", err)
	}
}

// presetInstalledBefore reports whether any remembered endpoint maps to this
// preset. The persist map survives restarts, so "previously approved"
// outlives the endpoint itself.
func (e *Engine) presetInstalledBefore(presetID string) bool {
	if e == nil || presetID == "" {
		return false
	}
	found := false
	e.mcpPresetByEP.Range(func(_, value any) bool {
		if id, _ := value.(string); id == presetID {
			found = true
			return false
		}
		return true
	})
	return found
}

// installedPresetEndpoint finds the live registry endpoint for a preset so
// installs stay idempotent: a surviving endpoint means "reconnect in
// Settings", not a duplicate Add.
func (e *Engine) installedPresetEndpoint(presetID string) (string, bool) {
	if e == nil || presetID == "" || e.mcp6Registry == nil {
		return "", false
	}
	var endpointID string
	e.mcpPresetByEP.Range(func(key, value any) bool {
		if id, _ := value.(string); id == presetID {
			if epID, _ := key.(string); epID != "" {
				endpointID = epID
				return false
			}
		}
		return true
	})
	if endpointID == "" {
		return "", false
	}
	ep, err := e.mcp6Registry.Get(endpointID)
	if err != nil || ep == nil {
		return "", false
	}
	return endpointID, true
}
