//go:build !windows

package electronsafestorage

func withAPIKeyPlatform(_, _, _, _ string, _ func([]byte) error) error {
	return ErrUnavailable
}
