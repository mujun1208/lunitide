//go:build !windows

package winexec

import "errors"

// ActivateWindowMatching is a no-op on non-Windows builds.
func ActivateWindowMatching(fragment string) error {
	return errors.New("window activation unavailable")
}
