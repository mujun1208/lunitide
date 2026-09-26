package producthub

import (
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
)

func auditWiring(cards []Card) []Finding {
	_, _, _, _, findings := collectWiring(cards)
	return findings
}

func collectWiring(cards []Card) (bridges, missingBridges, plugins, missingPlugins []string, findings []Finding) {
	methods := bridgeMethodSet()
	harness := harnessPluginSet()
	for _, c := range cards {
		if strings.HasPrefix(c.StableKey, "landscape.") {
			continue
		}
		var missing []string
		seen := map[string]bool{}
		for _, name := range claimedBridges(c) {
			if seen[name] {
				continue
			}
			seen[name] = true
			bridges = append(bridges, name)
			if entryWired(name, methods) {
				continue
			}
			missing = append(missing, name)
			missingBridges = append(missingBridges, name)
		}
		if len(missing) > 0 {
			findings = append(findings, finding("error", "PH_021", c.StableKey, "入口没有接到桥",
				c.Name+"："+strings.Join(missing, "、"),
				"卡片写了这个入口，当前桥方法名单里没有",
				"把入口改成桥名单里已有的方法，或补上这个方法。下次重新检查这名字在桥里了，本条消失。诊断不改产品代码。",
				"重新检查后本条不再出现", "open"))
		}
		if id, ok := pluginRosterID(c.StableKey); ok {
			plugins = append(plugins, id)
			if _, known := harness[id]; !known {
				missingPlugins = append(missingPlugins, id)
				findings = append(findings, finding("error", "PH_022", c.StableKey, "插件不在运行名单",
					c.Name+"："+id,
					"产品名单里有这个插件，运行名单里没有",
					"运行名单补上这个插件，或从产品名单去掉。下次重新检查对上了，本条消失。诊断不改产品代码。",
					"重新检查后本条不再出现", "open"))
			}
		}
		if (strings.HasSuffix(c.StableKey, ".skill.invoke") || strings.HasSuffix(c.StableKey, ".mcp.invoke")) && len(claimedBridges(c)) == 0 {
			findings = append(findings, finding("error", "PH_021", c.StableKey, "入口没有接到桥",
				c.Name+"：没有桥方法",
				"技能或 MCP 卡没有写上桥方法",
				"写上实际会调用的桥方法。下次重新检查写上了，本条消失。诊断不改产品代码。",
				"重新检查后本条不再出现", "open"))
		}
	}
	return bridges, missingBridges, plugins, missingPlugins, findings
}

func entryWired(name string, methods map[string]struct{}) bool {
	if _, ok := methods[name]; ok {
		return true
	}
	switch name {
	case "media.play", "desktop.open", "desktop.type", "desktop.quit", "desktop.browse",
		"command.run", "system.run", "location.get", "canvas.present", "web.search", "web.fetch",
		"computer.act", "todo.write", "im.send":
		return true
	}
	action, ok := strings.CutPrefix(name, "media.")
	if !ok || action == "" || strings.Contains(action, ".") {
		return false
	}
	switch action {
	case "play", "pause", "stop", "next", "prev", "previous", "toggle", "seek", "mute", "unmute",
		"set_volume", "clear", "create", "jump", "move", "remove", "skip", "open_and_play":
		return true
	default:
		return false
	}
}

func claimedBridges(c Card) []string {
	var out []string
	for _, name := range append(append([]string{}, c.Scaffold.Bridge...), c.Attributes.Tools...) {
		name = strings.TrimSpace(name)
		if strings.Count(name, ".") < 1 || strings.HasPrefix(name, "capability.") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func bridgeMethodSet() map[string]struct{} {
	out := make(map[string]struct{}, len(bridge.Methods))
	for _, method := range bridge.Methods {
		out[string(method)] = struct{}{}
	}
	return out
}

func harnessPluginSet() map[string]struct{} {
	specs := m8app.HarnessPlugins()
	out := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		out[spec.ID] = struct{}{}
	}
	return out
}
