//go:build !windows

package meetings

import "errors"

func openPlatformLoopback() (loopbackSource, error) {
	return nil, errors.New("system loopback is Windows-only")
}
