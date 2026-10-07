package remotegateway

import "fmt"

// firewallRuleName 是 Windows 防火墙入站规则的固定名称：Enable 时幂等
// 重建、Disable 时删除，规则名固定保证多实例/升级不累积重复规则。
const firewallRuleName = "Lunitide Remote Gateway"

func firewallAddArgs(port int) []string {
	return []string{
		"advfirewall", "firewall", "add", "rule",
		"name=" + firewallRuleName,
		"dir=in", "action=allow", "protocol=TCP",
		fmt.Sprintf("localport=%d", port),
	}
}

func firewallDeleteArgs() []string {
	return []string{"advfirewall", "firewall", "delete", "rule", "name=" + firewallRuleName}
}

func firewallShowArgs() []string {
	return []string{"advfirewall", "firewall", "show", "rule", "name=" + firewallRuleName}
}
