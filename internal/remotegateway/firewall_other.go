//go:build !windows

package remotegateway

// ensureFirewallRule / removeFirewallRule 在非 Windows 平台是空实现
// （远程网关当前仅面向 Windows 桌面宿主；保持包可交叉编译）。
func ensureFirewallRule(int, bool) error { return nil }

func removeFirewallRule(int) error { return nil }
