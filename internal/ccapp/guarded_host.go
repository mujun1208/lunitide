package ccapp

// Only mutating calls use this adapter. The original host retains optional
// observation/capture interfaces and platform-specific capability detection.
type guardedHost struct {
	Host
	service *Service
}

func (s *Service) controlHost() guardedHost { return guardedHost{s.host, s} }
func (h guardedHost) MouseMove(x, y int) error {
	return h.service.dispatch(true, func() error { return h.Host.MouseMove(x, y) })
}
func (h guardedHost) MouseClick(button string, clicks int) error {
	return h.service.dispatch(true, func() error { return h.Host.MouseClick(button, clicks) })
}
func (h guardedHost) MouseDrag(x1, y1, x2, y2 int) error {
	return h.service.dispatch(true, func() error { return h.Host.MouseDrag(x1, y1, x2, y2) })
}
func (h guardedHost) KeyboardType(text string) error {
	return h.service.dispatch(true, func() error { return h.Host.KeyboardType(text) })
}
func (h guardedHost) KeyboardShortcut(keys []string) error {
	return h.service.dispatch(true, func() error { return h.Host.KeyboardShortcut(keys) })
}
func (h guardedHost) MouseScroll(n int) error {
	return h.service.dispatch(true, func() error { return h.Host.MouseScroll(n) })
}
func (h guardedHost) MouseScrollH(n int) error {
	return h.service.dispatch(true, func() error { return h.Host.MouseScrollH(n) })
}
func (h guardedHost) EnsureForeground() error {
	return h.service.dispatch(true, h.Host.EnsureForeground)
}
func (h guardedHost) ClipboardSet(text string) error {
	return h.service.dispatch(false, func() error { return h.Host.ClipboardSet(text) })
}
func (h guardedHost) MenuClick(path string) error {
	return h.service.dispatch(true, func() error { return h.Host.MenuClick(path) })
}
func (h guardedHost) SetValue(target, value string) error {
	return h.service.dispatch(true, func() error { return h.Host.SetValue(target, value) })
}
func (h guardedHost) InvokeUI(target string) error {
	return h.service.dispatch(true, func() error { return h.Host.InvokeUI(target) })
}
func (h guardedHost) HoldKey(key string, down bool) error {
	if !down {
		return h.Host.HoldKey(key, false)
	}
	return h.service.dispatch(true, func() error { return h.Host.HoldKey(key, true) })
}
func (h guardedHost) FocusWindow(query string) (out WindowInfo, err error) {
	err = h.service.dispatch(false, func() error { out, err = h.Host.FocusWindow(query); return err })
	if err == nil {
		err = h.service.checkProcess(out.Process)
	}
	return
}
func (h guardedHost) ConfirmDialog(button string) (out DialogSnapshot, err error) {
	err = h.service.dispatch(true, func() error { out, err = h.Host.ConfirmDialog(button); return err })
	return
}
func (h guardedHost) WindowAction(query, op string, x, y, w, height int) (out WindowInfo, err error) {
	if err = h.service.checkTargetNow(ToolWindowAction, query, op); err != nil {
		return
	}
	err = h.service.dispatch(false, func() error { out, err = h.Host.WindowAction(query, op, x, y, w, height); return err })
	return
}
func (h guardedHost) QuitApp(query string) (closed int, out WindowInfo, err error) {
	if err = h.service.checkTargetNow(ToolAppQuit, query, ""); err != nil {
		return
	}
	err = h.service.dispatch(false, func() error { closed, out, err = h.Host.QuitApp(query); return err })
	return
}
