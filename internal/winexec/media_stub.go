//go:build !windows

package winexec

import "errors"

// SendMediaKey is unsupported off Windows.
func SendMediaKey(string) error {
	return errors.New("media keys unsupported on this platform")
}
