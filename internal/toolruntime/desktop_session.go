package toolruntime

import "errors"

// ErrDesktopLocked is the HAT-04 fail-closed signal: the workstation is
// locked, so no keystrokes, clicks, or other desktop actuation may run.
var ErrDesktopLocked = errors.New("锁屏中，已停止桌面输入，未向界面发送按键")

// desktopInputBlocked is the lock-screen probe. Tests replace it; production
// uses workstationLocked so headless sessions that cannot prove a lock stay
// unlocked instead of blocking every desktop tool.
var desktopInputBlocked = workstationLocked
