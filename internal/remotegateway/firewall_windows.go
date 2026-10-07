//go:build windows

package remotegateway

import (
	"fmt"
	"os/exec"
	"strings"
)

// ensureFirewallRule 幂等确保入站放行 TCP 端口的防火墙规则存在：
//  1. 规则已存在（show 退出码 0）直接返回；
//  2. 以当前权限执行 netsh add（管理员身份运行的引擎会直接成功）；
//  3. 普通权限失败且 allowElevate 时，经 PowerShell Start-Process -Verb
//     RunAs 弹 UAC 请求一次性授权（仅用户显式 Enable 的路径允许提权；
//     开机自动恢复监听不弹窗，失败只记日志，局域网直连可能仍可用）。
func ensureFirewallRule(port int, allowElevate bool) error {
	if firewallRuleExists() {
		return nil
	}
	add := firewallAddArgs(port)
	if err := runNetsh(add); err == nil {
		return nil
	} else if !allowElevate {
		return err
	}
	if err := runNetshElevated(add); err != nil {
		return err
	}
	if firewallRuleExists() {
		return nil
	}
	return fmt.Errorf("elevated netsh completed but rule still missing")
}

// removeFirewallRule 删除入站规则。删除同样需要管理员权限，但关闭路径
// 不弹 UAC：监听已停，残留规则没有放行目标（无安全影响），下次 Enable
// 会先重建规则；静默失败只记日志。
func removeFirewallRule(port int) error {
	if !firewallRuleExists() {
		return nil
	}
	return runNetsh(firewallDeleteArgs())
}

func firewallRuleExists() bool {
	return exec.Command("netsh", firewallShowArgs()...).Run() == nil
}

func runNetsh(args []string) error {
	out, err := exec.Command("netsh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %s: %w: %s", strings.Join(args, " "), err, clipOutput(out))
	}
	return nil
}

// runNetshElevated 经 PowerShell 弹 UAC 以管理员身份执行 netsh 并等待
// 完成。用户拒绝 UAC 或防火墙服务不可用时返回错误。
func runNetshElevated(args []string) error {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "''")+"'")
	}
	script := fmt.Sprintf("Start-Process -FilePath netsh -ArgumentList @(%s) -Verb RunAs -Wait -WindowStyle Hidden", strings.Join(quoted, ","))
	out, err := exec.Command("powershell", "-NoProfile", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("elevate netsh %s: %w: %s", strings.Join(args, " "), err, clipOutput(out))
	}
	return nil
}

func clipOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > 160 {
		text = text[:160]
	}
	return text
}
