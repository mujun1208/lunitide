package app

import "strings"

// Companion keeps full-access for low-risk tools (approved once).
// Dangerous names always raise approval_required — never session-approve.
func approvalProfileDangerous(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	if strings.HasPrefix(n, "cc.") || strings.HasPrefix(n, "desktop.") {
		return true
	}
	switch n {
	case "command.run", "im.send", "computer.act":
		return true
	default:
		return false
	}
}

func companionFullDiskWrite(name string) bool {
	switch strings.TrimSpace(name) {
	case "workspace.write", "workspace.edit":
		return true
	default:
		return false
	}
}

func companionToolPreapproved(name string, fullDisk bool) bool {
	if name == "user.ask" || approvalProfileDangerous(name) {
		return false
	}
	if fullDisk && companionFullDiskWrite(name) {
		return false
	}
	return true
}
