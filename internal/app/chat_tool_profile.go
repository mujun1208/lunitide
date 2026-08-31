package app

import (
	"strings"

	"github.com/lunitide/lunitide/internal/gateway"
)

type toolProfile string

const (
	toolProfileDefault   toolProfile = ""
	toolProfileMinimal   toolProfile = "minimal"
	toolProfileCoding    toolProfile = "coding"
	toolProfileColleague toolProfile = "colleague"
)

func parseToolProfile(raw string) toolProfile {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "minimal":
		return toolProfileMinimal
	case "coding":
		return toolProfileCoding
	case "colleague":
		return toolProfileColleague
	default:
		return toolProfileDefault
	}
}

func toolProfileAllow(profile toolProfile) map[string]bool {
	switch profile {
	case toolProfileMinimal:
		return map[string]bool{
			"web.search": true, "web.fetch": true,
			"memory.search": true, "memory.get": true,
			"user.ask": true,
		}
	case toolProfileCoding:
		return map[string]bool{
			"workspace.list": true, "workspace.read": true, "workspace.write": true,
			"workspace.search": true, "workspace.edit": true,
			"command.run": true,
			"web.search":  true, "web.fetch": true,
			"memory.search": true, "memory.get": true,
			"skill.invoke": true, "skill.view": true,
			"todo.write": true,
		}
	case toolProfileColleague:
		allow := map[string]bool{}
		for name := range specialistToolAllow {
			allow[name] = true
		}
		allow["memory.search"] = true
		allow["memory.get"] = true
		return allow
	default:
		return nil
	}
}

func applyToolProfile(defs []gateway.ToolDefinition, profile toolProfile) []gateway.ToolDefinition {
	allow := toolProfileAllow(profile)
	if allow == nil {
		return defs
	}
	return filterToolDefs(defs, allow)
}
