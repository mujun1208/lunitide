//go:build windows

package webviewhost

// closeDisposition decides how the desktop host reacts to a user close action.
type closeDisposition int

const (
	closeHide closeDisposition = iota
	closeDestroy
)

// dispositionForClose maps host state to hide-to-tray vs real exit. Tray
// "退出" sets forceQuit; every other close path (title-bar X, Alt+F4,
// taskbar close) hides while the tray icon keeps the process alive.
func (h *Host) dispositionForClose() closeDisposition {
	if h == nil {
		return closeDestroy
	}
	if h.forceQuit {
		return closeDestroy
	}
	if h.trayAdded {
		return closeHide
	}
	return closeDestroy
}
