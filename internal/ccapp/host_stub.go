//go:build !windows

package ccapp

// PlatformHost answers the OS control host. Computer control is
// Windows-only; every other build gets an unavailable host so the
// interception pipeline fails closed with M10-CC-010.
func PlatformHost() Host { return unavailableHost{} }

type unavailableHost struct{}

func (unavailableHost) Available() bool                        { return false }
func (unavailableHost) ScreenSize() (int, int)                 { return 0, 0 }
func (unavailableHost) MouseMove(x, y int) error               { return ErrCcEngineUnavailable }
func (unavailableHost) MouseClick(button string, clicks int) error {
	return ErrCcEngineUnavailable
}
func (unavailableHost) KeyboardType(text string) error          { return ErrCcEngineUnavailable }
func (unavailableHost) KeyboardShortcut(keys []string) error    { return ErrCcEngineUnavailable }
func (unavailableHost) MouseScroll(int) error                   { return ErrCcEngineUnavailable }
func (unavailableHost) ScreenCapture() ([]byte, error)          { return nil, ErrCcEngineUnavailable }
func (unavailableHost) ActiveWindow() (string, string, error) {
	return "", "", ErrCcEngineUnavailable
}
func (unavailableHost) ObserveDialogs() ([]DialogSnapshot, error) {
	return nil, ErrCcEngineUnavailable
}
func (unavailableHost) ConfirmDialog(string) (DialogSnapshot, error) {
	return DialogSnapshot{}, ErrCcEngineUnavailable
}
