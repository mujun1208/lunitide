//go:build !windows

package winexec

import "errors"

// ActivateWindowMatching is a no-op on non-Windows builds.
func ActivateWindowMatching(fragment string) error {
	return errors.New("window activation unavailable")
}

func ForegroundWindow() (string, string, error) {
	return "", "", errors.New("foreground window unavailable")
}

func ListVisibleWindows() []WindowHint {
	return nil
}
