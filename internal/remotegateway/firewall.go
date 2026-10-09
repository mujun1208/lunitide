package remotegateway

import (
	"fmt"
	"strings"
)

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

// netshElevateScript 构造以管理员身份执行一次 netsh 的 PowerShell 脚本。
// 两处关键点（都有实测回归依据，v0.17.5 修复）：
//  1. Windows PowerShell 5.1 的 Start-Process 不会给含空格的 ArgumentList
//     元素自动加引号——'name=Lunitide Remote Gateway' 会被拆成三个独立
//     参数传给 netsh，规则静默建不上（UAC 已批准、netsh 却参数错误）。
//     因此含空格的参数在元素值里内嵌双引号（'"name=X Y"'），保证 netsh
//     收到完整的一个参数；无空格参数原样传，与 Go exec 直接调用时的
//     命令行引号形状一致。
//  2. Start-Process 默认不回传子进程退出码，netsh 失败也会被当成"成功"。
//     用 -PassThru 拿到进程对象、exit $p.ExitCode 把退出码传出来，失败
//     不再静默。参数集由本文件构造，不含双引号，无需再转义。
func netshElevateScript(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t") {
			quoted = append(quoted, `'"`+arg+`"'`)
		} else {
			quoted = append(quoted, `'`+arg+`'`)
		}
	}
	return fmt.Sprintf(`$p = Start-Process -FilePath netsh -ArgumentList @(%s) -Verb RunAs -Wait -WindowStyle Hidden -PassThru; exit $p.ExitCode`, strings.Join(quoted, ","))
}
