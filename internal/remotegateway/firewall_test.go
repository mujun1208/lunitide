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

// TestNetshElevateScript 钉死提权脚本的引号形状：含空格的参数必须整体
// 内嵌双引号（PowerShell 5.1 Start-Process 不自动加引号，拆散后 netsh
// 参数错误、规则静默建不上——v0.17.5 实测回归修复），且必须 -PassThru
// 透传 netsh 退出码（失败不再被误判为成功）。
func TestNetshElevateScript(t *testing.T) {
	script := netshElevateScript(firewallAddArgs(DefaultPort))
	want := `$p = Start-Process -FilePath netsh -ArgumentList @('advfirewall','firewall','add','rule','"name=Lunitide Remote Gateway"','dir=in','action=allow','protocol=TCP','localport=47651') -Verb RunAs -Wait -WindowStyle Hidden -PassThru; exit $p.ExitCode`
	if script != want {
		t.Fatalf("elevate script =\n%s\nwant\n%s", script, want)
	}
	if !strings.Contains(script, `'"name=Lunitide Remote Gateway"'`) {
		t.Errorf("rule name arg must embed double quotes inside the single-quoted element, got %s", script)
	}
	if !strings.Contains(script, "-PassThru") || !strings.Contains(script, "exit $p.ExitCode") {
		t.Errorf("script must propagate netsh exit code via -PassThru, got %s", script)
	}
}
