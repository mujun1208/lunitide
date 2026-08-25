package voice

import (
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// Keeping the recognizer off the network.
//
// sherpa's websocket server takes a --port and nothing else: there is no
// bind-address option, so it listens on every interface. Windows blocks
// unsolicited inbound traffic by default, so the machine is not actually
// exposed — but the firewall asks the user about it, and that prompt arrives
// mid-conversation, names an executable they have never heard of, and offers
// them the chance to say yes.
//
// An explicit inbound block rule removes both problems at once. The prompt is
// only raised when no rule matches, so a rule silences it; and because the
// rule denies rather than allows, the answer holds even if some other
// installer later flips the default. Loopback is never filtered by Windows
// Firewall, so our own connection to 127.0.0.1 is unaffected.

const firewallRuleName = "Lunitide 本地语音识别"

// firewallOnce keeps this to one attempt per run. A user who declines the
// elevation should not be asked again every time they speak.
var firewallOnce sync.Once

// ensureFirewallRule adds the block rule if it is missing.
//
// Failure is deliberately not an error the caller acts on. The rule is a
// papercut fix, not a safety property — without it recognition still works
// and the machine is still protected by the default inbound block. Refusing
// to recognize speech because a firewall rule could not be written would
// trade a small annoyance for a broken feature.
func ensureFirewallRule(executable string) {
	firewallOnce.Do(func() {
		if firewallRuleExists() {
			return
		}
		addFirewallRule(executable)
	})
}

// firewallRuleExists asks whether the rule is already there. Reading rules
// does not require elevation, so this is the cheap path on every run after
// the first.
func firewallRuleExists() bool {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+firewallRuleName)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		// netsh exits non-zero when no rule matches, which is the common
		// case rather than a failure.
		return false
	}
	return !strings.Contains(string(out), "No rules match")
}

// addFirewallRule writes the rule, elevating because netsh cannot add one
// without administrator rights and this installer runs per-user.
//
// The elevation is requested through PowerShell's Start-Process -Verb RunAs
// rather than by relaunching ourselves elevated: only the netsh call needs
// the privilege, and an app that asks to run as administrator to hold a
// conversation is asking for far more than it needs.
func addFirewallRule(executable string) {
	script := "Start-Process -FilePath netsh -Verb RunAs -WindowStyle Hidden -Wait -ArgumentList " +
		"'advfirewall','firewall','add','rule'," +
		"'name=" + firewallRuleName + "','dir=in','action=block'," +
		"'program=" + executable + "','enable=yes'"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	_ = cmd.Run()
}
