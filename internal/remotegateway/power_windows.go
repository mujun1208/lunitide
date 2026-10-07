//go:build windows

package remotegateway

import (
	"fmt"
	"syscall"
)

// SetThreadExecutionState 不在 x/sys/windows 导出集里，按仓库惯例
// （winexec/webviewhost）用 lazydll 声明。
var kernel32Power = syscall.NewLazyDLL("kernel32.dll")

var procSetThreadExecutionState = kernel32Power.NewProc("SetThreadExecutionState")

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// setKeepAwake 通过 SetThreadExecutionState 声明/释放系统唤醒需求。
// ES_CONTINUOUS 状态粘在调用线程上，因此本调用被钉在 keepAwakeLoop 的
// 专用 OS 线程（见 service.go），设置与清除必然同线程。
func setKeepAwake(on bool) error {
	flags := uintptr(esContinuous)
	if on {
		flags |= esSystemRequired
	}
	prev, _, err := procSetThreadExecutionState.Call(flags)
	if prev == 0 && err != syscall.Errno(0) {
		return fmt.Errorf("SetThreadExecutionState: %w", err)
	}
	return nil
}
