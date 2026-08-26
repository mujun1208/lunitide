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
//
// Writing the rule needs administrator rights, which this per-user install
// does not have and does not ask for — see addFirewallRule. So the rule is a
// convenience for machines where it happens to be writable, not something
// any behaviour here depends on.

const firewallRuleName = "Lunitide 本地语音识别"

// firewallOnce keeps this to one attempt per run.
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

// addFirewallRule writes the rule if this process happens to have the rights
// to, and gives up quietly if it does not.
//
// It used to ask for them, through PowerShell's Start-Process -Verb RunAs.
// That is a bad trade and it showed: the rule exists to spare the user one
// firewall prompt, and buying it with a User Account Control prompt — which
// is more alarming, names "网络命令外壳" rather than this app, and returned
// on every launch — costs more than the prompt it prevents. Nothing here is
// worth elevation. Windows still blocks unsolicited inbound by default, so
// the machine is no less protected without the rule; the user may simply see
// the firewall ask about the recognizer once.
func addFirewallRule(executable string) {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+firewallRuleName, "dir=in", "action=block",
		"program="+executable, "enable=yes")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	_ = cmd.Run()
}
