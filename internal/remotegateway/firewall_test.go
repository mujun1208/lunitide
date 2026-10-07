package remotegateway

import (
	"strings"
	"testing"
)

func TestFirewallArgs(t *testing.T) {
	add := firewallAddArgs(DefaultPort)
	if got, want := strings.Join(add, " "), "advfirewall firewall add rule name=Lunitide Remote Gateway dir=in action=allow protocol=TCP localport=47651"; got != want {
		t.Fatalf("add args = %q, want %q", got, want)
	}
	del := firewallDeleteArgs()
	if got, want := strings.Join(del, " "), "advfirewall firewall delete rule name=Lunitide Remote Gateway"; got != want {
		t.Fatalf("delete args = %q, want %q", got, want)
	}
	show := firewallShowArgs()
	if got, want := strings.Join(show, " "), "advfirewall firewall show rule name=Lunitide Remote Gateway"; got != want {
		t.Fatalf("show args = %q, want %q", got, want)
	}
	// 三组参数共用同一规则名，保证 add/show/delete 操作同一条规则。
	for name, args := range map[string][]string{"add": add, "delete": del, "show": show} {
		if !containsRuleName(args) {
			t.Errorf("%s args missing rule name %q", name, firewallRuleName)
		}
	}
}

func containsRuleName(args []string) bool {
	for _, arg := range args {
		if arg == "name="+firewallRuleName {
			return true
		}
	}
	return false
}
