package app

import (
	"encoding/json"
	"strings"
)

// ungatedEngineToolDenied covers the tool families the chat loop dispatches
// itself instead of handing to the tool runtime: MCP, the settings-plane
// installers, and browser.act. The runtime owns the approval gate, so these
// names never reach it — they executed unprompted in every mode, including the
// one whose entire promise is a prompt. There is no pending-approval path to
// route them through yet (toolruntime.Prepare only knows its own switch), so
// the honest answer in a mode that promises a gate is to refuse and say why.
//
// The reason is returned as an ok:false tool result: the model reads it, keeps
// its turn, and can tell the user which mode to switch to.
func ungatedEngineToolDenied(mode executionMode, companion bool, name string, args json.RawMessage) (string, bool) {
	gated := mode == executionModeApproval
	switch strings.TrimSpace(name) {
	case "mcp.install", "plugin.install":
		// Adds a remote endpoint or a plugin to the machine. Never something
		// a voice turn should land either.
		if !gated && !companion {
			return "", false
		}
		return "ok:false\n" + name + " 会给本机装上新的服务端或插件，这条链路暂时没有审批弹窗，本轮不执行。请在设置里手动安装。", true
	case "mcp.call":
		// The far side is a third-party server; the engine cannot know whether
		// it writes, so it assumes it does.
		if !gated {
			return "", false
		}
		return "ok:false\nmcp.call 会调到本机之外的服务，当前是手动审批模式，而这条链路还没有审批弹窗，本轮不执行。切到自动审批或完全访问后再试。", true
	case "browser.act":
		if !browserActActuates(args) {
			return "", false
		}
		if companion {
			return "ok:false\nbrowser.act 的点击/输入会动到已登录的浏览器，语音里没有可确认的界面，本轮不执行。请在文字对话里再说一次。", true
		}
		if !gated {
			return "", false
		}
		return "ok:false\nbrowser.act 的点击/输入会动到已登录的浏览器，当前是手动审批模式，而这条链路还没有审批弹窗，本轮不执行。切到自动审批或完全访问后再试。", true
	}
	if _, _, isMcp := parseMcpToolName(name); isMcp {
		if !gated {
			return "", false
		}
		return "ok:false\n" + name + " 会调到本机之外的服务，当前是手动审批模式，而这条链路还没有审批弹窗，本轮不执行。切到自动审批或完全访问后再试。", true
	}
	return "", false
}

// browserActActuates separates driving the page from reading it. Navigating
// and snapshotting are how the model finds out what is there; clicking and
// typing land inside whatever session the browser is already signed into.
func browserActActuates(args json.RawMessage) bool {
	var a struct {
		Op string `json:"op"`
	}
	if json.Unmarshal(args, &a) != nil {
		// Unreadable arguments: browser.act will reject them anyway, and
		// assuming "just looking" is the wrong way to be wrong.
		return true
	}
	switch strings.TrimSpace(a.Op) {
	case "click", "type", "press", "select", "dialog":
		return true
	default:
		return false
	}
}

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
