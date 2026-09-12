package agenthub

// CapabilityFor is the V1.2 matrix. Codex/Cursor/Kimi have documented
// non-interactive argv. Detect `available` still requires the exe on PATH.
func CapabilityFor(name string) Capability {
	switch name {
	case "codex", "cursor", "kimi":
		return Capability{NonInteractive: true, StreamJSON: true}
	default:
		return Capability{}
	}
}

func agentNames() []string {
	return []string{"codex", "cursor", "kimi"}
}

func exeName(agent string) string {
	if agent == "cursor" {
		return "cursor-agent"
	}
	return agent
}
