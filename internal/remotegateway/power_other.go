//go:build !windows

package remotegateway

// setKeepAwake 在非 Windows 平台是空实现（远程网关当前仅面向 Windows
// 桌面宿主；保持包可交叉编译）。
func setKeepAwake(bool) error { return nil }
