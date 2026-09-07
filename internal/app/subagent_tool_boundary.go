package app

import (
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// Capability names belong to the durable spawn contract; model tool names
// belong to the chat gateway. Translate explicitly so a newly added writer
// cannot become available merely because it was absent from a denylist.
func subagentReadTools(caps []string) map[string]bool {
	have := map[string]bool{}
	for _, cap := range caps {
		have[cap] = true
	}
	return map[string]bool{
		"workspace.list":   have["fs.tree"] || have["fs.glob"] || have["fs.stat"],
		"workspace.read":   have["fs.read"] || have["fs.readMany"],
		"workspace.search": have["fs.grep"],
		"excel.parse":      have["fs.read"] || have["fs.readMany"],
		"command.run":      have["fs.read"] && have["fs.tree"],
		"web.fetch":        have["web.fetch"],
		"web.search":       have["web.search"],
		"browser.act":      have["browser.act:navigate"] || have["browser.act:read"] || have["browser.act:snapshot"],
	}
}

// Enforce the same restriction at execution time, even if a provider emits
// an unoffered tool or a cached caller supplies an overbroad allowed map.
func subagentCallAllowed(profile subagentProfileDef, call llmadapter.ToolCall) bool {
	for _, name := range profile.WriteTools {
		if name == call.Name && !expertWriteToolDenied(name) {
			return true
		}
	}
	if !subagentReadTools(profile.ReadCaps)[call.Name] {
		return false
	}
	switch call.Name {
	case "browser.act":
		var args struct {
			Op string `json:"op"`
		}
		if json.Unmarshal(call.Arguments, &args) != nil {
			return false
		}
		for _, cap := range profile.ReadCaps {
			if cap == "browser.act:"+args.Op {
				return true
			}
		}
		return false
	case "command.run":
		var args struct {
			Argv []string `json:"argv"`
		}
		return json.Unmarshal(call.Arguments, &args) == nil && subagentReadOnlyCommand(args.Argv)
	default:
		return true
	}
}

// The parent command allowlist also includes add/commit/stash and custom
// writers. Read-only delegation keeps the existing observation commands,
// while explicitly authorized command.run still uses the parent runtime.
func subagentReadOnlyCommand(argv []string) bool {
	if len(argv) == 2 && argv[0] == "go" && argv[1] == "version" {
		return true
	}
	if len(argv) < 3 || argv[0] != "git" || argv[1] != "--no-pager" {
		return false
	}
	switch argv[2] {
	case "status", "log", "diff", "show":
		for _, arg := range argv[3:] {
			for _, flag := range []string{"--output", "--ext-diff", "--textconv", "--exec", "--paginate", "--config", "-c"} {
				if arg == flag || strings.HasPrefix(arg, flag+"=") {
					return false
				}
			}
		}
		return true
	case "branch":
		return len(argv) == 3 || len(argv) == 4 && (argv[3] == "--list" || argv[3] == "--show-current")
	}
	return false
}
