package app

import (
	"fmt"
	"strings"
)

type capabilityPackSpec struct {
	Skills       []string
	McpPresetIDs []string
	ToolGates    []string
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
