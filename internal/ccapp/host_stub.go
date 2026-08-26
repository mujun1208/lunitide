//go:build !windows

package ccapp

// PlatformHost answers the OS control host. Computer control is
// Windows-only; every other build gets an unavailable host so the
// interception pipeline fails closed with M10-CC-010.
func PlatformHost() Host { return unavailableHost{} }

type unavailableHost struct{}

var _ Host = unavailableHost{}

func (unavailableHost) Available() bool          { return false }
func (unavailableHost) ScreenSize() (int, int)   { return 0, 0 }
func (unavailableHost) ScreenOrigin() (int, int) { return 0, 0 }
func (unavailableHost) CursorPosition() (int, int, error) {
	return 0, 0, ErrCcEngineUnavailable
}
func (unavailableHost) MouseMove(x, y int) error { return ErrCcEngineUnavailable }
func (unavailableHost) MouseClick(button string, clicks int) error {
	return ErrCcEngineUnavailable
}
func (unavailableHost) MouseDrag(int, int, int, int) error   { return ErrCcEngineUnavailable }
func (unavailableHost) KeyboardType(text string) error       { return ErrCcEngineUnavailable }
func (unavailableHost) KeyboardShortcut(keys []string) error { return ErrCcEngineUnavailable }
func (unavailableHost) MouseScroll(int) error                { return ErrCcEngineUnavailable }
func (unavailableHost) MouseScrollH(int) error               { return ErrCcEngineUnavailable }
func (unavailableHost) EnsureForeground() error              { return ErrCcEngineUnavailable }
func (unavailableHost) ScreenCapture() ([]byte, error)       { return nil, ErrCcEngineUnavailable }
func (unavailableHost) WindowCapture(string) ([]byte, int, int, error) {
	return nil, 0, 0, ErrCcEngineUnavailable
}
func (unavailableHost) ActiveWindow() (string, string, error) {
	return "", "", ErrCcEngineUnavailable
}
func (unavailableHost) ListWindows() ([]WindowInfo, error) {
	return nil, ErrCcEngineUnavailable
}
func (unavailableHost) FocusWindow(string) (WindowInfo, error) {
	return WindowInfo{}, ErrCcEngineUnavailable
}
func (unavailableHost) ObserveDialogs() ([]DialogSnapshot, error) {
	return nil, ErrCcEngineUnavailable
}
func (unavailableHost) ConfirmDialog(string) (DialogSnapshot, error) {
	return DialogSnapshot{}, ErrCcEngineUnavailable
}
func (unavailableHost) ObserveUI(int) ([]UINode, error) {
	return nil, ErrCcEngineUnavailable
}
func (unavailableHost) ClipboardGet() (string, error) {
	return "", ErrCcEngineUnavailable
}
func (unavailableHost) ClipboardSet(string) error {
	return ErrCcEngineUnavailable
}
func (unavailableHost) WindowAction(string, string, int, int, int, int) (WindowInfo, error) {
	return WindowInfo{}, ErrCcEngineUnavailable
}
func (unavailableHost) QuitApp(string) (int, WindowInfo, error) {
	return 0, WindowInfo{}, ErrCcEngineUnavailable
}
func (unavailableHost) MenuClick(string) error { return ErrCcEngineUnavailable }
func (unavailableHost) SetValue(string, string) error {
	return ErrCcEngineUnavailable
}
func (unavailableHost) InvokeUI(string) error { return ErrCcEngineUnavailable }
