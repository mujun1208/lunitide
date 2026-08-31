package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/skillapp"
)

type capabilityPackSpec struct {
	Skills        []string
	McpPresetIDs  []string
	ToolGates     []string
}

func packSpecFromManifest(manifest map[string]any) capabilityPackSpec {
	return capabilityPackSpec{
		Skills:       stringSliceFromAny(manifest["skills"]),
		McpPresetIDs: stringSliceFromAny(firstAny(manifest["mcpPresetIds"], manifest["mcp"])),
		ToolGates:    stringSliceFromAny(firstAny(manifest["toolGates"], manifest["gates"])),
	}
}

func firstAny(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				text = strings.TrimSpace(text)
				if text != "" {
					out = append(out, text)
				}
			}
		}
		return out
	default:
		return nil
	}
}

func (e *Engine) applyCapabilityPack(ctx context.Context, spec capabilityPackSpec) (notes []string, failed string) {
	for _, templateID := range spec.Skills {
		if !skillServiceAvailable(e.skills) {
			return notes, "技能服务不可用"
		}
		created, err := e.skills.InstallFromCatalog(ctx, templateID)
		if err == nil {
			if created.Status == skill.SkillStatusDraft {
				if perr := e.skills.Publish(ctx, created.ID); perr != nil {
					return notes, "技能 " + created.Name + " 已安装但发布失败：" + perr.Error()
				}
			}
			notes = append(notes, "技能 "+created.Name)
			continue
		}
		if errors.Is(err, skillapp.ErrTemplateInstalled) {
			notes = append(notes, "技能 "+templateID+"（已有）")
			continue
		}
		return notes, "技能 " + templateID + "：" + err.Error()
	}
	for _, presetID := range spec.McpPresetIDs {
		if e.m7mcp == nil {
			return notes, "MCP 服务不可用"
		}
		if _, ok := mcp6.PresetByID(presetID); !ok {
			return notes, "MCP " + presetID + " 不在策展货架"
		}
		raw, _ := json.Marshal(map[string]string{"presetId": presetID})
		out, err := e.invokeMcpInstallPreset(ctx, raw)
		if err != nil {
			if strings.Contains(err.Error(), "already") || strings.Contains(err.Error(), "已") {
				notes = append(notes, "MCP "+presetID+"（已有）")
				continue
			}
			return notes, "MCP " + presetID + "：" + err.Error()
		}
		notes = append(notes, "MCP "+presetID+" "+out)
	}
	for _, gate := range spec.ToolGates {
		if e.m8plugin == nil {
			notes = append(notes, "门闸 "+gate+"（插件服务不可用）")
			continue
		}
		raw, _ := json.Marshal(map[string]string{"origin": "local", "source": gate})
		if _, err := e.invokePluginInstall(ctx, raw); err != nil {
			notes = append(notes, "门闸 "+gate+"：跳过（"+err.Error()+"）")
			continue
		}
		notes = append(notes, "门闸 "+gate)
	}
	return notes, ""
}

func formatPackNotes(notes []string, failed string) string {
	var b strings.Builder
	if len(notes) > 0 {
		b.WriteString("已安装：")
		b.WriteString(strings.Join(notes, "；"))
	}
	if failed != "" {
		if b.Len() > 0 {
			b.WriteString("。")
		}
		b.WriteString("失败：")
		b.WriteString(failed)
	}
	if b.Len() == 0 {
		return "清单为空，只登记了卡片。"
	}
	return b.String()
}

func formatPackInstallResult(label, pluginID, state string, notes []string, failed string) string {
	return fmt.Sprintf("已创建能力包「%s」（id=%s，state=%s）。%s 不会执行 TypeScript。", label, pluginID, state, formatPackNotes(notes, failed))
}
